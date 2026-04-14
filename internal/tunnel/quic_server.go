package tunnel

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/apeming/go-proxy-server/internal/activity"
	"github.com/apeming/go-proxy-server/internal/constants"
	applogger "github.com/apeming/go-proxy-server/internal/logger"
	"gorm.io/gorm"
)

type QUICManagedServer struct {
	ControlAddr        string
	PublicBindAddr     string
	Token              string
	HandshakeTimeout   time.Duration
	SyncInterval       time.Duration
	HeartbeatInterval  time.Duration
	TLSConfig          *tls.Config
	AutoPortRangeStart int
	AutoPortRangeEnd   int
	Store              *ManagedStore

	listener  *quic.Listener
	errCh     chan error
	closeOnce sync.Once

	mu      sync.Mutex
	clients map[string]*quicManagedClientSession

	sessionsMu sync.RWMutex
	sessions   map[string]*managedSessionRecord
}

type quicManagedClientSession struct {
	server *QUICManagedServer
	name   string

	conn          *quic.Conn
	controlStream *quic.Stream
	remoteAddr    string

	writeMu         sync.Mutex
	datagramWriteMu sync.Mutex
	routesMu        sync.Mutex
	routes          map[string]quicManagedRouteBinding
	closed          bool
}

type quicManagedRouteBinding interface {
	close()
	publicPort() int
	setRoute(route ManagedRoute)
	getRoute() ManagedRoute
	handleClientDatagram(frame quicUDPClientDatagram) error
}

type quicManagedTCPRoute struct {
	session  *quicManagedClientSession
	listener net.Listener

	routeMu sync.RWMutex
	route   ManagedRoute

	closeOnce sync.Once
}

type quicManagedUDPRoute struct {
	session *quicManagedClientSession
	conn    *net.UDPConn

	routeMu sync.RWMutex
	route   ManagedRoute

	sessionsMu       sync.Mutex
	sessionsByID     map[string]*quicManagedUDPSession
	sessionsBySource map[string]*quicManagedUDPSession
	closeOnce        sync.Once
}

type quicManagedUDPSession struct {
	id         string
	sourceAddr *net.UDPAddr
	record     *managedSessionRecord

	lastActivityMu sync.Mutex
	lastActivity   time.Time
}

func NewQUICManagedServer(db *gorm.DB, controlAddr, publicBindAddr, token string) *QUICManagedServer {
	if controlAddr == "" {
		controlAddr = ":7000"
	}
	if publicBindAddr == "" {
		publicBindAddr = "0.0.0.0"
	}
	return &QUICManagedServer{
		ControlAddr:       controlAddr,
		PublicBindAddr:    publicBindAddr,
		Token:             token,
		HandshakeTimeout:  10 * time.Second,
		SyncInterval:      2 * time.Second,
		HeartbeatInterval: 10 * time.Second,
		Store:             NewManagedStore(db),
		errCh:             make(chan error, 1),
		clients:           make(map[string]*quicManagedClientSession),
		sessions:          make(map[string]*managedSessionRecord),
	}
}

func (s *QUICManagedServer) Start(ctx context.Context) error {
	if s.Token == "" {
		return fmt.Errorf("tunnel token is required")
	}
	if err := validateManagedAutoPortRange(s.AutoPortRangeStart, s.AutoPortRangeEnd); err != nil {
		return err
	}
	if s.HandshakeTimeout <= 0 {
		s.HandshakeTimeout = 10 * time.Second
	}
	if s.SyncInterval <= 0 {
		s.SyncInterval = 2 * time.Second
	}
	if s.HeartbeatInterval <= 0 {
		s.HeartbeatInterval = 10 * time.Second
	}
	if s.TLSConfig == nil {
		return fmt.Errorf("quic tunnel server TLS config is required")
	}
	if s.Store != nil {
		if err := s.Store.InitializeRuntimeState(); err != nil {
			return err
		}
	}

	tlsConfig := cloneQUICServerTLSConfig(s.TLSConfig)
	listener, err := quic.ListenAddr(s.ControlAddr, tlsConfig, &quic.Config{
		EnableDatagrams: true,
		KeepAlivePeriod: s.HeartbeatInterval,
		MaxIdleTimeout:  2 * s.HeartbeatInterval,
	})
	if err != nil {
		return fmt.Errorf("listen quic tunnel control address: %w", err)
	}
	addr := listener.Addr()
	s.listener = listener
	s.ControlAddr = addr.String()

	go func() {
		<-ctx.Done()
		s.close()
	}()
	go s.syncLoop(ctx)
	go func() {
		s.errCh <- s.serve(ctx)
	}()

	applogger.Info("QUIC managed tunnel server listening on %s", s.ControlAddr)
	return nil
}

