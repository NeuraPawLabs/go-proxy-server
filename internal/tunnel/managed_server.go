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
	"sort"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/apeming/go-proxy-server/internal/activity"
	"github.com/apeming/go-proxy-server/internal/constants"
	applogger "github.com/apeming/go-proxy-server/internal/logger"
	"gorm.io/gorm"
)

type ManagedServer struct {
	ControlAddr           string
	PublicBindAddr        string
	Token                 string
	PendingTimeout        time.Duration
	HandshakeTimeout      time.Duration
	SyncInterval          time.Duration
	HeartbeatInterval     time.Duration
	MaxPendingConnections int
	TLSConfig             *tls.Config
	AllowInsecure         bool
	AutoPortRangeStart    int
	AutoPortRangeEnd      int
	Store                 *ManagedStore

	listener  net.Listener
	errCh     chan error
	closeOnce sync.Once

	mu      sync.Mutex
	clients map[string]*managedClientSession

	sessionsMu sync.RWMutex
	sessions   map[string]*managedSessionRecord
}

type managedClientSession struct {
	server      *ManagedServer
	name        string
	remoteAddr  string
	controlConn net.Conn

	writeMu  sync.Mutex
	routesMu sync.Mutex
	routes   map[string]*managedRouteListener
	closed   bool
}

type managedRouteListener struct {
	session  *managedClientSession
	listener net.Listener

	routeMu sync.RWMutex
	route   ManagedRoute

	pendingMu sync.Mutex
	pending   map[string]net.Conn
	closeOnce sync.Once
}

func NewManagedServer(db *gorm.DB, controlAddr, publicBindAddr, token string) *ManagedServer {
	if controlAddr == "" {
		controlAddr = ":7000"
	}
	if publicBindAddr == "" {
		publicBindAddr = "0.0.0.0"
	}
	return &ManagedServer{
		ControlAddr:           controlAddr,
		PublicBindAddr:        publicBindAddr,
		Token:                 token,
		PendingTimeout:        10 * time.Second,
		HandshakeTimeout:      10 * time.Second,
		SyncInterval:          2 * time.Second,
		HeartbeatInterval:     10 * time.Second,
		MaxPendingConnections: 128,
		Store:                 NewManagedStore(db),
		errCh:                 make(chan error, 1),
		clients:               make(map[string]*managedClientSession),
		sessions:              make(map[string]*managedSessionRecord),
	}
}

func (s *ManagedServer) Start(ctx context.Context) error {
	if s.Token == "" {
		return fmt.Errorf("tunnel token is required")
	}
	if err := validateManagedAutoPortRange(s.AutoPortRangeStart, s.AutoPortRangeEnd); err != nil {
		return err
	}
	if s.HandshakeTimeout <= 0 {
		s.HandshakeTimeout = 10 * time.Second
	}
	if s.PendingTimeout <= 0 {
		s.PendingTimeout = 10 * time.Second
	}
	if s.SyncInterval <= 0 {
		s.SyncInterval = 2 * time.Second
	}
	if s.HeartbeatInterval <= 0 {
		s.HeartbeatInterval = 10 * time.Second
	}
	if s.MaxPendingConnections <= 0 {
		s.MaxPendingConnections = 128
	}
	if s.TLSConfig == nil && !s.AllowInsecure {
		return fmt.Errorf("tunnel server TLS config is required unless insecure mode is explicitly enabled")
	}
	if s.Store != nil {
		if err := s.Store.InitializeRuntimeState(); err != nil {
			return err
		}
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
	go s.syncLoop(ctx)
	go func() {
		s.errCh <- s.serve()
	}()

	applogger.Info("Managed tunnel server listening on %s", s.ControlAddr)
	return nil
}

func (s *ManagedServer) Wait() error {
	err := <-s.errCh
	if err == nil || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func (s *ManagedServer) GetControlAddr() string {
	return s.ControlAddr
}

func (s *ManagedServer) Engine() string {
	return EngineClassic
}

func (s *ManagedServer) serve() error {
	consecutiveErrors := 0
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			consecutiveErrors++
			applogger.Error("Managed tunnel accept error: %v", err)
			if consecutiveErrors >= constants.MaxConsecutiveAcceptErrors {
				return fmt.Errorf("managed tunnel server: too many consecutive accept errors")
			}
			time.Sleep(constants.AcceptErrorBackoff)
			continue
		}
		consecutiveErrors = 0
		go s.handleConn(conn)
	}
}

