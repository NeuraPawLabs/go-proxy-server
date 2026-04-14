package proxy

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/apeming/go-proxy-server/internal/activity"
	"github.com/apeming/go-proxy-server/internal/auth"
	"github.com/apeming/go-proxy-server/internal/config"
	"github.com/apeming/go-proxy-server/internal/constants"
	"github.com/apeming/go-proxy-server/internal/logger"
	"github.com/apeming/go-proxy-server/internal/metrics"
	"github.com/apeming/go-proxy-server/internal/security"
)

// Transport pool for connection reuse to destination servers
var (
	defaultTransport *http.Transport
	transportOnce    sync.Once
	// Transport cache for bind-listen mode: map[localIP] -> *http.Transport
	// Caches transports per local address to enable connection pooling
	transportCache sync.Map
)

// Buffer pool for bufio.Reader to reduce memory allocations
var readerPool = sync.Pool{
	New: func() interface{} {
		return bufio.NewReaderSize(nil, constants.BufferSizeSmall)
	},
}

// getReader gets a bufio.Reader from the pool and resets it with the given connection
func getReader(conn net.Conn) *bufio.Reader {
	reader := readerPool.Get().(*bufio.Reader)
	reader.Reset(conn)
	return reader
}

// putReader returns a bufio.Reader to the pool
func putReader(reader *bufio.Reader) {
	// Reset with nil to release the connection reference
	reader.Reset(nil)
	readerPool.Put(reader)
}

// getDefaultTransport returns a shared HTTP transport with connection pooling and DNS rebinding protection
func getDefaultTransport() *http.Transport {
	transportOnce.Do(func() {
		defaultTransport = &http.Transport{
			MaxIdleConns:        constants.HTTPPoolMaxIdleConns,
			MaxIdleConnsPerHost: constants.HTTPPoolMaxIdleConnsPerHost,
			IdleConnTimeout:     constants.HTTPPoolIdleConnTimeout,
			DisableKeepAlives:   false,
			DisableCompression:  false,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				timeout := config.GetTimeout()
				dialer := &net.Dialer{
					Timeout: timeout.Connect,
				}
				conn, err := dialer.DialContext(ctx, network, addr)
				if err != nil {
					return nil, err
				}
				// Verify connected IP to prevent DNS rebinding attacks
				if err := security.VerifyConnectedIP(conn); err != nil {
					conn.Close()
					return nil, err
				}
				return conn, nil
			},
		}
	})
	return defaultTransport
}

// getTransportForLocalAddr returns a cached HTTP transport for the given local address
// This enables connection pooling in bind-listen mode where each local IP needs its own transport
func getTransportForLocalAddr(localAddr *net.TCPAddr) *http.Transport {
	key := localAddr.IP.String()

	// Try to load existing transport from cache
	if cached, ok := transportCache.Load(key); ok {
		return cached.(*http.Transport)
	}

	// Create new transport with local address binding and DNS rebinding protection
	transport := &http.Transport{
		MaxIdleConns:        constants.HTTPPoolMaxIdleConns,
		MaxIdleConnsPerHost: constants.HTTPPoolMaxIdleConnsPerHost,
		IdleConnTimeout:     constants.HTTPPoolIdleConnTimeout,
		DisableKeepAlives:   false,
		DisableCompression:  false,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			currentTimeout := config.GetTimeout()
			dialer := &net.Dialer{
				LocalAddr: localAddr,
				Timeout:   currentTimeout.Connect,
			}
			conn, err := dialer.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			// Verify connected IP to prevent DNS rebinding attacks
			if err := security.VerifyConnectedIP(conn); err != nil {
				conn.Close()
				return nil, err
			}
			return conn, nil
		},
	}

	// Store in cache (LoadOrStore ensures only one transport per key)
	actual, _ := transportCache.LoadOrStore(key, transport)
	return actual.(*http.Transport)
}

// CloseAllTransports closes all cached transports (call on shutdown)
func CloseAllTransports() {
	if defaultTransport != nil {
		defaultTransport.CloseIdleConnections()
	}
	transportCache.Range(func(key, value interface{}) bool {
		if transport, ok := value.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
		return true
	})
}