func (s *QUICManagedServer) Wait() error {
	err := <-s.errCh
	if err == nil || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func (s *QUICManagedServer) GetControlAddr() string {
	return s.ControlAddr
}

func (s *QUICManagedServer) Engine() string {
	return EngineQUIC
}

func (s *QUICManagedServer) ListActiveSessions() []ManagedSessionSnapshot {
	s.sessionsMu.RLock()
	snapshots := make([]ManagedSessionSnapshot, 0, len(s.sessions))
	for _, record := range s.sessions {
		snapshots = append(snapshots, record.snapshot())
	}
	s.sessionsMu.RUnlock()

	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].OpenedAt.After(snapshots[j].OpenedAt)
	})
	return snapshots
}

func (s *QUICManagedServer) ActiveSessionCount() int {
	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()
	return len(s.sessions)
}

func (s *QUICManagedServer) trackActiveSession(record *managedSessionRecord) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	s.sessions[record.id] = record
}

func (s *QUICManagedServer) untrackActiveSession(sessionID string) {
	s.sessionsMu.Lock()
	delete(s.sessions, sessionID)
	s.sessionsMu.Unlock()
}

func (s *QUICManagedServer) serve(ctx context.Context) error {
	consecutiveErrors := 0
	for {
		conn, err := s.listener.Accept(ctx)
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return nil
			}
			consecutiveErrors++
			applogger.Error("QUIC managed tunnel accept error: %v", err)
			if consecutiveErrors >= constants.MaxConsecutiveAcceptErrors {
				return fmt.Errorf("quic managed tunnel server: too many consecutive accept errors")
			}
			time.Sleep(constants.AcceptErrorBackoff)
			continue
		}
		consecutiveErrors = 0
		go s.handleConn(ctx, conn)
	}
}

func (s *QUICManagedServer) handleConn(ctx context.Context, conn *quic.Conn) {
	streamCtx, cancel := context.WithTimeout(ctx, s.HandshakeTimeout)
	defer cancel()

	controlStream, err := conn.AcceptStream(streamCtx)
	if err != nil {
		_ = conn.CloseWithError(0, "control stream not established")
		return
	}
	reader := bufio.NewReader(controlStream)

	var hello quicControlRegister
	if err := readJSONLine(reader, &hello); err != nil {
		_ = writeJSONLine(controlStream, quicControlAck{Type: quicControlTypeAck, Status: responseStatusError, Error: err.Error()})
		_ = conn.CloseWithError(0, "invalid register request")
		return
	}
	if hello.Type != quicControlTypeRegister {
		_ = writeJSONLine(controlStream, quicControlAck{Type: quicControlTypeAck, Status: responseStatusError, Error: "unexpected control message"})
		_ = conn.CloseWithError(0, "unexpected control message")
		return
	}

	session, err := s.registerClient(conn, controlStream, hello)
	if err != nil {
		_ = writeJSONLine(controlStream, quicControlAck{Type: quicControlTypeAck, Status: responseStatusError, Error: err.Error()})
		_ = conn.CloseWithError(0, err.Error())
		return
	}
	defer s.unregisterClient(session)

	if err := writeJSONLine(controlStream, quicControlAck{Type: quicControlTypeAck, Status: responseStatusOK}); err != nil {
		_ = conn.CloseWithError(0, "failed to ack register")
		return
	}
	if err := s.syncSessionRoutes(session); err != nil {
		applogger.Warn("Initial QUIC tunnel route sync failed for %s: %v", session.name, err)
	}

	go s.receiveClientDatagrams(ctx, session)

	for {
		var msg controlMessage
		if err := readJSONLine(reader, &msg); err != nil {
			return
		}
		switch msg.Type {
		case messageHeartbeat:
			if s.Store != nil {
				_ = s.Store.UpsertClientHeartbeat(session.name, session.remoteAddr, EngineQUIC, true)
			}
		case messageSyncRoutes:
			continue
		}
	}
}

