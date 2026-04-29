package proxy

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/apeming/go-proxy-server/internal/activity"
	"github.com/apeming/go-proxy-server/internal/auth"
	"github.com/apeming/go-proxy-server/internal/config"
	"github.com/apeming/go-proxy-server/internal/logger"
	"github.com/apeming/go-proxy-server/internal/metrics"
	"github.com/apeming/go-proxy-server/internal/security"
)

// SOCKS5 protocol constants
const (
	// SOCKS version
	socks5Version = 0x05

	// Authentication methods
	authMethodNoAuth       = 0x00
	authMethodUserPassword = 0x02
	authMethodNoAcceptable = 0xFF

	// Authentication sub-protocol version
	authSubVersion = 0x01

	// SOCKS5 commands
	cmdConnect      = 0x01
	cmdBind         = 0x02
	cmdUDPAssociate = 0x03

	// Address types
	addrTypeIPv4   = 0x01
	addrTypeDomain = 0x03
	addrTypeIPv6   = 0x04

	// Reply codes
	replySuccess              = 0x00
	replyGeneralFailure       = 0x01
	replyConnectionNotAllowed = 0x02
	replyNetworkUnreachable   = 0x03
	replyHostUnreachable      = 0x04
	replyConnectionRefused    = 0x05
	replyTTLExpired           = 0x06
	replyCommandNotSupported  = 0x07
	replyAddrTypeNotSupported = 0x08

	// Limits
	maxMethods     = 10
	maxUsernameLen = 64
	maxPasswordLen = 128
	maxDomainLen   = 255 // RFC 1035: maximum domain name length
)

