package tunnel

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/apeming/go-proxy-server/internal/activity"
	"github.com/apeming/go-proxy-server/internal/constants"
	applogger "github.com/apeming/go-proxy-server/internal/logger"
)

type Server struct {
	ControlAddr           string
	PublicBindAddr        string
	Token                 string
	PendingTimeout        time.Duration
	HandshakeTimeout      time.Duration
	MaxPendingConnections int
	TLSConfig             *tls.Config
	AllowInsecure         bool

	listener  net.Listener
	errCh     chan error
	closeOnce sync.Once

	mu      sync.Mutex
	tunnels map[string]*serverTunnel
}

type serverTunnel struct {
	server         *Server
	name           string
	publicPort     int
	publicListener net.Listener
	controlConn    net.Conn

	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[string]net.Conn
	closeOnce sync.Once
}

func NewServer(controlAddr, publicBindAddr, token string) *Server {
	if controlAddr == "" {
		controlAddr = ":7000"
	}
	if publicBindAddr == "" {
		publicBindAddr = "0.0.0.0"
	}

	return &Server{
		ControlAddr:           controlAddr,
		PublicBindAddr:        publicBindAddr,
		Token:                 token,
		PendingTimeout:        10 * time.Second,
		HandshakeTimeout:      10 * time.Second,
		MaxPendingConnections: 128,
		errCh:                 make(chan error, 1),
		tunnels:               make(map[string]*serverTunnel),
	}
}

func (s *Server) Start(ctx context.Context) error {
	if s.Token == "" {
		return fmt.Errorf("tunnel token is required")
	}
	if s.HandshakeTimeout <= 0 {
		s.HandshakeTimeout = 10 * time.Second
	}
	if s.MaxPendingConnections <= 0 {
		s.MaxPendingConnections = 128
	}
	if s.TLSConfig == nil && !s.AllowInsecure {
		return fmt.Errorf("tunnel server TLS config is required unless insecure mode is explicitly enabled")
	}

	rawListener, err := net.Listen("tcp", s.ControlAddr)
	if err != nil {
		return fmt.Errorf("listen tunnel control address: %w", err)
	}
	var listener net.Listener = rawListener
	if s.TLSConfig != nil {
		listener = tls.NewListener(rawListener, s.TLSConfig)
	}

	s.listener = listener
	s.ControlAddr = rawListener.Addr().String()

	go func() {
		<-ctx.Done()
		s.close()
	}()

	go func() {
		s.errCh <- s.serve()
	}()

	applogger.Info("Tunnel server listening on %s", s.ControlAddr)
	return nil
}

func (s *Server) Wait() error {
	err := <-s.errCh
	if err == nil || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func (s *Server) serve() error {
	consecutiveErrors := 0
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}

			consecutiveErrors++
			applogger.Error("Tunnel control accept error: %v", err)
			if consecutiveErrors >= constants.MaxConsecutiveAcceptErrors {
				return fmt.Errorf("tunnel server: too many consecutive accept errors")
			}
			time.Sleep(constants.AcceptErrorBackoff)
			continue
		}

		consecutiveErrors = 0
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	_ = conn.SetDeadline(time.Now().Add(s.HandshakeTimeout))
	reader := bufio.NewReader(conn)

	var hs handshake
	if err := readJSONLine(reader, &hs); err != nil {
		applogger.Warn("Tunnel handshake failed from %s: %v", conn.RemoteAddr(), err)
		_ = conn.Close()
		return
	}

	switch hs.Mode {
	case modeControl:
		s.handleControlConn(conn, reader, hs)
	case modeData:
		s.handleDataConn(conn, hs)
	default:
		_ = writeJSONLine(conn, response{Status: responseStatusError, Error: errUnsupportedTunnelMode})
		_ = conn.Close()
	}
}