func (s *QUICManagedServer) registerClient(conn *quic.Conn, controlStream *quic.Stream, hello quicControlRegister) (*quicManagedClientSession, error) {
	if hello.Token != s.Token {
		return nil, fmt.Errorf(errInvalidTunnelToken)
	}
	if hello.ClientName == "" {
		return nil, fmt.Errorf(errClientNameRequired)
	}

	session := &quicManagedClientSession{
		server:        s,
		name:          hello.ClientName,
		conn:          conn,
		controlStream: controlStream,
		remoteAddr:    conn.RemoteAddr().String(),
		routes:        make(map[string]quicManagedRouteBinding),
	}

	s.mu.Lock()
	existing := s.clients[hello.ClientName]
	s.clients[hello.ClientName] = session
	s.mu.Unlock()
	if existing != nil {
		applogger.Warn("Replacing existing QUIC managed tunnel client %s", hello.ClientName)
		existing.close()
	}
	if s.Store != nil {
		if err := s.Store.UpsertClientHeartbeat(hello.ClientName, session.remoteAddr, EngineQUIC, true); err != nil {
			return nil, err
		}
	}

	activity.RecordEvent(activity.EventRecord{
		Category:  "tunnel",
		EventType: "quic_client_control_connected",
		Severity:  activity.SeverityInfo,
		Source:    "quic_tunnel_server",
		Message:   fmt.Sprintf("QUIC tunnel client %s connected", hello.ClientName),
		Details: map[string]any{
			"client_name": hello.ClientName,
			"remote_addr": session.remoteAddr,
		},
	})
	applogger.Info("QUIC managed tunnel client %s connected from %s", hello.ClientName, session.remoteAddr)
	return session, nil
}

func (s *QUICManagedServer) unregisterClient(session *quicManagedClientSession) {
	s.mu.Lock()
	if s.clients[session.name] == session {
		delete(s.clients, session.name)
	}
	s.mu.Unlock()
	session.close()
	if s.Store != nil {
		_ = s.Store.MarkClientDisconnected(session.name)
	}
	activity.RecordEvent(activity.EventRecord{
		Category:  "tunnel",
		EventType: "quic_client_control_disconnected",
		Severity:  activity.SeverityInfo,
		Source:    "quic_tunnel_server",
		Message:   fmt.Sprintf("QUIC tunnel client %s disconnected", session.name),
		Details: map[string]any{
			"client_name": session.name,
		},
	})
}

func (s *QUICManagedServer) receiveClientDatagrams(ctx context.Context, session *quicManagedClientSession) {
	for {
		data, err := session.conn.ReceiveDatagram(ctx)
		if err != nil {
			return
		}
		if err := s.handleClientDatagramPayload(session, data); err != nil {
			applogger.Warn("Handle QUIC client datagram for %s failed: %v", session.name, err)
		}
	}
}

func (s *QUICManagedServer) handleClientDatagramPayload(session *quicManagedClientSession, data []byte) error {
	frame, err := unmarshalQUICUDPClientDatagram(data)
	if err != nil {
		applogger.Warn("Invalid QUIC datagram from %s: %v", session.name, err)
		return nil
	}
	return session.handleClientDatagram(frame)
}

func (s *QUICManagedServer) syncLoop(ctx context.Context) {
	ticker := time.NewTicker(s.SyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.syncAllRoutes(); err != nil {
				applogger.Warn("QUIC managed route sync failed: %v", err)
			}
		}
	}
}

func (s *QUICManagedServer) syncAllRoutes() error {
	if s.Store == nil {
		return nil
	}
	desiredByClient, err := s.Store.ListDesiredRoutes()
	if err != nil {
		return err
	}

	s.mu.Lock()
	sessions := make([]*quicManagedClientSession, 0, len(s.clients))
	for _, session := range s.clients {
		sessions = append(sessions, session)
	}
	s.mu.Unlock()
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].name < sessions[j].name
	})

	for _, session := range sessions {
		if err := s.applyRoutes(session, desiredByClient[session.name]); err != nil {
			applogger.Warn("Apply QUIC managed routes for %s failed: %v", session.name, err)
		}
	}
	return nil
}

func (s *QUICManagedServer) syncSessionRoutes(session *quicManagedClientSession) error {
	if s.Store == nil {
		return nil
	}
	desiredByClient, err := s.Store.ListDesiredRoutes()
	if err != nil {
		return err
	}
	return s.applyRoutes(session, desiredByClient[session.name])
}