func (s *ManagedServer) handleConn(conn net.Conn) {
	_ = conn.SetDeadline(time.Now().Add(s.HandshakeTimeout))
	reader := bufio.NewReader(conn)

	var hs handshake
	if err := readJSONLine(reader, &hs); err != nil {
		applogger.Warn("Managed tunnel handshake failed from %s: %v", conn.RemoteAddr(), err)
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

func (s *ManagedServer) handleControlConn(conn net.Conn, reader *bufio.Reader, hs handshake) {
	session, err := s.registerClient(conn, hs)
	if err != nil {
		_ = writeJSONLine(conn, response{Status: responseStatusError, Error: err.Error()})
		_ = conn.Close()
		return
	}
	defer s.unregisterClient(session)

	if err := writeJSONLine(conn, response{Status: responseStatusOK}); err != nil {
		applogger.Warn("Managed tunnel ack failed for client %s: %v", session.name, err)
		return
	}
	_ = conn.SetDeadline(time.Time{})

	if err := s.syncSessionRoutes(session); err != nil {
		applogger.Warn("Initial tunnel route sync failed for %s: %v", session.name, err)
	}

	for {
		var msg controlMessage
		if err := readJSONLine(reader, &msg); err != nil {
			return
		}
		if msg.Type == messageSyncRoutes {
			continue
		}
	}
}

func (s *ManagedServer) registerClient(conn net.Conn, hs handshake) (*managedClientSession, error) {
	if hs.Token != s.Token {
		return nil, fmt.Errorf(errInvalidTunnelToken)
	}
	clientName := hs.ClientName
	if clientName == "" {
		clientName = hs.TunnelName
	}
	if clientName == "" {
		return nil, fmt.Errorf(errClientNameRequired)
	}

	session := &managedClientSession{
		server:      s,
		name:        clientName,
		remoteAddr:  conn.RemoteAddr().String(),
		controlConn: conn,
		routes:      make(map[string]*managedRouteListener),
	}

	s.mu.Lock()
	existing := s.clients[clientName]
	s.clients[clientName] = session
	s.mu.Unlock()
	if existing != nil {
		applogger.Warn("Replacing existing managed tunnel client %s", clientName)
		existing.close()
	}

	if s.Store != nil {
		if err := s.Store.UpsertClientHeartbeat(clientName, session.remoteAddr, EngineClassic, true); err != nil {
			applogger.Warn("Failed to update tunnel client heartbeat for %s: %v", clientName, err)
		}
	}
	go s.heartbeatLoop(session)
	applogger.Info("Managed tunnel client %s connected from %s", clientName, session.remoteAddr)
	activity.RecordEvent(activity.EventRecord{
		Category:  "tunnel",
		EventType: "managed_client_connected",
		Severity:  activity.SeverityInfo,
		Source:    "managed_tunnel_server",
		Message:   fmt.Sprintf("Managed tunnel client %s connected", clientName),
		Details: map[string]any{
			"client_name":    clientName,
			"remote_addr":    session.remoteAddr,
			"allow_insecure": s.AllowInsecure,
		},
	})
	return session, nil
}

func (s *ManagedServer) unregisterClient(session *managedClientSession) {
	wasActive := false
	s.mu.Lock()
	if current, ok := s.clients[session.name]; ok && current == session {
		delete(s.clients, session.name)
		wasActive = true
	}
	s.mu.Unlock()

	session.close()
	if wasActive && s.Store != nil {
		if err := s.Store.MarkClientDisconnected(session.name); err != nil {
			applogger.Warn("Failed to mark tunnel client %s disconnected: %v", session.name, err)
		}
	}
	if wasActive {
		applogger.Info("Managed tunnel client %s disconnected", session.name)
		activity.RecordEvent(activity.EventRecord{
			Category:  "tunnel",
			EventType: "managed_client_disconnected",
			Severity:  activity.SeverityInfo,
			Source:    "managed_tunnel_server",
			Message:   fmt.Sprintf("Managed tunnel client %s disconnected", session.name),
			Details: map[string]any{
				"client_name": session.name,
			},
		})
	}
}

func (s *ManagedServer) heartbeatLoop(session *managedClientSession) {
	ticker := time.NewTicker(s.HeartbeatInterval)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		current := s.clients[session.name]
		s.mu.Unlock()
		if current != session {
			return
		}
		if s.Store != nil {
			if err := s.Store.UpsertClientHeartbeat(session.name, session.remoteAddr, EngineClassic, true); err != nil {
				applogger.Warn("Failed to refresh tunnel client heartbeat for %s: %v", session.name, err)
			}
		}
	}
}

func (s *ManagedServer) syncLoop(ctx context.Context) {
	ticker := time.NewTicker(s.SyncInterval)
	defer ticker.Stop()
	for {
		if err := s.syncAllRoutes(); err != nil {
			applogger.Warn("Managed tunnel route sync failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *ManagedServer) syncAllRoutes() error {
	if s.Store == nil {
		return nil
	}
	desiredByClient, err := s.Store.ListDesiredRoutes()
	if err != nil {
		return err
	}

	s.mu.Lock()
	sessions := make([]*managedClientSession, 0, len(s.clients))
	for _, session := range s.clients {
		sessions = append(sessions, session)
	}
	s.mu.Unlock()
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].name < sessions[j].name
	})

	for _, session := range sessions {
		if err := s.applyRoutes(session, desiredByClient[session.name]); err != nil {
			applogger.Warn("Apply managed routes for %s failed: %v", session.name, err)
		}
	}
	return nil
}

func (s *ManagedServer) syncSessionRoutes(session *managedClientSession) error {
	if s.Store == nil {
		return nil
	}
	desiredByClient, err := s.Store.ListDesiredRoutes()
	if err != nil {
		return err
	}
	return s.applyRoutes(session, desiredByClient[session.name])
}

func (s *ManagedServer) applyRoutes(session *managedClientSession, routes []ManagedRoute) error {
	desired := make(map[string]ManagedRoute, len(routes))
	for _, route := range routes {
		desired[route.Name] = route
	}

	session.routesMu.Lock()
	current := make(map[string]*managedRouteListener, len(session.routes))
	for name, route := range session.routes {
		current[name] = route
	}
	session.routesMu.Unlock()

	for name, currentRoute := range current {
		currentConfig := currentRoute.getRoute()
		route, ok := desired[name]
		if !ok || !route.Enabled || route.TargetAddr == "" || route.PublicPort != currentConfig.PublicPort {
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
		if normalizeManagedRouteProtocol(route.Protocol) != ProtocolTCP {
			if s.Store != nil {
				_ = s.Store.UpdateRouteRuntime(session.name, route.Name, 0, "classic tunnel engine only supports TCP routes")
			}
			continue
		}

		session.routesMu.Lock()
		listener := session.routes[route.Name]
		session.routesMu.Unlock()
		if listener != nil {
			listener.setRoute(route)
			if s.Store != nil {
				_ = s.Store.UpdateRouteRuntime(session.name, route.Name, listener.publicPort(), "")
			}
			continue
		}

		created, err := s.newRouteListener(session, route)
		if err != nil {
			activity.RecordEvent(activity.EventRecord{
				Category:  "tunnel",
				EventType: "managed_route_expose_failed",
				Severity:  activity.SeverityWarn,
				Source:    "managed_tunnel_server",
				Message:   fmt.Sprintf("Failed to expose managed tunnel route %s/%s", session.name, route.Name),
				Details: map[string]any{
					"client_name": session.name,
					"route_name":  route.Name,
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
		go created.acceptPublicConnections()
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
			Enabled:           route.Enabled && normalizeManagedRouteProtocol(route.Protocol) == ProtocolTCP,
			Protocol:          ProtocolTCP,
			UDPIdleTimeoutSec: route.UDPIdleTimeoutSec,
			UDPMaxPayload:     route.UDPMaxPayload,
		})
	}
	return session.writeControl(controlMessage{Type: messageSyncRoutes, Routes: payload})
}

func (s *ManagedServer) newRouteListener(session *managedClientSession, route ManagedRoute) (*managedRouteListener, error) {
	listener, err := s.listenPublicRoutePort(route)
	if err != nil {
		return nil, fmt.Errorf("listen public port for route %s: %w", route.Name, err)
	}
	created := &managedRouteListener{
		session:  session,
		route:    cloneManagedRoute(route),
		listener: listener,
		pending:  make(map[string]net.Conn),
	}
	applogger.Info("Managed tunnel route %s/%s exposed on public port %d", session.name, route.Name, created.publicPort())
	activity.RecordEvent(activity.EventRecord{
		Category:  "tunnel",
		EventType: "managed_route_exposed",
		Severity:  activity.SeverityInfo,
		Source:    "managed_tunnel_server",
		Message:   fmt.Sprintf("Managed tunnel route %s/%s exposed", session.name, route.Name),
		Details: map[string]any{
			"client_name": session.name,
			"route_name":  route.Name,
			"public_port": created.publicPort(),
		},
	})
	return created, nil
}

func cloneManagedRoute(route ManagedRoute) ManagedRoute {
	cloned := route
	if route.IPWhitelist != nil {
		cloned.IPWhitelist = append([]string(nil), route.IPWhitelist...)
	}
	return cloned
}

func validateManagedAutoPortRange(start, end int) error {
	if start == 0 && end == 0 {
		return nil
	}
	if start <= 0 || end <= 0 {
		return fmt.Errorf("managed tunnel auto port range start and end must both be set")
	}
	if start > 65535 || end > 65535 {
		return fmt.Errorf("managed tunnel auto port range must be within 1-65535")
	}
	if start > end {
		return fmt.Errorf("managed tunnel auto port range start must be less than or equal to end")
	}
	return nil
}

func (s *ManagedServer) listenPublicRoutePort(route ManagedRoute) (net.Listener, error) {
	if route.PublicPort != 0 {
		return net.Listen("tcp", net.JoinHostPort(s.PublicBindAddr, strconv.Itoa(route.PublicPort)))
	}

	// Already assigned: use exactly that port, never reallocate
	if route.AssignedPublicPort > 0 {
		return net.Listen("tcp", net.JoinHostPort(s.PublicBindAddr, strconv.Itoa(route.AssignedPublicPort)))
	}

	// First-time allocation
	if s.AutoPortRangeStart == 0 && s.AutoPortRangeEnd == 0 {
		return net.Listen("tcp", net.JoinHostPort(s.PublicBindAddr, "0"))
	}

	return s.allocateNewPort()
}

func (s *ManagedServer) allocateNewPort() (net.Listener, error) {
	reserved := make(map[int]struct{})
	if s.Store != nil {
		ports, err := s.Store.GetAssignedPorts(ProtocolTCP)
		if err != nil {
			return nil, fmt.Errorf("get assigned ports: %w", err)
		}
		for _, p := range ports {
			reserved[p] = struct{}{}
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

func isAddrInUseError(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE)
}

func (s *ManagedServer) handleDataConn(conn net.Conn, hs handshake) {
	if hs.Token != s.Token {
		_ = writeJSONLine(conn, response{Status: responseStatusError, Error: errInvalidTunnelToken})
		_ = conn.Close()
		return
	}
	clientName := hs.ClientName
	if clientName == "" {
		clientName = hs.TunnelName
	}
	if clientName == "" || hs.RouteName == "" || hs.ConnectionID == "" {
		_ = writeJSONLine(conn, response{Status: responseStatusError, Error: errManagedDataFieldsRequired})
		_ = conn.Close()
		return
	}

	s.mu.Lock()
	session := s.clients[clientName]
	s.mu.Unlock()
	if session == nil {
		_ = writeJSONLine(conn, response{Status: responseStatusError, Error: "tunnel client not found"})
		_ = conn.Close()
		return
	}

	session.routesMu.Lock()
	route := session.routes[hs.RouteName]
	session.routesMu.Unlock()
	if route == nil {
		_ = writeJSONLine(conn, response{Status: responseStatusError, Error: "route not found"})
		_ = conn.Close()
		return
	}

	publicConn := route.takePending(hs.ConnectionID)
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

	routeConfig := route.getRoute()
	sessionRecord := newManagedSessionRecord(
		hs.ConnectionID,
		EngineClassic,
		ProtocolTCP,
		clientName,
		hs.RouteName,
		route.publicPort(),
		routeConfig.TargetAddr,
		publicConn.RemoteAddr().String(),
	)
	s.trackActiveSession(sessionRecord)
	go relayConnectionsWithObserver(publicConn, conn, defaultTunnelIdleTimeout, relayObserver{
		OnBytes: func(direction relayDirection, n int) {
			switch direction {
			case relayDirectionLeftToRight:
				sessionRecord.addBytesFromPublic(n)
			case relayDirectionRightToLeft:
				sessionRecord.addBytesToPublic(n)
			}
		},
		OnClose: func() {
			s.untrackActiveSession(hs.ConnectionID)
		},
	})
}

func (s *ManagedServer) close() {
	s.closeOnce.Do(func() {
		if s.listener != nil {
			_ = s.listener.Close()
		}
		s.mu.Lock()
		sessions := make([]*managedClientSession, 0, len(s.clients))
		for _, session := range s.clients {
			sessions = append(sessions, session)
		}
		s.clients = map[string]*managedClientSession{}
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

func (s *managedClientSession) writeControl(msg controlMessage) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return writeJSONLine(s.controlConn, msg)
}

func (s *managedClientSession) close() {
	s.routesMu.Lock()
	if s.closed {
		s.routesMu.Unlock()
		return
	}
	s.closed = true
	routes := make([]*managedRouteListener, 0, len(s.routes))
	for _, route := range s.routes {
		routes = append(routes, route)
	}
	s.routes = map[string]*managedRouteListener{}
	s.routesMu.Unlock()
	for _, route := range routes {
		route.close()
	}
	if s.controlConn != nil {
		_ = s.controlConn.Close()
	}
}

func (r *managedRouteListener) publicPort() int {
	if r.listener == nil {
		return 0
	}
	if tcpAddr, ok := r.listener.Addr().(*net.TCPAddr); ok {
		return tcpAddr.Port
	}
	return 0
}

func (r *managedRouteListener) setRoute(route ManagedRoute) {
	r.routeMu.Lock()
	defer r.routeMu.Unlock()
	r.route = cloneManagedRoute(route)
}

func (r *managedRouteListener) getRoute() ManagedRoute {
	r.routeMu.RLock()
	defer r.routeMu.RUnlock()
	return cloneManagedRoute(r.route)
}

func (r *managedRouteListener) acceptPublicConnections() {
	consecutiveErrors := 0
	for {
		conn, err := r.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			consecutiveErrors++
			route := r.getRoute()
			applogger.Error("Managed tunnel %s/%s public accept error: %v", r.session.name, route.Name, err)
			if consecutiveErrors >= constants.MaxConsecutiveAcceptErrors {
				return
			}
			time.Sleep(constants.AcceptErrorBackoff)
			continue
		}
		consecutiveErrors = 0
		if !r.isSourceAllowed(conn.RemoteAddr()) {
			route := r.getRoute()
			applogger.Warn("Managed tunnel %s/%s rejected public connection from %s by route IP whitelist", r.session.name, route.Name, conn.RemoteAddr())
			_ = conn.Close()
			continue
		}
		if err := r.openPendingConnection(conn); err != nil {
			route := r.getRoute()
			applogger.Warn("Managed tunnel %s/%s failed to open pending connection: %v", r.session.name, route.Name, err)
			_ = conn.Close()
		}
	}
}

func (r *managedRouteListener) openPendingConnection(publicConn net.Conn) error {
	r.pendingMu.Lock()
	if len(r.pending) >= r.session.server.MaxPendingConnections {
		r.pendingMu.Unlock()
		return fmt.Errorf("too many pending public connections")
	}
	r.pendingMu.Unlock()

	connectionID, err := newManagedConnectionID()
	if err != nil {
		return err
	}

	r.pendingMu.Lock()
	r.pending[connectionID] = publicConn
	r.pendingMu.Unlock()

	route := r.getRoute()
	if err := r.session.writeControl(controlMessage{Type: messageOpen, RouteName: route.Name, ConnectionID: connectionID}); err != nil {
		r.pendingMu.Lock()
		delete(r.pending, connectionID)
		r.pendingMu.Unlock()
		return err
	}

	go r.expirePendingConnection(connectionID)
	return nil
}

func (r *managedRouteListener) expirePendingConnection(connectionID string) {
	timer := time.NewTimer(r.session.server.PendingTimeout)
	defer timer.Stop()
	<-timer.C

	r.pendingMu.Lock()
	publicConn, ok := r.pending[connectionID]
	if ok {
		delete(r.pending, connectionID)
	}
	r.pendingMu.Unlock()
	if ok {
		_ = publicConn.Close()
	}
}

func (r *managedRouteListener) takePending(connectionID string) net.Conn {
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	publicConn := r.pending[connectionID]
	delete(r.pending, connectionID)
	return publicConn
}

func (r *managedRouteListener) isSourceAllowed(addr net.Addr) bool {
	route := r.getRoute()
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

func (r *managedRouteListener) close() {
	r.closeOnce.Do(func() {
		if r.listener != nil {
			_ = r.listener.Close()
		}
		r.pendingMu.Lock()
		for id, conn := range r.pending {
			_ = conn.Close()
			delete(r.pending, id)
		}
		r.pendingMu.Unlock()
	})
}

func newManagedConnectionID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate connection ID: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