// writeHTTPError writes an HTTP error response to the connection
func writeHTTPError(conn net.Conn, statusCode int, statusText string, headers map[string]string) error {
	resp := &http.Response{
		Status:     fmt.Sprintf("%d %s", statusCode, statusText),
		StatusCode: statusCode,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
	}

	// Set default headers
	resp.Header.Set("Content-Length", "0")
	resp.Header.Set("Connection", "close")

	// Set custom headers
	for k, v := range headers {
		resp.Header.Set(k, v)
	}

	return resp.Write(conn)
}

// validateAndConnect performs SSRF check, establishes connection, and verifies connected IP
// Returns the connection and any error encountered
func validateAndConnect(host string, bindListen bool, localAddr *net.TCPAddr, timeout config.TimeoutConfig) (net.Conn, error) {
	// Check for SSRF attacks (prevent access to private IPs)
	if err := security.CheckSSRF(host); err != nil {
		// Don't log the host to avoid leaking user's target destinations
		logger.Warn("SSRF protection triggered")
		activity.RecordEvent(activity.EventRecord{
			Category:  "security",
			EventType: "http_ssrf_blocked",
			Severity:  activity.SeverityWarn,
			Source:    "http_proxy",
			Message:   "HTTP SSRF protection blocked an outbound request",
		})
		return nil, fmt.Errorf("SSRF protection: %w", err)
	}

	// Connect to the destination host with timeout
	dialer := &net.Dialer{
		Timeout: timeout.Connect,
	}
	if bindListen {
		dialer.LocalAddr = localAddr
	}
	destConn, err := dialer.Dial("tcp", host)
	if err != nil {
		logger.Error("Failed to connect to destination host: %v", err)
		return nil, fmt.Errorf("connection failed: %w", err)
	}

	// Verify connected IP to prevent DNS rebinding attacks
	if err := security.VerifyConnectedIP(destConn); err != nil {
		// Don't log the error details to avoid leaking target IP information
		logger.Warn("DNS rebinding protection triggered")
		activity.RecordEvent(activity.EventRecord{
			Category:  "security",
			EventType: "http_dns_rebinding_blocked",
			Severity:  activity.SeverityWarn,
			Source:    "http_proxy",
			Message:   "HTTP DNS rebinding protection blocked an outbound request",
		})
		destConn.Close()
		return nil, fmt.Errorf("DNS rebinding protection: %w", err)
	}

	return destConn, nil
}

// shouldCloseConnection determines if the connection should be closed based on HTTP headers
// Returns true if connection should be closed, false if it can be kept alive
func shouldCloseConnection(req *http.Request, resp *http.Response) bool {
	// Close connection if either client or server requests it
	if strings.ToLower(req.Header.Get("Connection")) == "close" {
		return true
	}
	if strings.ToLower(resp.Header.Get("Connection")) == "close" {
		return true
	}
	// HTTP/1.0 defaults to close unless explicitly kept alive
	if req.ProtoMajor == 1 && req.ProtoMinor == 0 {
		if strings.ToLower(req.Header.Get("Connection")) != "keep-alive" {
			return true
		}
	}
	return false
}