func HandleSocks5Connection(conn net.Conn, bindListen bool) {
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
	limiter := GetSOCKS5Limiter()
	if !limiter.Acquire(clientIP) {
		logger.Warn("Connection limit reached for IP %s", clientIP)
		activity.RecordEvent(activity.EventRecord{
			Category:  "security",
			EventType: "socks5_rate_limit_blocked",
			Severity:  activity.SeverityWarn,
			Source:    "socks5_proxy",
			Message:   "SOCKS5 connection was blocked by connection limits",
			Details: map[string]any{
				"client_ip": clientIP,
			},
		})
		if collector := metrics.GetCollector(); collector != nil {
			collector.RecordError()
		}
		return
	}
	defer limiter.Release(clientIP)

	// Initial version/method negotiation
	methods, err := readMethods(conn)
	if err != nil {
		logger.Error("Failed to read methods: %v", err)
		return
	}

	// Get local and remote TCP addresses with type assertion checks
	tcpLocalAddr, ok := conn.LocalAddr().(*net.TCPAddr)
	if !ok {
		logger.Error("Connection is not TCP")
		return
	}
	localAddr := &net.TCPAddr{IP: tcpLocalAddr.IP}
	outboundLocalAddr, bindDecision := resolveOutboundLocalAddr(bindListen, localAddr)
	logBindDecision("SOCKS5", clientIP, bindDecision)

	// Check if the client's IP address is in the whitelist first
	if auth.CheckIPWhitelist(clientIP) {
		// IP in whitelist, no authentication required
		if _, err := conn.Write([]byte{socks5Version, authMethodNoAuth}); err != nil {
			logger.Error("Failed to write response: %v", err)
			return
		}
	} else if isAuthMethodSupported(methods) {
		// Not in whitelist, but supports authentication
		if _, err := conn.Write([]byte{socks5Version, authMethodUserPassword}); err != nil {
			logger.Error("Failed to write response: %v", err)
			return
		}

		// Read the Username/Password authentication request
		if err = readAuthenticationRequest(conn); err != nil {
			logger.Info("Authentication failed from %s: %v", clientIP, err)
			activity.RecordEvent(activity.EventRecord{
				Category:  "auth",
				EventType: "socks5_auth_failed",
				Severity:  activity.SeverityWarn,
				Source:    "socks5_proxy",
				Message:   "SOCKS5 username/password authentication failed",
				Details: map[string]any{
					"client_ip": clientIP,
				},
			})
			// Send authentication failure response
			if _, err := conn.Write([]byte{authSubVersion, 0x01}); err != nil {
				logger.Error("Failed to write response: %v", err)
			}
			return
		}

		// Send the authentication response with success
		if _, err := conn.Write([]byte{authSubVersion, replySuccess}); err != nil {
			logger.Error("Failed to write response: %v", err)
			return
		}
	} else {
		// Not in whitelist and doesn't support authentication
		logger.Info("Unauthorized connection attempt from %s", clientIP)
		activity.RecordEvent(activity.EventRecord{
			Category:  "security",
			EventType: "socks5_unauthorized_attempt",
			Severity:  activity.SeverityWarn,
			Source:    "socks5_proxy",
			Message:   "SOCKS5 connection was rejected because the client is not whitelisted and did not offer authentication",
			Details: map[string]any{
				"client_ip": clientIP,
			},
		})
		if _, err := conn.Write([]byte{socks5Version, authMethodNoAcceptable}); err != nil {
			logger.Error("Failed to write response: %v", err)
		}
		return
	}

	// Read the SOCKS5 request
	host, err := readSocks5Request(conn)
	if err != nil {
		logger.Error("Failed to read SOCKS5 request: %v", err)
		// Determine error code based on error type
		errorCode := byte(replyGeneralFailure)
		if strings.Contains(err.Error(), "unsupported command") {
			errorCode = byte(replyCommandNotSupported)
		}
		// Send error response
		sendSocks5Reply(conn, errorCode)
		return
	}

	// Check for SSRF attacks (prevent access to private IPs)
	if err := security.CheckSSRF(host); err != nil {
		// Don't log the error details to avoid leaking target host information
		logger.Info("SSRF protection triggered for connection from %s", clientIP)
		activity.RecordEvent(activity.EventRecord{
			Category:  "security",
			EventType: "socks5_ssrf_blocked",
			Severity:  activity.SeverityWarn,
			Source:    "socks5_proxy",
			Message:   "SOCKS5 SSRF protection blocked an outbound request",
			Details: map[string]any{
				"client_ip": clientIP,
			},
		})
		sendSocks5Reply(conn, replyConnectionNotAllowed)
		return
	}

	// Connect to the destination host with timeout
	timeout := config.GetTimeout()
	dialer := &net.Dialer{
		Timeout: timeout.Connect,
	}
	if bindListen && outboundLocalAddr != nil {
		dialer.LocalAddr = outboundLocalAddr
	}
	destConn, err := dialer.Dial("tcp", host)

	if err != nil {
		logger.Error("Failed to connect to destination host: %v", err)
		// Determine appropriate SOCKS5 error code based on error type
		errorCode := byte(replyGeneralFailure)

		// Check for specific network errors
		if netErr, ok := err.(net.Error); ok {
			if netErr.Timeout() {
				errorCode = byte(replyTTLExpired)
			}
		}

		// Check for connection refused
		if strings.Contains(err.Error(), "connection refused") {
			errorCode = byte(replyConnectionRefused)
		} else if strings.Contains(err.Error(), "network is unreachable") {
			errorCode = byte(replyNetworkUnreachable)
		} else if strings.Contains(err.Error(), "no route to host") || strings.Contains(err.Error(), "host is unreachable") {
			errorCode = byte(replyHostUnreachable)
		}

		// Send error response
		sendSocks5Reply(conn, errorCode)
		return
	}
	defer destConn.Close()

	// Verify connected IP to prevent DNS rebinding attacks
	if err := security.VerifyConnectedIP(destConn); err != nil {
		// Don't log the error details to avoid leaking target IP information
		logger.Info("DNS rebinding protection triggered for connection from %s", clientIP)
		activity.RecordEvent(activity.EventRecord{
			Category:  "security",
			EventType: "socks5_dns_rebinding_blocked",
			Severity:  activity.SeverityWarn,
			Source:    "socks5_proxy",
			Message:   "SOCKS5 DNS rebinding protection blocked an outbound request",
			Details: map[string]any{
				"client_ip": clientIP,
			},
		})
		sendSocks5Reply(conn, replyConnectionNotAllowed)
		return
	}

	// Send success response to the client
	sendSocks5Reply(conn, replySuccess)

	// Create context for cancellation with maximum connection age
	ctx, cancel := context.WithTimeout(context.Background(), timeout.MaxConnectionAge)
	defer cancel()

	// Copy data between client and destination with idle timeout
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
		logger.Info("Connection maximum age reached, closing connection")
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
		logger.Warn("Force closing connection after cleanup timeout")
	}
}

func readMethods(conn net.Conn) ([]byte, error) {
	// Get buffer from pool
	buffer := bufferPool.Get().([]byte)
	defer bufferPool.Put(buffer)

	_, err := io.ReadFull(conn, buffer[:2])
	if err != nil {
		return nil, err
	}

	// Verify SOCKS version (must be 0x05)
	if buffer[0] != socks5Version {
		return nil, fmt.Errorf("unsupported SOCKS version: 0x%02x (expected 0x05)", buffer[0])
	}

	numMethods := int(buffer[1])

	// Validate number of methods (must be between 1 and maxMethods)
	if numMethods < 1 {
		return nil, fmt.Errorf("invalid number of methods: %d (must be at least 1)", numMethods)
	}
	if numMethods > maxMethods {
		return nil, fmt.Errorf("invalid number of methods: %d (maximum %d allowed)", numMethods, maxMethods)
	}

	methods := make([]byte, numMethods)
	_, err = io.ReadFull(conn, methods)
	if err != nil {
		return nil, err
	}
	return methods, nil
}

func isAuthMethodSupported(methods []byte) bool {
	for _, method := range methods {
		if method == authMethodUserPassword {
			return true
		}
	}
	return false
}