func (s *QUICManagedServer) applyRoutes(session *quicManagedClientSession, routes []ManagedRoute) error {
	desired := make(map[string]ManagedRoute, len(routes))
	for _, route := range routes {
		desired[route.Name] = route
	}

	session.routesMu.Lock()
	current := make(map[string]quicManagedRouteBinding, len(session.routes))
	for name, route := range session.routes {
		current[name] = route
	}
	session.routesMu.Unlock()

	for name, currentRoute := range current {
		currentConfig := currentRoute.getRoute()
		route, ok := desired[name]
		if !ok || !route.Enabled || route.TargetAddr == "" || route.PublicPort != currentConfig.PublicPort || route.Protocol != currentConfig.Protocol {
			currentRoute.close()
			session.routesMu.Lock()
			if session.routes[name] == currentRoute {
				delete(session.routes, name)
			}
			session.routesMu.Unlock()
			if s.Store != nil {
				_ = s.Store.UpdateRouteRuntime(session.name, name, 0, "")
			}
		}
	}

	for _, route := range routes {
		if !route.Enabled || route.TargetAddr == "" {
			if s.Store != nil {
				_ = s.Store.UpdateRouteRuntime(session.name, route.Name, 0, "")
			}
			continue
		}

		session.routesMu.Lock()
		binding := session.routes[route.Name]
		session.routesMu.Unlock()
		if binding != nil {
			binding.setRoute(route)
			if s.Store != nil {
				_ = s.Store.UpdateRouteRuntime(session.name, route.Name, binding.publicPort(), "")
			}
			continue
		}

		created, err := s.newRouteBinding(session, route)
		if err != nil {
			activity.RecordEvent(activity.EventRecord{
				Category:  "tunnel",
				EventType: "quic_route_expose_failed",
				Severity:  activity.SeverityWarn,
				Source:    "quic_tunnel_server",
				Message:   fmt.Sprintf("Failed to expose QUIC managed route %s/%s", session.name, route.Name),
				Details: map[string]any{
					"client_name": session.name,
					"route_name":  route.Name,
					"protocol":    route.Protocol,
					"public_port": route.PublicPort,
					"error":       err.Error(),
				},
			})
			if s.Store != nil {
				_ = s.Store.UpdateRouteRuntime(session.name, route.Name, 0, err.Error())
			}
			continue
		}

		session.routesMu.Lock()
		session.routes[route.Name] = created
		session.routesMu.Unlock()
		if s.Store != nil {
			_ = s.Store.UpdateRouteRuntime(session.name, route.Name, created.publicPort(), "")
		}
	}

	payload := make([]routeSync, 0, len(routes))
	for _, route := range routes {
		payload = append(payload, routeSync{
			Name:              route.Name,
			TargetAddr:        route.TargetAddr,
			PublicPort:        route.PublicPort,
			Enabled:           route.Enabled,
			Protocol:          route.Protocol,
			UDPIdleTimeoutSec: route.UDPIdleTimeoutSec,
			UDPMaxPayload:     route.UDPMaxPayload,
		})
	}
	return session.writeControl(controlMessage{Type: messageSyncRoutes, Routes: payload})
}

func (s *QUICManagedServer) newRouteBinding(session *quicManagedClientSession, route ManagedRoute) (quicManagedRouteBinding, error) {
	switch route.Protocol {
	case ProtocolTCP:
		listener, err := s.listenPublicTCPRoutePort(route)
		if err != nil {
			return nil, fmt.Errorf("listen public tcp port for route %s: %w", route.Name, err)
		}
		binding := &quicManagedTCPRoute{session: session, listener: listener, route: cloneManagedRoute(route)}
		go binding.acceptPublicConnections()
		applogger.Info("QUIC managed tunnel TCP route %s/%s exposed on public port %d", session.name, route.Name, binding.publicPort())
		return binding, nil
	case ProtocolUDP:
		conn, err := s.listenPublicUDPRoutePort(route)
		if err != nil {
			return nil, fmt.Errorf("listen public udp port for route %s: %w", route.Name, err)
		}
		binding := &quicManagedUDPRoute{
			session:          session,
			conn:             conn,
			route:            cloneManagedRoute(route),
			sessionsByID:     make(map[string]*quicManagedUDPSession),
			sessionsBySource: make(map[string]*quicManagedUDPSession),
		}
		go binding.serve()
		applogger.Info("QUIC managed tunnel UDP route %s/%s exposed on public port %d", session.name, route.Name, binding.publicPort())
		return binding, nil
	default:
		return nil, fmt.Errorf("unsupported route protocol")
	}
}