func HandleHTTPConnection(conn net.Conn, bindListen bool) {
	defer conn.Close()

	// Record connection
	if collector := metrics.GetCollector(); collector != nil {
		collector.RecordConnection()
		defer collector.RecordDisconnection()
	}

	// Get the client's IP address early for rate limiting
	clientAddr, ok := conn.RemoteAddr().(*net.TCPAddr)
	if !ok {
		logger.Error("Connection is not TCP")
		if collector := metrics.GetCollector(); collector != nil {
			collector.RecordError()
		}
		return
	}
	clientIP := clientAddr.IP.String()

	// Apply connection rate limiting
	limiter := GetHTTPLimiter()
	if !limiter.Acquire(clientIP) {
		logger.Warn("Connection limit reached for IP %s", clientIP)
		activity.RecordEvent(activity.EventRecord{
			Category:  "security",
			EventType: "http_rate_limit_blocked",
			Severity:  activity.SeverityWarn,
			Source:    "http_proxy",
			Message:   "HTTP connection was blocked by connection limits",
			Details: map[string]any{
				"client_ip": clientIP,
			},
		})
		// Try to send 503 Service Unavailable before closing
		writeHTTPError(conn, http.StatusServiceUnavailable, "Service Unavailable", nil)
		if collector := metrics.GetCollector(); collector != nil {
			collector.RecordError()
		}
		return
	}
	defer limiter.Release(clientIP)

	// Get local TCP addresses with type assertion checks
	tcpLocalAddr, ok := conn.LocalAddr().(*net.TCPAddr)
	if !ok {
		logger.Error("Connection is not TCP")
		return
	}
	localAddr := &net.TCPAddr{IP: tcpLocalAddr.IP}

	// Get timeout configuration once at the beginning
	timeout := config.GetTimeout()

	// Get buffered reader from pool for persistent connection support
	reader := getReader(conn)
	defer putReader(reader)

	// Connection-level authentication state for Keep-Alive optimization
	// Track request count to periodically re-verify credentials for security
	var isAuthenticated bool
	var requestCount int

	// Handle multiple requests on the same connection (HTTP/1.1 Keep-Alive)
	for {
		// Set read timeout for waiting for next request (use IdleRead timeout)
		conn.SetReadDeadline(time.Now().Add(timeout.IdleRead))

		// Read the HTTP request
		req, err := http.ReadRequest(reader)
		if err != nil {
			// EOF or timeout is normal for persistent connections
			if err == io.EOF {
				// Client closed connection gracefully
				return
			}
			// Check for timeout or connection reset
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// Idle timeout reached, close connection
				return
			}
			// Check for connection reset or other network errors
			if strings.Contains(err.Error(), "connection reset") ||
				strings.Contains(err.Error(), "broken pipe") {
				// Connection closed by peer
				return
			}
			// Other errors are unexpected
			logger.Error("Failed to read HTTP request: %v", err)
			return
		}

		// Increment request count and check if re-authentication is needed
		requestCount++
		if requestCount > constants.MaxRequestsBeforeReauth {
			// Reset authentication state to force re-verification
			isAuthenticated = false
			requestCount = 0
		}

		// Check authentication
		// For Keep-Alive connections, use cached authentication state to avoid repeated bcrypt verification
		// but re-verify periodically for security
		authenticated := isAuthenticated

		if !authenticated {
			// Check if the client's IP address is in the whitelist first
			if auth.CheckIPWhitelist(clientIP) {
				authenticated = true
				isAuthenticated = true
			} else {
				// Check for Proxy-Authorization header
				authHeader := req.Header.Get("Proxy-Authorization")
				if authHeader != "" {
					// Parse Basic authentication
					if strings.HasPrefix(authHeader, "Basic ") {
						encoded := strings.TrimPrefix(authHeader, "Basic ")
						decoded, err := base64.StdEncoding.DecodeString(encoded)
						if err == nil {
							authParts := strings.SplitN(string(decoded), ":", 2)
							if len(authParts) == 2 {
								username := authParts[0]
								password := authParts[1]

								// Verify credentials
								if auth.VerifyCredentials(username, []byte(password)) == nil {
									authenticated = true
									isAuthenticated = true
								}
							}
						}
					}
				}
			}
		}

		if !authenticated {
			// Send 407 Proxy Authentication Required
			headers := map[string]string{
				"Proxy-Authenticate": "Basic realm=\"Proxy\"",
			}
			if err := writeHTTPError(conn, http.StatusProxyAuthRequired, "Proxy Authentication Required", headers); err != nil {
				logger.Error("Failed to write authentication response: %v", err)
			}
			return
		}

		// Handle the request based on method
		if req.Method == http.MethodConnect {
			// HTTPS tunneling (CONNECT method) - closes connection after tunnel
			handleHTTPSConnect(conn, req, bindListen, localAddr, timeout)
			return
		} else {
			// Regular HTTP proxy - may support keep-alive
			shouldClose := handleHTTPRequest(conn, req, reader, bindListen, localAddr, timeout)
			if shouldClose {
				return
			}
		}
	}
}