func (s *Server) handleControlConn(conn net.Conn, reader *bufio.Reader, hs handshake) {
	tunnel, err := s.registerTunnel(conn, hs)
	if err != nil {
		_ = writeJSONLine(conn, response{Status: responseStatusError, Error: err.Error()})
		_ = conn.Close()
		return
	}
	defer s.unregisterTunnel(tunnel)

	if err := writeJSONLine(conn, response{Status: responseStatusOK, TunnelName: tunnel.name, PublicPort: tunnel.publicPort}); err != nil {
		applogger.Warn("Failed to acknowledge tunnel %s: %v", tunnel.name, err)
		return
	}
	_ = conn.SetDeadline(time.Time{})

	for {
		var msg controlMessage
		if err := readJSONLine(reader, &msg); err != nil {
			return
		}
	}
}

func (s *Server) registerTunnel(conn net.Conn, hs handshake) (*serverTunnel, error) {
	if hs.Token != s.Token {
		return nil, fmt.Errorf(errInvalidTunnelToken)
	}
	if hs.TunnelName == "" {
		return nil, fmt.Errorf(errTunnelNameRequired)
	}
	if hs.PublicPort < 0 || hs.PublicPort > 65535 {
		return nil, fmt.Errorf("public port must be between 0 and 65535")
	}

	s.mu.Lock()
	existing := s.tunnels[hs.TunnelName]
	if existing != nil {
		delete(s.tunnels, hs.TunnelName)
	}
	s.mu.Unlock()
	if existing != nil {
		applogger.Warn("Replacing existing tunnel registration for %s", hs.TunnelName)
		existing.close()
	}

	listener, err := net.Listen("tcp", net.JoinHostPort(s.PublicBindAddr, strconv.Itoa(hs.PublicPort)))
	if err != nil {
		return nil, fmt.Errorf("listen public port: %w", err)
	}

	tunnel := &serverTunnel{
		server:         s,
		name:           hs.TunnelName,
		publicPort:     listener.Addr().(*net.TCPAddr).Port,
		publicListener: listener,
		controlConn:    conn,
		pending:        make(map[string]net.Conn),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if current := s.tunnels[hs.TunnelName]; current != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("tunnel name registration changed while reconnecting: %s", hs.TunnelName)
	}
	s.tunnels[hs.TunnelName] = tunnel

	go tunnel.acceptPublicConnections()
	applogger.Info("Tunnel %s exposed on public port %d", tunnel.name, tunnel.publicPort)
	activity.RecordEvent(activity.EventRecord{
		Category:  "tunnel",
		EventType: "legacy_tunnel_exposed",
		Severity:  activity.SeverityInfo,
		Source:    "tunnel_server",
		Message:   fmt.Sprintf("Tunnel %s exposed", tunnel.name),
		Details: map[string]any{
			"tunnel_name": tunnel.name,
			"public_port": tunnel.publicPort,
		},
	})
	return tunnel, nil
}

func (s *Server) unregisterTunnel(tunnel *serverTunnel) {
	s.mu.Lock()
	if current, ok := s.tunnels[tunnel.name]; ok && current == tunnel {
		delete(s.tunnels, tunnel.name)
	}
	s.mu.Unlock()

	tunnel.close()
	applogger.Info("Tunnel %s stopped", tunnel.name)
	activity.RecordEvent(activity.EventRecord{
		Category:  "tunnel",
		EventType: "legacy_tunnel_stopped",
		Severity:  activity.SeverityInfo,
		Source:    "tunnel_server",
		Message:   fmt.Sprintf("Tunnel %s stopped", tunnel.name),
		Details: map[string]any{
			"tunnel_name": tunnel.name,
		},
	})
}

func (s *Server) getTunnel(name string) *serverTunnel {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tunnels[name]
}

func (s *Server) close() {
	s.closeOnce.Do(func() {
		if s.listener != nil {
			_ = s.listener.Close()
		}

		s.mu.Lock()
		tunnels := make([]*serverTunnel, 0, len(s.tunnels))
		for _, tunnel := range s.tunnels {
			tunnels = append(tunnels, tunnel)
		}
		s.mu.Unlock()

		for _, tunnel := range tunnels {
			tunnel.close()
		}
	})
}

