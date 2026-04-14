package tunnel

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	applogger "github.com/apeming/go-proxy-server/internal/logger"
)

type Client struct {
	ServerAddr       string
	Token            string
	TunnelName       string
	TargetAddr       string
	PublicPort       int
	DialTimeout      time.Duration
	ReconnectDelay   time.Duration
	HandshakeTimeout time.Duration
	TLSConfig        *tls.Config
	AllowInsecure    bool
	OnConnected      func(publicPort int)

	mu sync.RWMutex
}

type permanentError struct {
	err error
}

func (e *permanentError) Error() string {
	return e.err.Error()
}

func (e *permanentError) Unwrap() error {
	return e.err
}

func NewClient(serverAddr, token, tunnelName, targetAddr string, publicPort int) *Client {
	return &Client{
		ServerAddr:       serverAddr,
		Token:            token,
		TunnelName:       tunnelName,
		TargetAddr:       targetAddr,
		PublicPort:       publicPort,
		DialTimeout:      5 * time.Second,
		ReconnectDelay:   3 * time.Second,
		HandshakeTimeout: 10 * time.Second,
	}
}

func (c *Client) Run(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return err
	}

	for {
		err := c.runOnce()
		if ctx.Err() != nil {
			return nil
		}

		var fatal *permanentError
		if errors.As(err, &fatal) {
			return fatal.err
		}

		applogger.Warn("Tunnel client %s disconnected: %v", c.TunnelName, err)

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(c.ReconnectDelay):
		}
	}
}

func (c *Client) validate() error {
	if c.ServerAddr == "" {
		return fmt.Errorf("tunnel server address is required")
	}
	if c.Token == "" {
		return fmt.Errorf("tunnel token is required")
	}
	if c.TunnelName == "" {
		return fmt.Errorf("tunnel name is required")
	}
	if c.TargetAddr == "" {
		return fmt.Errorf("tunnel target address is required")
	}
	if c.PublicPort < 0 || c.PublicPort > 65535 {
		return fmt.Errorf("public port must be between 0 and 65535")
	}
	if c.DialTimeout <= 0 {
		c.DialTimeout = 5 * time.Second
	}
	if c.ReconnectDelay <= 0 {
		c.ReconnectDelay = 3 * time.Second
	}
	if c.HandshakeTimeout <= 0 {
		c.HandshakeTimeout = 10 * time.Second
	}
	if c.TLSConfig == nil && !c.AllowInsecure {
		return fmt.Errorf("tunnel client TLS config is required unless insecure mode is explicitly enabled")
	}
	return nil
}

func (c *Client) runOnce() error {
	conn, err := c.dialServer(context.Background())
	if err != nil {
		return fmt.Errorf("connect tunnel server: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(c.HandshakeTimeout))

	reader := bufio.NewReader(conn)
	if err := writeJSONLine(conn, handshake{
		Mode:       modeControl,
		Token:      c.Token,
		TunnelName: c.TunnelName,
		PublicPort: c.PublicPort,
	}); err != nil {
		return err
	}

	var resp response
	if err := readJSONLine(reader, &resp); err != nil {
		return fmt.Errorf("read tunnel registration response: %w", err)
	}
	if resp.Status != responseStatusOK {
		if isPermanentServerError(resp.Error) {
			return &permanentError{err: errors.New(resp.Error)}
		}
		return errors.New(resp.Error)
	}
	_ = conn.SetDeadline(time.Time{})

	c.setPublicPort(resp.PublicPort)
	if c.OnConnected != nil {
		c.OnConnected(resp.PublicPort)
	}
	applogger.Info("Tunnel client %s connected, public port %d -> %s", c.TunnelName, resp.PublicPort, c.TargetAddr)

	for {
		var msg controlMessage
		if err := readJSONLine(reader, &msg); err != nil {
			return err
		}
		if msg.Type != messageOpen || msg.ConnectionID == "" {
			continue
		}

		go c.handleOpen(msg.ConnectionID)
	}
}

func (c *Client) handleOpen(connectionID string) {
	localConn, err := net.DialTimeout("tcp", c.TargetAddr, c.DialTimeout)
	if err != nil {
		applogger.Warn("Tunnel %s failed to connect local target %s: %v", c.TunnelName, c.TargetAddr, err)
		return
	}

	dataConn, err := c.dialServer(context.Background())
	if err != nil {
		_ = localConn.Close()
		applogger.Warn("Tunnel %s failed to open data connection: %v", c.TunnelName, err)
		return
	}
	_ = dataConn.SetDeadline(time.Now().Add(c.HandshakeTimeout))

	reader := bufio.NewReader(dataConn)
	if err := writeJSONLine(dataConn, handshake{
		Mode:         modeData,
		Token:        c.Token,
		TunnelName:   c.TunnelName,
		ConnectionID: connectionID,
	}); err != nil {
		_ = localConn.Close()
		_ = dataConn.Close()
		applogger.Warn("Tunnel %s failed to send data handshake: %v", c.TunnelName, err)
		return
	}

	var resp response
	if err := readJSONLine(reader, &resp); err != nil {
		_ = localConn.Close()
		_ = dataConn.Close()
		applogger.Warn("Tunnel %s failed to read data handshake response: %v", c.TunnelName, err)
		return
	}
	if resp.Status != responseStatusOK {
		_ = localConn.Close()
		_ = dataConn.Close()
		applogger.Warn("Tunnel %s data connection rejected: %s", c.TunnelName, resp.Error)
		return
	}
	_ = dataConn.SetDeadline(time.Time{})

	relayConnections(localConn, dataConn)
}

func (c *Client) dialServer(ctx context.Context) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout:   c.DialTimeout,
		KeepAlive: 30 * time.Second,
	}
	if c.TLSConfig != nil {
		tlsDialer := &tls.Dialer{
			NetDialer: dialer,
			Config:    c.TLSConfig,
		}
		return tlsDialer.DialContext(ctx, "tcp", c.ServerAddr)
	}
	return dialer.DialContext(ctx, "tcp", c.ServerAddr)
}

func isPermanentServerError(msg string) bool {
	switch msg {
	case errInvalidTunnelToken,
		errTunnelNameRequired,
		"public port must be between 0 and 65535",
		errTunnelNameConnectionIDRequired,
		errUnsupportedTunnelMode:
		return true
	default:
		return false
	}
}

func (c *Client) setPublicPort(port int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.PublicPort = port
}

func (c *Client) GetPublicPort() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.PublicPort
}