func (s *QUICManagedServer) listenPublicTCPRoutePort(route ManagedRoute) (net.Listener, error) {
	if route.PublicPort != 0 {
		return net.Listen("tcp", net.JoinHostPort(s.PublicBindAddr, strconv.Itoa(route.PublicPort)))
	}
	if route.AssignedPublicPort > 0 {
		return net.Listen("tcp", net.JoinHostPort(s.PublicBindAddr, strconv.Itoa(route.AssignedPublicPort)))
	}
	if s.AutoPortRangeStart == 0 && s.AutoPortRangeEnd == 0 {
		return net.Listen("tcp", net.JoinHostPort(s.PublicBindAddr, "0"))
	}
	return s.allocateNewTCPPort()
}

func (s *QUICManagedServer) allocateNewTCPPort() (net.Listener, error) {
	reserved := make(map[int]struct{})
	if s.Store != nil {
		ports, err := s.Store.GetAssignedPorts(ProtocolTCP)
		if err != nil {
			return nil, fmt.Errorf("get assigned tcp ports: %w", err)
		}
		for _, port := range ports {
			reserved[port] = struct{}{}
		}
	}
	for port := s.AutoPortRangeStart; port <= s.AutoPortRangeEnd; port++ {
		if _, ok := reserved[port]; ok {
			continue
		}
		listener, err := net.Listen("tcp", net.JoinHostPort(s.PublicBindAddr, strconv.Itoa(port)))
		if err == nil {
			return listener, nil
		}
		if isAddrInUseError(err) {
			continue
		}
		return nil, err
	}
	return nil, fmt.Errorf("no available public port in auto allocation range %d-%d", s.AutoPortRangeStart, s.AutoPortRangeEnd)
}

func (s *QUICManagedServer) listenPublicUDPRoutePort(route ManagedRoute) (*net.UDPConn, error) {
	if route.PublicPort != 0 {
		addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(s.PublicBindAddr, strconv.Itoa(route.PublicPort)))
		if err != nil {
			return nil, err
		}
		return net.ListenUDP("udp", addr)
	}
	if route.AssignedPublicPort > 0 {
		addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(s.PublicBindAddr, strconv.Itoa(route.AssignedPublicPort)))
		if err != nil {
			return nil, err
		}
		return net.ListenUDP("udp", addr)
	}
	if s.AutoPortRangeStart == 0 && s.AutoPortRangeEnd == 0 {
		addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(s.PublicBindAddr, "0"))
		if err != nil {
			return nil, err
		}
		return net.ListenUDP("udp", addr)
	}
	return s.allocateNewUDPPort()
}

func (s *QUICManagedServer) allocateNewUDPPort() (*net.UDPConn, error) {
	reserved := make(map[int]struct{})
	if s.Store != nil {
		ports, err := s.Store.GetAssignedPorts(ProtocolUDP)
		if err != nil {
			return nil, fmt.Errorf("get assigned udp ports: %w", err)
		}
		for _, port := range ports {
			reserved[port] = struct{}{}
		}
	}
	for port := s.AutoPortRangeStart; port <= s.AutoPortRangeEnd; port++ {
		if _, ok := reserved[port]; ok {
			continue
		}
		addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(s.PublicBindAddr, strconv.Itoa(port)))
		if err != nil {
			return nil, err
		}
		conn, err := net.ListenUDP("udp", addr)
		if err == nil {
			return conn, nil
		}
		if isAddrInUseError(err) {
			continue
		}
		return nil, err
	}
	return nil, fmt.Errorf("no available public udp port in auto allocation range %d-%d", s.AutoPortRangeStart, s.AutoPortRangeEnd)
}

func (s *QUICManagedServer) close() {
	s.closeOnce.Do(func() {
		if s.listener != nil {
			_ = s.listener.Close()
		}
		s.mu.Lock()
		sessions := make([]*quicManagedClientSession, 0, len(s.clients))
		for _, session := range s.clients {
			sessions = append(sessions, session)
		}
		s.clients = map[string]*quicManagedClientSession{}
		s.mu.Unlock()
		s.sessionsMu.Lock()
		s.sessions = map[string]*managedSessionRecord{}
		s.sessionsMu.Unlock()
		for _, session := range sessions {
			session.close()
			if s.Store != nil {
				_ = s.Store.MarkClientDisconnected(session.name)
			}
		}
	})
}

func (s *quicManagedClientSession) writeControl(msg controlMessage) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return writeJSONLine(s.controlStream, msg)
}

