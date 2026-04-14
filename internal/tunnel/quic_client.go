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

	"github.com/quic-go/quic-go"

	"github.com/apeming/go-proxy-server/internal/activity"
	applogger "github.com/apeming/go-proxy-server/internal/logger"
)

type QUICManagedClient struct {
	ServerAddr        string
	Token             string
	ClientName        string
	DialTimeout       time.Duration
	ReconnectDelay    time.Duration
	HandshakeTimeout  time.Duration
	HeartbeatInterval time.Duration
	TLSConfig         *tls.Config
	OnConnected       func(clientName string)
	OnDisconnected    func(clientName string, err error)
	OnRoutesChanged   func(clientName string, routes []ManagedClientRoute)

	routesMu    sync.RWMutex
	routes      map[string]routeSync
	udpMu       sync.Mutex
	udpSessions map[string]*quicClientUDPSession
}

type quicClientUDPSession struct {
	client    *QUICManagedClient
	id        string
	routeName string
	route     routeSync
	conn      *net.UDPConn

	writeMu        sync.Mutex
	lastActivityMu sync.Mutex
	lastActivity   time.Time
	closeOnce      sync.Once
}

func NewQUICManagedClient(serverAddr, token, clientName string) *QUICManagedClient {
	return &QUICManagedClient{
		ServerAddr:        serverAddr,
		Token:             token,
		ClientName:        clientName,
		DialTimeout:       5 * time.Second,
		ReconnectDelay:    3 * time.Second,
		HandshakeTimeout:  10 * time.Second,
		HeartbeatInterval: 10 * time.Second,
		routes:            make(map[string]routeSync),
		udpSessions:       make(map[string]*quicClientUDPSession),
	}
}

func (c *QUICManagedClient) Run(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return err
	}
	for {
		err := c.runOnce(ctx)
		if ctx.Err() != nil {
			c.notifyDisconnected(nil)
			return nil
		}
		var fatal *permanentError
		if errors.As(err, &fatal) {
			c.notifyDisconnected(fatal.err)
			return fatal.err
		}
		c.notifyDisconnected(err)
		applogger.Warn("QUIC managed tunnel client %s disconnected: %v", c.ClientName, err)
		select {
		case <-ctx.Done():
			c.notifyDisconnected(nil)
			return nil
		case <-time.After(c.ReconnectDelay):
		}
	}
}

func (c *QUICManagedClient) validate() error {
	if c.ServerAddr == "" {
		return fmt.Errorf("tunnel server address is required")
	}
	if c.Token == "" {
		return fmt.Errorf("tunnel token is required")
	}
	if c.ClientName == "" {
		return fmt.Errorf("tunnel client name is required")
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
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = 10 * time.Second
	}
	if c.TLSConfig == nil {
		return fmt.Errorf("quic tunnel client TLS config is required")
	}
	return nil
}

func (c *QUICManagedClient) runOnce(ctx context.Context) error {
	conn, err := c.dialServer(ctx)
	if err != nil {
		return fmt.Errorf("connect quic tunnel server: %w", err)
	}
	defer conn.CloseWithError(0, "")

	streamCtx, cancel := context.WithTimeout(ctx, c.HandshakeTimeout)
	defer cancel()
	controlStream, err := conn.OpenStreamSync(streamCtx)
	if err != nil {
		return fmt.Errorf("open quic control stream: %w", err)
	}
	reader := bufio.NewReader(controlStream)
	if err := writeJSONLine(controlStream, quicControlRegister{Type: quicControlTypeRegister, Token: c.Token, ClientName: c.ClientName}); err != nil {
		return err
	}

	var ack quicControlAck
	if err := readJSONLine(reader, &ack); err != nil {
		return fmt.Errorf("read quic registration response: %w", err)
	}
	if ack.Status != responseStatusOK {
		if isPermanentManagedError(ack.Error) {
			return &permanentError{err: errors.New(ack.Error)}
		}
		return errors.New(ack.Error)
	}

	if c.OnConnected != nil {
		c.OnConnected(c.ClientName)
	}
	applogger.Info("QUIC managed tunnel client %s connected", c.ClientName)
	activity.RecordEvent(activity.EventRecord{
		Category:  "tunnel",
		EventType: "quic_client_control_connected",
		Severity:  activity.SeverityInfo,
		Source:    "quic_tunnel_client",
		Message:   fmt.Sprintf("QUIC managed tunnel client %s connected", c.ClientName),
		Details: map[string]any{
			"client_name": c.ClientName,
			"server_addr": c.ServerAddr,
		},
	})

	errorCh := make(chan error, 3)
	go c.acceptStreams(ctx, conn, errorCh)
	go c.receiveDatagrams(ctx, conn, errorCh)
	go c.heartbeatLoop(ctx, controlStream, errorCh)

	for {
		select {
		case <-ctx.Done():
			c.closeUDPSessions()
			return nil
		case err := <-errorCh:
			c.closeUDPSessions()
			return err
		default:
		}

		var msg controlMessage
		if err := readJSONLine(reader, &msg); err != nil {
			c.closeUDPSessions()
			return err
		}
		switch msg.Type {
		case messageSyncRoutes:
			c.replaceRoutes(msg.Routes)
		}
	}
}