func readAuthenticationRequest(conn net.Conn) error {
	// Get buffer from pool
	buffer := bufferPool.Get().([]byte)
	defer bufferPool.Put(buffer)

	if _, err := io.ReadFull(conn, buffer[:1]); err != nil {
		return err
	}

	// Verify authentication sub-protocol version (should be 0x01)
	if buffer[0] != authSubVersion {
		return fmt.Errorf("unsupported authentication version: 0x%02x", buffer[0])
	}

	var uLen, pLen byte
	if err := binary.Read(conn, binary.BigEndian, &uLen); err != nil {
		return err
	}

	// Validate username length (reasonable limit: 1-maxUsernameLen bytes)
	if uLen < 1 {
		return fmt.Errorf("invalid username length: %d (must be at least 1)", uLen)
	}
	if uLen > maxUsernameLen {
		return fmt.Errorf("invalid username length: %d (maximum %d allowed)", uLen, maxUsernameLen)
	}

	usernameBytes := make([]byte, uLen)
	_, err := io.ReadFull(conn, usernameBytes)
	if err != nil {
		return err
	}
	username := string(usernameBytes)

	if err = binary.Read(conn, binary.BigEndian, &pLen); err != nil {
		return err
	}

	// Validate password length (reasonable limit: 1-maxPasswordLen bytes)
	if pLen < 1 {
		return fmt.Errorf("invalid password length: %d (must be at least 1)", pLen)
	}
	if pLen > maxPasswordLen {
		return fmt.Errorf("invalid password length: %d (maximum %d allowed)", pLen, maxPasswordLen)
	}

	passwordBytes := make([]byte, pLen)
	_, err = io.ReadFull(conn, passwordBytes)
	if err != nil {
		return err
	}

	// Verify credentials without caching (security fix)
	return auth.VerifyCredentials(username, passwordBytes)
}

func readSocks5Request(conn net.Conn) (string, error) {
	// Get buffer from pool
	buffer := bufferPool.Get().([]byte)
	defer bufferPool.Put(buffer)

	_, err := io.ReadFull(conn, buffer[:4])
	if err != nil {
		return "", err
	}

	// Check SOCKS5 version
	if buffer[0] != socks5Version {
		return "", fmt.Errorf("unsupported SOCKS version: %d", buffer[0])
	}

	// Check CMD field - only support CONNECT (0x01)
	if buffer[1] != cmdConnect {
		return "", fmt.Errorf("unsupported command: %d (only CONNECT is supported)", buffer[1])
	}

	// Parse the destination address
	host := ""
	switch buffer[3] {
	case addrTypeIPv4: // IPv4 address
		ip := make([]byte, 4)
		_, err = io.ReadFull(conn, ip)
		if err != nil {
			return "", err
		}
		host = net.IP(ip).String()
	case addrTypeDomain: // Domain name
		var domainLen byte
		if err := binary.Read(conn, binary.BigEndian, &domainLen); err != nil {
			return "", err
		}
		// Validate domain length (must be between 1 and 255 per SOCKS5 and DNS specs)
		if domainLen < 1 {
			return "", fmt.Errorf("invalid domain length: %d (must be at least 1)", domainLen)
		}
		if domainLen > maxDomainLen {
			return "", fmt.Errorf("invalid domain length: %d (maximum %d allowed)", domainLen, maxDomainLen)
		}
		domainBytes := make([]byte, domainLen)
		_, err = io.ReadFull(conn, domainBytes)
		if err != nil {
			return "", err
		}
		host = string(domainBytes)
	case addrTypeIPv6: // IPv6 address
		ip := make([]byte, 16)
		_, err = io.ReadFull(conn, ip)
		if err != nil {
			return "", err
		}
		host = net.IP(ip).String()
	default:
		return "", fmt.Errorf("unsupported address type: 0x%02x", buffer[3])
	}

	// Parse the destination port
	portBytes := make([]byte, 2)
	_, err = io.ReadFull(conn, portBytes)
	if err != nil {
		return "", err
	}
	port := binary.BigEndian.Uint16(portBytes)

	return fmt.Sprintf("%s:%d", host, port), nil
}

// sendSocks5Reply sends a SOCKS5 reply message with the specified reply code
func sendSocks5Reply(conn net.Conn, replyCode byte) {
	// Standard SOCKS5 reply format: VER REP RSV ATYP BND.ADDR BND.PORT
	reply := []byte{
		socks5Version,          // VER
		replyCode,              // REP
		0x00,                   // RSV (reserved)
		addrTypeIPv4,           // ATYP (IPv4)
		0x00, 0x00, 0x00, 0x00, // BND.ADDR (0.0.0.0)
		0x00, 0x00, // BND.PORT (0)
	}
	if _, err := conn.Write(reply); err != nil {
		logger.Error("Failed to write SOCKS5 reply: %v", err)
	}
}