func (s *quicManagedClientSession) sendDatagram(payload []byte) error {
	s.datagramWriteMu.Lock()
	defer s.datagramWriteMu.Unlock()
	return s.conn.SendDatagram(payload)
}

func (s *quicManagedClientSession) handleClientDatagram(frame quicUDPClientDatagram) error {
	s.routesMu.Lock()
	bindings := make([]quicManagedRouteBinding, 0, len(s.routes))
	for _, binding := range s.routes {
		bindings = append(bindings, binding)
	}
	s.routesMu.Unlock()
	for _, binding := range bindings {
		if err := binding.handleClientDatagram(frame); err == nil {
			return nil
		}
	}
	return fmt.Errorf("udp session not found")
}

func (s *quicManagedClientSession) close() {
	s.routesMu.Lock()
	if s.closed {
		s.routesMu.Unlock()
		return
	}
	s.closed = true
	routes := make([]quicManagedRouteBinding, 0, len(s.routes))
	for _, route := range s.routes {
		routes = append(routes, route)
	}
	s.routes = map[string]quicManagedRouteBinding{}
	s.routesMu.Unlock()
	for _, route := range routes {
		route.close()
	}
	if s.controlStream != nil {
		_ = (*s.controlStream).Close()
		(*s.controlStream).CancelRead(0)
	}
	if s.conn != nil {
		_ = s.conn.CloseWithError(0, "")
	}
}

func (r *quicManagedTCPRoute) publicPort() int {
	if r.listener == nil {
		return 0
	}
	if tcpAddr, ok := r.listener.Addr().(*net.TCPAddr); ok {
		return tcpAddr.Port
	}
	return 0
}

func (r *quicManagedTCPRoute) setRoute(route ManagedRoute) {
	r.routeMu.Lock()
	defer r.routeMu.Unlock()
	r.route = cloneManagedRoute(route)
}

func (r *quicManagedTCPRoute) getRoute() ManagedRoute {
	r.routeMu.RLock()
	defer r.routeMu.RUnlock()
	return cloneManagedRoute(r.route)
}

func (r *quicManagedTCPRoute) handleClientDatagram(frame quicUDPClientDatagram) error {
	return fmt.Errorf("not udp route")
}

func (r *quicManagedTCPRoute) acceptPublicConnections() {
	consecutiveErrors := 0
	for {
		conn, err := r.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			consecutiveErrors++
			route := r.getRoute()
			applogger.Error("QUIC managed tunnel %s/%s public accept error: %v", r.session.name, route.Name, err)
			if consecutiveErrors >= constants.MaxConsecutiveAcceptErrors {
				return
			}
			time.Sleep(constants.AcceptErrorBackoff)
			continue
		}
		consecutiveErrors = 0
		if !isSourceAllowedForRoute(r.getRoute(), conn.RemoteAddr()) {
			_ = conn.Close()
			continue
		}
		go r.handlePublicConnection(conn)
	}
}

func (r *quicManagedTCPRoute) handlePublicConnection(publicConn net.Conn) {
	route := r.getRoute()
	connectionID, err := newManagedConnectionID()
	if err != nil {
		_ = publicConn.Close()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.session.server.HandshakeTimeout)
	defer cancel()
	stream, err := r.session.conn.OpenStreamSync(ctx)
	if err != nil {
		_ = publicConn.Close()
		return
	}
	reader := bufio.NewReader(stream)
	if err := writeJSONLine(stream, quicTCPStreamOpenRequest{
		Type:         quicStreamTypeTCP,
		RouteName:    route.Name,
		ConnectionID: connectionID,
		SourceAddr:   publicConn.RemoteAddr().String(),
		PublicPort:   r.publicPort(),
	}); err != nil {
		stream.CancelRead(0)
		stream.CancelWrite(0)
		_ = publicConn.Close()
		return
	}
	var ack quicControlAck
	if err := readJSONLine(reader, &ack); err != nil || ack.Status != responseStatusOK {
		stream.CancelRead(0)
		stream.CancelWrite(0)
		_ = publicConn.Close()
		return
	}

	sessionRecord := newManagedSessionRecord(connectionID, EngineQUIC, ProtocolTCP, r.session.name, route.Name, r.publicPort(), route.TargetAddr, publicConn.RemoteAddr().String())
	r.session.server.trackActiveSession(sessionRecord)
	go relayNetConnAndQUICStream(publicConn, stream, defaultTunnelIdleTimeout, relayObserver{
		OnBytes: func(direction relayDirection, n int) {
			switch direction {
			case relayDirectionLeftToRight:
				sessionRecord.addBytesFromPublic(n)
			case relayDirectionRightToLeft:
				sessionRecord.addBytesToPublic(n)
			}
		},
		OnClose: func() {
			r.session.server.untrackActiveSession(connectionID)
		},
	})
}