func (c *QUICManagedClient) dialServer(ctx context.Context) (*quic.Conn, error) {
	tlsConfig := cloneQUICClientTLSConfig(c.TLSConfig)
	dialCtx, cancel := context.WithTimeout(ctx, c.DialTimeout)
	defer cancel()
	return quic.DialAddr(dialCtx, c.ServerAddr, tlsConfig, &quic.Config{
		EnableDatagrams: true,
		KeepAlivePeriod: c.HeartbeatInterval,
		MaxIdleTimeout:  2 * c.HeartbeatInterval,
	})
}

func (c *QUICManagedClient) heartbeatLoop(ctx context.Context, stream *quic.Stream, errorCh chan<- error) {
	ticker := time.NewTicker(c.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := writeJSONLine(stream, controlMessage{Type: messageHeartbeat}); err != nil {
				select {
				case errorCh <- err:
				default:
				}
				return
			}
		}
	}
}

func (c *QUICManagedClient) acceptStreams(ctx context.Context, conn *quic.Conn, errorCh chan<- error) {
	for {
		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			select {
			case errorCh <- err:
			default:
			}
			return
		}
		go c.handleStream(ctx, stream)
	}
}

func (c *QUICManagedClient) handleStream(ctx context.Context, stream *quic.Stream) {
	reader := bufio.NewReader(stream)
	var req quicTCPStreamOpenRequest
	if err := readJSONLine(reader, &req); err != nil {
		stream.CancelRead(0)
		stream.CancelWrite(0)
		return
	}
	if req.Type != quicStreamTypeTCP {
		_ = writeJSONLine(stream, quicControlAck{Type: quicControlTypeAck, Status: responseStatusError, Error: "unsupported stream type"})
		stream.CancelRead(0)
		stream.CancelWrite(0)
		return
	}

	route, ok := c.getRoute(req.RouteName)
	if !ok || !route.Enabled || route.TargetAddr == "" || normalizeManagedRouteProtocol(route.Protocol) != ProtocolTCP {
		_ = writeJSONLine(stream, quicControlAck{Type: quicControlTypeAck, Status: responseStatusError, Error: "route not available"})
		stream.CancelRead(0)
		stream.CancelWrite(0)
		return
	}

	localConn, err := net.DialTimeout("tcp", route.TargetAddr, c.DialTimeout)
	if err != nil {
		_ = writeJSONLine(stream, quicControlAck{Type: quicControlTypeAck, Status: responseStatusError, Error: err.Error()})
		stream.CancelRead(0)
		stream.CancelWrite(0)
		return
	}
	if err := writeJSONLine(stream, quicControlAck{Type: quicControlTypeAck, Status: responseStatusOK}); err != nil {
		_ = localConn.Close()
		stream.CancelRead(0)
		stream.CancelWrite(0)
		return
	}
	go relayNetConnAndQUICStream(localConn, stream, defaultTunnelIdleTimeout, relayObserver{})
}

func (c *QUICManagedClient) receiveDatagrams(ctx context.Context, conn *quic.Conn, errorCh chan<- error) {
	for {
		data, err := conn.ReceiveDatagram(ctx)
		if err != nil {
			select {
			case errorCh <- err:
			default:
			}
			return
		}
		if err := c.handleServerDatagramPayload(conn, data); err != nil {
			applogger.Warn("Handle QUIC UDP datagram failed: %v", err)
		}
	}
}

func (c *QUICManagedClient) handleServerDatagramPayload(conn *quic.Conn, data []byte) error {
	frame, err := unmarshalQUICUDPServerDatagram(data)
	if err != nil {
		applogger.Warn("Invalid QUIC UDP datagram from server: %v", err)
		return nil
	}
	return c.handleUDPDatagram(conn, frame)
}

func (c *QUICManagedClient) handleUDPDatagram(conn *quic.Conn, frame quicUDPServerDatagram) error {
	route, ok := c.getRoute(frame.RouteName)
	if !ok || !route.Enabled || route.TargetAddr == "" || normalizeManagedRouteProtocol(route.Protocol) != ProtocolUDP {
		payload, _ := marshalQUICUDPClientDatagram(quicUDPClientDatagram{SessionID: frame.SessionID, Close: true})
		_ = conn.SendDatagram(payload)
		return fmt.Errorf("udp route not available")
	}
	if len(frame.Payload) > route.UDPMaxPayload {
		payload, _ := marshalQUICUDPClientDatagram(quicUDPClientDatagram{SessionID: frame.SessionID, Close: true})
		_ = conn.SendDatagram(payload)
		return fmt.Errorf("udp payload exceeds configured limit")
	}

	session, err := c.getOrCreateUDPSession(conn, frame.SessionID, frame.RouteName, route)
	if err != nil {
		return err
	}
	if err := session.write(frame.Payload); err != nil {
		return err
	}
	session.touch()
	return nil
}