func handleHTTPSConnect(conn net.Conn, req *http.Request, bindListen bool, localAddr *net.TCPAddr, timeout config.TimeoutConfig) {
	// Extract host and port from request
	host := req.Host
	if !strings.Contains(host, ":") {
		host = host + ":443"
	}

	// Validate and connect to destination (includes SSRF check and DNS rebinding protection)
	destConn, err := validateAndConnect(host, bindListen, localAddr, timeout)
	if err != nil {
		// Determine response based on error type
		if strings.Contains(err.Error(), "SSRF protection") || strings.Contains(err.Error(), "DNS rebinding") {
			writeHTTPError(conn, http.StatusForbidden, "Forbidden", nil)
		} else {
			writeHTTPError(conn, http.StatusBadGateway, "Bad Gateway", nil)
		}
		return
	}
	defer destConn.Close()

	// Send 200 Connection Established response
	resp := &http.Response{
		Status:     "200 Connection Established",
		StatusCode: 200,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
	}
	err = resp.Write(conn)
	if err != nil {
		logger.Error("Failed to send response: %v", err)
		return
	}

	// Create context for cancellation with maximum connection age
	ctx, cancel := context.WithTimeout(context.Background(), timeout.MaxConnectionAge)
	defer cancel()

	// Start bidirectional data transfer with idle timeout
	errChan := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	// Client to destination (upload)
	go func() {
		defer wg.Done()
		err := copyWithIdleTimeout(ctx, destConn, conn, timeout.IdleRead, timeout.IdleWrite, true)
		if tcpConn, ok := destConn.(*net.TCPConn); ok {
			tcpConn.CloseWrite()
		}
		errChan <- err
	}()

	// Destination to client (download)
	go func() {
		defer wg.Done()
		err := copyWithIdleTimeout(ctx, conn, destConn, timeout.IdleRead, timeout.IdleWrite, false)
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.CloseWrite()
		}
		errChan <- err
	}()

	// Wait for first goroutine to complete or timeout
	select {
	case <-errChan:
		// First goroutine finished
	case <-ctx.Done():
		// Timeout reached
		logger.Info("HTTPS tunnel maximum age reached, closing connection")
	}

	// Cancel context to stop the other goroutine
	cancel()

	// Wait for both goroutines to finish with cleanup timeout
	cleanupDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(cleanupDone)
	}()

	select {
	case <-cleanupDone:
		// Both goroutines finished gracefully
	case <-time.After(timeout.CleanupTimeout):
		// Force close if cleanup takes too long
		logger.Warn("Force closing HTTPS tunnel after cleanup timeout")
	}
}

func handleHTTPRequest(conn net.Conn, req *http.Request, reader *bufio.Reader, bindListen bool, localAddr *net.TCPAddr, timeout config.TimeoutConfig) bool {
	// Extract host from request
	host := req.Host
	if !strings.Contains(host, ":") {
		host = host + ":80"
	}

	// SSRF check before making request
	if err := security.CheckSSRF(host); err != nil {
		writeHTTPError(conn, http.StatusForbidden, "Forbidden", nil)
		return true // Close connection
	}

	// Remove Proxy-Authorization header before forwarding
	req.Header.Del("Proxy-Authorization")
	req.Header.Del("Proxy-Connection")

	// Convert request to absolute form to relative form
	req.RequestURI = ""

	// Use HTTP client with connection pooling
	var transport *http.Transport
	if bindListen {
		// Use cached transport for this local address to enable connection pooling
		transport = getTransportForLocalAddr(localAddr)
	} else {
		// Use default shared transport
		transport = getDefaultTransport()
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   timeout.IdleRead + timeout.IdleWrite,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Don't follow redirects automatically
			return http.ErrUseLastResponse
		},
	}

	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		logger.Error("Failed to make HTTP request: %v", err)
		writeHTTPError(conn, http.StatusBadGateway, "Bad Gateway", nil)
		return true // Close connection
	}
	defer resp.Body.Close()

	// Note: DNS rebinding protection is now handled in the Transport's DialContext
	// See getDefaultTransport() and getTransportForLocalAddr() for implementation

	// Check if connection should be kept alive using helper function
	shouldClose := shouldCloseConnection(req, resp)

	// Ensure Connection header is set correctly in response
	if shouldClose {
		resp.Header.Set("Connection", "close")
	}

	// Set write timeout for sending response
	conn.SetWriteDeadline(time.Now().Add(timeout.IdleWrite))

	// Write response to client
	err = resp.Write(conn)
	if err != nil {
		logger.Error("Failed to write response to client: %v", err)
		return true // Close connection
	}

	return shouldClose
}