func (r *quicManagedTCPRoute) close() {
	r.closeOnce.Do(func() {
		if r.listener != nil {
			_ = r.listener.Close()
		}
	})
}

func (r *quicManagedUDPRoute) publicPort() int {
	if r.conn == nil {
		return 0
	}
	if udpAddr, ok := r.conn.LocalAddr().(*net.UDPAddr); ok {
		return udpAddr.Port
	}
	return 0
}

func (r *quicManagedUDPRoute) setRoute(route ManagedRoute) {
	r.routeMu.Lock()
	defer r.routeMu.Unlock()
	r.route = cloneManagedRoute(route)
}

func (r *quicManagedUDPRoute) getRoute() ManagedRoute {
	r.routeMu.RLock()
	defer r.routeMu.RUnlock()
	return cloneManagedRoute(r.route)
}

func (r *quicManagedUDPRoute) serve() {
	go r.cleanupLoop()
	buffer := make([]byte, max(r.getRoute().UDPMaxPayload, 2048))
	for {
		n, addr, err := r.conn.ReadFromUDP(buffer)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			applogger.Warn("QUIC managed UDP route %s/%s read error: %v", r.session.name, r.getRoute().Name, err)
			return
		}
		route := r.getRoute()
		if !isSourceAllowedForRoute(route, addr) {
			continue
		}
		if n > route.UDPMaxPayload {
			applogger.Warn("QUIC managed UDP route %s/%s dropped oversized packet from %s", r.session.name, route.Name, addr)
			continue
		}
		session := r.getOrCreateSession(addr, route)
		session.record.addPacketFromPublic(n)
		payload, err := marshalQUICUDPServerDatagram(quicUDPServerDatagram{
			SessionID:  session.id,
			RouteName:  route.Name,
			SourceAddr: addr.String(),
			Payload:    cloneBytes(buffer[:n]),
		})
		if err != nil {
			applogger.Warn("Marshal QUIC UDP datagram failed: %v", err)
			continue
		}
		if err := r.session.sendDatagram(payload); err != nil {
			applogger.Warn("Send QUIC UDP datagram failed: %v", err)
		}
		session.touch()
	}
}

func (r *quicManagedUDPRoute) getOrCreateSession(addr *net.UDPAddr, route ManagedRoute) *quicManagedUDPSession {
	r.sessionsMu.Lock()
	defer r.sessionsMu.Unlock()
	if session := r.sessionsBySource[addr.String()]; session != nil {
		return session
	}
	id, err := newManagedConnectionID()
	if err != nil {
		id = fmt.Sprintf("udp-%d", time.Now().UnixNano())
	}
	record := newManagedSessionRecord(id, EngineQUIC, ProtocolUDP, r.session.name, route.Name, r.publicPort(), route.TargetAddr, addr.String())
	r.session.server.trackActiveSession(record)
	session := &quicManagedUDPSession{id: id, sourceAddr: addr, record: record, lastActivity: time.Now()}
	r.sessionsByID[id] = session
	r.sessionsBySource[addr.String()] = session
	return session
}

func (r *quicManagedUDPRoute) handleClientDatagram(frame quicUDPClientDatagram) error {
	r.sessionsMu.Lock()
	session := r.sessionsByID[frame.SessionID]
	if frame.Close {
		if session != nil {
			delete(r.sessionsByID, frame.SessionID)
			delete(r.sessionsBySource, session.sourceAddr.String())
			r.session.server.untrackActiveSession(frame.SessionID)
		}
		r.sessionsMu.Unlock()
		return nil
	}
	r.sessionsMu.Unlock()
	if session == nil {
		return fmt.Errorf("udp session not found")
	}
	if _, err := r.conn.WriteToUDP(frame.Payload, session.sourceAddr); err != nil {
		return err
	}
	session.record.addPacketToPublic(len(frame.Payload))
	session.touch()
	return nil
}