func (c *QUICManagedClient) getOrCreateUDPSession(conn *quic.Conn, sessionID, routeName string, route routeSync) (*quicClientUDPSession, error) {
	c.udpMu.Lock()
	defer c.udpMu.Unlock()
	if session := c.udpSessions[sessionID]; session != nil {
		return session, nil
	}
	targetAddr, err := net.ResolveUDPAddr("udp", route.TargetAddr)
	if err != nil {
		return nil, err
	}
	udpConn, err := net.DialUDP("udp", nil, targetAddr)
	if err != nil {
		return nil, err
	}
	session := &quicClientUDPSession{
		client:       c,
		id:           sessionID,
		routeName:    routeName,
		route:        route,
		conn:         udpConn,
		lastActivity: time.Now(),
	}
	c.udpSessions[sessionID] = session
	go session.readResponses(conn)
	return session, nil
}

func (c *QUICManagedClient) removeUDPSession(sessionID string) {
	c.udpMu.Lock()
	session := c.udpSessions[sessionID]
	delete(c.udpSessions, sessionID)
	c.udpMu.Unlock()
	if session != nil {
		session.close()
	}
}

func (c *QUICManagedClient) closeUDPSessions() {
	c.udpMu.Lock()
	sessions := make([]*quicClientUDPSession, 0, len(c.udpSessions))
	for id, session := range c.udpSessions {
		sessions = append(sessions, session)
		delete(c.udpSessions, id)
	}
	c.udpMu.Unlock()
	for _, session := range sessions {
		session.close()
	}
}

func (s *quicClientUDPSession) write(payload []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if _, err := s.conn.Write(payload); err != nil {
		return err
	}
	return nil
}

func (s *quicClientUDPSession) readResponses(conn *quic.Conn) {
	buffer := make([]byte, max(s.route.UDPMaxPayload, 2048))
	idleTimeout := time.Duration(normalizeManagedUDPIdleTimeout(s.route.UDPIdleTimeoutSec)) * time.Second
	for {
		_ = s.conn.SetReadDeadline(time.Now().Add(idleTimeout))
		n, err := s.conn.Read(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				if s.lastActivityBefore(time.Now().Add(-idleTimeout)) {
					break
				}
				continue
			}
			break
		}
		payload, marshalErr := marshalQUICUDPClientDatagram(quicUDPClientDatagram{SessionID: s.id, Payload: cloneBytes(buffer[:n])})
		if marshalErr != nil {
			break
		}
		if err := conn.SendDatagram(payload); err != nil {
			break
		}
		s.touch()
	}
	payload, _ := marshalQUICUDPClientDatagram(quicUDPClientDatagram{SessionID: s.id, Close: true})
	_ = conn.SendDatagram(payload)
	s.client.removeUDPSession(s.id)
}

func (s *quicClientUDPSession) touch() {
	s.lastActivityMu.Lock()
	s.lastActivity = time.Now()
	s.lastActivityMu.Unlock()
}

func (s *quicClientUDPSession) lastActivityBefore(deadline time.Time) bool {
	s.lastActivityMu.Lock()
	defer s.lastActivityMu.Unlock()
	return s.lastActivity.Before(deadline)
}

func (s *quicClientUDPSession) close() {
	s.closeOnce.Do(func() {
		if s.conn != nil {
			_ = s.conn.Close()
		}
	})
}

func (c *QUICManagedClient) replaceRoutes(routes []routeSync) {
	next := make(map[string]routeSync, len(routes))
	snapshot := make([]ManagedClientRoute, 0, len(routes))
	for _, route := range routes {
		route.Protocol = normalizeManagedRouteProtocol(route.Protocol)
		route.UDPIdleTimeoutSec = normalizeManagedUDPIdleTimeout(route.UDPIdleTimeoutSec)
		route.UDPMaxPayload = normalizeManagedUDPMaxPayload(route.UDPMaxPayload)
		next[route.Name] = route
		snapshot = append(snapshot, ManagedClientRoute{
			Name:              route.Name,
			TargetAddr:        route.TargetAddr,
			PublicPort:        route.PublicPort,
			Enabled:           route.Enabled,
			Protocol:          route.Protocol,
			UDPIdleTimeoutSec: route.UDPIdleTimeoutSec,
			UDPMaxPayload:     route.UDPMaxPayload,
		})
	}

	c.routesMu.Lock()
	c.routes = next
	c.routesMu.Unlock()
	if c.OnRoutesChanged != nil {
		c.OnRoutesChanged(c.ClientName, snapshot)
	}
}

func (c *QUICManagedClient) getRoute(name string) (routeSync, bool) {
	c.routesMu.RLock()
	defer c.routesMu.RUnlock()
	route, ok := c.routes[name]
	return route, ok
}

func (c *QUICManagedClient) notifyDisconnected(err error) {
	if c.OnDisconnected != nil {
		c.OnDisconnected(c.ClientName, err)
	}
}