func (t *serverTunnel) acceptPublicConnections() {
	consecutiveErrors := 0
	for {
		conn, err := t.publicListener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}

			consecutiveErrors++
			applogger.Error("Tunnel %s public accept error: %v", t.name, err)
			if consecutiveErrors >= constants.MaxConsecutiveAcceptErrors {
				return
			}
			time.Sleep(constants.AcceptErrorBackoff)
			continue
		}

		consecutiveErrors = 0
		if err := t.openPendingConnection(conn); err != nil {
			applogger.Warn("Tunnel %s failed to open pending connection: %v", t.name, err)
			_ = conn.Close()
		}
	}
}

func (t *serverTunnel) openPendingConnection(publicConn net.Conn) error {
	t.pendingMu.Lock()
	if len(t.pending) >= t.server.MaxPendingConnections {
		t.pendingMu.Unlock()
		return fmt.Errorf("too many pending public connections")
	}
	t.pendingMu.Unlock()

	connectionID, err := newConnectionID()
	if err != nil {
		return err
	}

	t.pendingMu.Lock()
	t.pending[connectionID] = publicConn
	t.pendingMu.Unlock()

	if err := t.writeControl(controlMessage{Type: messageOpen, ConnectionID: connectionID}); err != nil {
		t.pendingMu.Lock()
		delete(t.pending, connectionID)
		t.pendingMu.Unlock()
		return err
	}

	go t.expirePendingConnection(connectionID)
	return nil
}

func (t *serverTunnel) expirePendingConnection(connectionID string) {
	timer := time.NewTimer(t.server.PendingTimeout)
	defer timer.Stop()

	<-timer.C

	t.pendingMu.Lock()
	publicConn, ok := t.pending[connectionID]
	if ok {
		delete(t.pending, connectionID)
	}
	t.pendingMu.Unlock()

	if ok {
		_ = publicConn.Close()
	}
}

func (s *Server) handleDataConn(conn net.Conn, hs handshake) {
	if hs.Token != s.Token {
		_ = writeJSONLine(conn, response{Status: responseStatusError, Error: errInvalidTunnelToken})
		_ = conn.Close()
		return
	}
	if hs.TunnelName == "" || hs.ConnectionID == "" {
		_ = writeJSONLine(conn, response{Status: responseStatusError, Error: errTunnelNameConnectionIDRequired})
		_ = conn.Close()
		return
	}

	tunnel := s.getTunnel(hs.TunnelName)
	if tunnel == nil {
		_ = writeJSONLine(conn, response{Status: responseStatusError, Error: "tunnel not found"})
		_ = conn.Close()
		return
	}

	publicConn := tunnel.takePending(hs.ConnectionID)
	if publicConn == nil {
		_ = writeJSONLine(conn, response{Status: responseStatusError, Error: "pending connection not found"})
		_ = conn.Close()
		return
	}

	if err := writeJSONLine(conn, response{Status: responseStatusOK}); err != nil {
		_ = publicConn.Close()
		_ = conn.Close()
		return
	}
	_ = conn.SetDeadline(time.Time{})

	go relayConnections(publicConn, conn)
}

func (t *serverTunnel) takePending(connectionID string) net.Conn {
	t.pendingMu.Lock()
	defer t.pendingMu.Unlock()

	publicConn := t.pending[connectionID]
	delete(t.pending, connectionID)
	return publicConn
}

func (t *serverTunnel) writeControl(msg controlMessage) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	return writeJSONLine(t.controlConn, msg)
}

func (t *serverTunnel) close() {
	t.closeOnce.Do(func() {
		if t.publicListener != nil {
			_ = t.publicListener.Close()
		}
		if t.controlConn != nil {
			_ = t.controlConn.Close()
		}

		t.pendingMu.Lock()
		for id, conn := range t.pending {
			_ = conn.Close()
			delete(t.pending, id)
		}
		t.pendingMu.Unlock()
	})
}

func newConnectionID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate connection ID: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