func (r *quicManagedUDPRoute) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		route := r.getRoute()
		deadline := time.Now().Add(-time.Duration(route.UDPIdleTimeoutSec) * time.Second)
		var expired []string
		r.sessionsMu.Lock()
		for id, session := range r.sessionsByID {
			if session.lastActivityBefore(deadline) {
				expired = append(expired, id)
				delete(r.sessionsByID, id)
				delete(r.sessionsBySource, session.sourceAddr.String())
			}
		}
		r.sessionsMu.Unlock()
		for _, id := range expired {
			r.session.server.untrackActiveSession(id)
		}
		if r.conn == nil {
			return
		}
	}
}

func (r *quicManagedUDPRoute) close() {
	r.closeOnce.Do(func() {
		if r.conn != nil {
			_ = r.conn.Close()
		}
		r.sessionsMu.Lock()
		for id, session := range r.sessionsByID {
			delete(r.sessionsBySource, session.sourceAddr.String())
			delete(r.sessionsByID, id)
			r.session.server.untrackActiveSession(id)
		}
		r.sessionsMu.Unlock()
	})
}

func (s *quicManagedUDPSession) touch() {
	s.lastActivityMu.Lock()
	s.lastActivity = time.Now()
	s.lastActivityMu.Unlock()
}

func (s *quicManagedUDPSession) lastActivityBefore(deadline time.Time) bool {
	s.lastActivityMu.Lock()
	defer s.lastActivityMu.Unlock()
	return s.lastActivity.Before(deadline)
}

func cloneQUICServerTLSConfig(base *tls.Config) *tls.Config {
	cfg := base.Clone()
	cfg.NextProtos = []string{managedQUICALPN}
	cfg.MinVersion = tls.VersionTLS13
	return cfg
}

func cloneQUICClientTLSConfig(base *tls.Config) *tls.Config {
	cfg := base.Clone()
	cfg.NextProtos = []string{managedQUICALPN}
	cfg.MinVersion = tls.VersionTLS13
	return cfg
}

func relayNetConnAndQUICStream(left net.Conn, stream *quic.Stream, idleTimeout time.Duration, observer relayObserver) {
	defer left.Close()
	defer stream.CancelRead(0)
	defer stream.CancelWrite(0)
	if observer.OnClose != nil {
		defer observer.OnClose()
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		copyNetToQUICStream(stream, left, idleTimeout, relayDirectionLeftToRight, observer)
		_ = stream.Close()
		closeRead(left)
	}()

	go func() {
		defer wg.Done()
		copyQUICStreamToNet(left, stream, idleTimeout, relayDirectionRightToLeft, observer)
		closeWrite(left)
		stream.CancelRead(0)
	}()

	wg.Wait()
}

func copyNetToQUICStream(dst *quic.Stream, src net.Conn, idleTimeout time.Duration, direction relayDirection, observer relayObserver) {
	buf := relayBufferPool.Get().([]byte)
	defer relayBufferPool.Put(buf)
	for {
		if idleTimeout > 0 {
			_ = src.SetReadDeadline(time.Now().Add(idleTimeout))
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			if idleTimeout > 0 {
				_ = dst.SetWriteDeadline(time.Now().Add(idleTimeout))
			}
			if _, err := dst.Write(buf[:n]); err != nil {
				return
			}
			if observer.OnBytes != nil {
				observer.OnBytes(direction, n)
			}
		}
		if readErr != nil {
			return
		}
	}
}

func copyQUICStreamToNet(dst net.Conn, src *quic.Stream, idleTimeout time.Duration, direction relayDirection, observer relayObserver) {
	buf := relayBufferPool.Get().([]byte)
	defer relayBufferPool.Put(buf)
	for {
		if idleTimeout > 0 {
			_ = src.SetReadDeadline(time.Now().Add(idleTimeout))
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			if idleTimeout > 0 {
				_ = dst.SetWriteDeadline(time.Now().Add(idleTimeout))
			}
			if err := writeAll(dst, buf[:n]); err != nil {
				return
			}
			if observer.OnBytes != nil {
				observer.OnBytes(direction, n)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return
			}
			return
		}
	}
}

func isSourceAllowedForRoute(route ManagedRoute, addr net.Addr) bool {
	if len(route.IPWhitelist) == 0 {
		return true
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return false
	}
	sourceIP := net.ParseIP(host)
	if sourceIP == nil {
		return false
	}
	for _, entry := range route.IPWhitelist {
		if entry == "" {
			continue
		}
		if _, network, err := net.ParseCIDR(entry); err == nil {
			if network.Contains(sourceIP) {
				return true
			}
			continue
		}
		allowedIP := net.ParseIP(entry)
		if allowedIP != nil && allowedIP.Equal(sourceIP) {
			return true
		}
	}
	return false
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
