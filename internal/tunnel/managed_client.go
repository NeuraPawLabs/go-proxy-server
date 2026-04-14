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

	"github.com/apeming/go-proxy-server/internal/activity"
	applogger "github.com/apeming/go-proxy-server/internal/logger"
)

type ManagedClient struct {
	ServerAddr       string
	Token            string
	ClientName       string
	DialTimeout      time.Duration
	ReconnectDelay   time.Duration
	HandshakeTimeout time.Duration
	TLSConfig        *tls.Config
	AllowInsecure    bool
	OnConnected      func(clientName string)
	OnDisconnected   func(clientName string, err error)
	OnRoutesChanged  func(clientName string, routes []ManagedClientRoute)

	routesMu sync.RWMutex
	routes   map[string]routeSync
}

type ManagedClientRoute struct {
	Name              string `json:"name"`
	TargetAddr        string `json:"targetAddr"`
	PublicPort        int    `json:"publicPort"`
	Enabled           bool   `json:"enabled"`
	Protocol          string `json:"protocol"`
	UDPIdleTimeoutSec int    `json:"udpIdleTimeoutSec,omitempty"`
	UDPMaxPayload     int    `json:"udpMaxPayload,omitempty"`
}

func NewManagedClient(serverAddr, token, clientName string) *ManagedClient {
	return &ManagedClient{
		ServerAddr:       serverAddr,
		Token:            token,
		ClientName:       clientName,
		DialTimeout:      5 * time.Second,
		ReconnectDelay:   3 * time.Second,
		HandshakeTimeout: 10 * time.Second,
		routes:           make(map[string]routeSync),
	}
}

func (c *ManagedClient) Run(ctx context.Context) error {
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
		applogger.Warn("Managed tunnel client %s disconnected: %v", c.ClientName, err)
		select {
		case <-ctx.Done():
			c.notifyDisconnected(nil)
			return nil
		case <-time.After(c.ReconnectDelay):
		}
	}
}

func (c *ManagedClient) validate() error {
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
	if c.TLSConfig == nil && !c.AllowInsecure {
		return fmt.Errorf("tunnel client TLS config is required unless insecure mode is explicitly enabled")
	}
	return nil
}

func (c *ManagedClient) runOnce(ctx context.Context) error {
	conn, err := c.dialServer(ctx)
	if err != nil {
		return fmt.Errorf("connect tunnel server: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(c.HandshakeTimeout))

	reader := bufio.NewReader(conn)
	if err := writeJSONLine(conn, handshake{Mode: modeControl, Token: c.Token, ClientName: c.ClientName}); err != nil {
		return err
	}

	var resp response
	if err := readJSONLine(reader, &resp); err != nil {
		return fmt.Errorf("read tunnel registration response: %w", err)
	}
	if resp.Status != responseStatusOK {
		if isPermanentManagedError(resp.Error) {
			return &permanentError{err: errors.New(resp.Error)}
		}
		return errors.New(resp.Error)
	}
	_ = conn.SetDeadline(time.Time{})
	if c.OnConnected != nil {
		c.OnConnected(c.ClientName)
	}
	applogger.Info("Managed tunnel client %s connected", c.ClientName)
	activity.RecordEvent(activity.EventRecord{
		Category:  "tunnel",
		EventType: "managed_client_control_connected",
		Severity:  activity.SeverityInfo,
		Source:    "managed_tunnel_client",
		Message:   fmt.Sprintf("Managed tunnel client %s connected", c.ClientName),
		Details: map[string]any{
			"client_name": c.ClientName,
			"server_addr": c.ServerAddr,
		},
	})

	for {
		var msg controlMessage
		if err := readJSONLine(reader, &msg); err != nil {
			return err
		}
		switch msg.Type {
		case messageSyncRoutes:
			c.replaceRoutes(msg.Routes)
		case messageOpen:
			if msg.ConnectionID == "" || msg.RouteName == "" {
				continue
			}
			go c.handleOpen(ctx, msg.RouteName, msg.ConnectionID)
		}
	}
}

func (c *ManagedClient) handleOpen(ctx context.Context, routeName, connectionID string) {
	route, ok := c.getRoute(routeName)
	if !ok || !route.Enabled || route.TargetAddr == "" {
		applogger.Warn("Managed tunnel route %s/%s is unavailable", c.ClientName, routeName)
		activity.RecordEvent(activity.EventRecord{
			Category:  "tunnel",
			EventType: "managed_route_unavailable",
			Severity:  activity.SeverityWarn,
			Source:    "managed_tunnel_client",
			Message:   fmt.Sprintf("Managed tunnel route %s/%s is unavailable", c.ClientName, routeName),
			Details: map[string]any{
				"client_name": c.ClientName,
				"route_name":  routeName,
			},
		})
		return
	}

	localConn, err := net.DialTimeout("tcp", route.TargetAddr, c.DialTimeout)
	if err != nil {
		applogger.Warn("Managed tunnel %s/%s failed to connect local target %s: %v", c.ClientName, routeName, route.TargetAddr, err)
		activity.RecordEvent(activity.EventRecord{
			Category:  "tunnel",
			EventType: "managed_target_connect_failed",
			Severity:  activity.SeverityWarn,
			Source:    "managed_tunnel_client",
			Message:   fmt.Sprintf("Managed tunnel %s/%s failed to connect local target", c.ClientName, routeName),
			Details: map[string]any{
				"client_name": c.ClientName,
				"route_name":  routeName,
				"target_addr": route.TargetAddr,
				"error":       err.Error(),
			},
		})
		return
	}

	dataConn, err := c.dialServer(ctx)
	if err != nil {
		_ = localConn.Close()
		applogger.Warn("Managed tunnel %s/%s failed to open data connection: %v", c.ClientName, routeName, err)
		return
	}
	_ = dataConn.SetDeadline(time.Now().Add(c.HandshakeTimeout))

	reader := bufio.NewReader(dataConn)
	if err := writeJSONLine(dataConn, handshake{Mode: modeData, Token: c.Token, ClientName: c.ClientName, RouteName: routeName, ConnectionID: connectionID}); err != nil {
		_ = localConn.Close()
		_ = dataConn.Close()
		applogger.Warn("Managed tunnel %s/%s failed to send data handshake: %v", c.ClientName, routeName, err)
		return
	}

	var resp response
	if err := readJSONLine(reader, &resp); err != nil {
		_ = localConn.Close()
		_ = dataConn.Close()
		applogger.Warn("Managed tunnel %s/%s failed to read data handshake response: %v", c.ClientName, routeName, err)
		return
	}
	if resp.Status != responseStatusOK {
		_ = localConn.Close()
		_ = dataConn.Close()
		applogger.Warn("Managed tunnel %s/%s data connection rejected: %s", c.ClientName, routeName, resp.Error)
		activity.RecordEvent(activity.EventRecord{
			Category:  "tunnel",
			EventType: "managed_data_connection_rejected",
			Severity:  activity.SeverityWarn,
			Source:    "managed_tunnel_client",
			Message:   fmt.Sprintf("Managed tunnel %s/%s data connection rejected", c.ClientName, routeName),
			Details: map[string]any{
				"client_name":   c.ClientName,
				"route_name":    routeName,
				"connection_id": connectionID,
				"error":         resp.Error,
			},
		})
		return
	}
	_ = dataConn.SetDeadline(time.Time{})
	relayConnections(localConn, dataConn)
}

func (c *ManagedClient) dialServer(ctx context.Context) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: c.DialTimeout, KeepAlive: 30 * time.Second}
	if c.TLSConfig != nil {
		tlsDialer := &tls.Dialer{NetDialer: dialer, Config: c.TLSConfig}
		return tlsDialer.DialContext(ctx, "tcp", c.ServerAddr)
	}
	return dialer.DialContext(ctx, "tcp", c.ServerAddr)
}

func (c *ManagedClient) replaceRoutes(routes []routeSync) {
	next := make(map[string]routeSync, len(routes))
	snapshot := make([]ManagedClientRoute, 0, len(routes))
	for _, route := range routes {
		next[route.Name] = route
		snapshot = append(snapshot, ManagedClientRoute{
			Name:              route.Name,
			TargetAddr:        route.TargetAddr,
			PublicPort:        route.PublicPort,
			Enabled:           route.Enabled,
			Protocol:          normalizeManagedRouteProtocol(route.Protocol),
			UDPIdleTimeoutSec: normalizeManagedUDPIdleTimeout(route.UDPIdleTimeoutSec),
			UDPMaxPayload:     normalizeManagedUDPMaxPayload(route.UDPMaxPayload),
		})
	}
	c.routesMu.Lock()
	c.routes = next
	c.routesMu.Unlock()
	if c.OnRoutesChanged != nil {
		c.OnRoutesChanged(c.ClientName, snapshot)
	}
}

func (c *ManagedClient) getRoute(name string) (routeSync, bool) {
	c.routesMu.RLock()
	defer c.routesMu.RUnlock()
	route, ok := c.routes[name]
	return route, ok
}

func (c *ManagedClient) RoutesSnapshot() []ManagedClientRoute {
	c.routesMu.RLock()
	defer c.routesMu.RUnlock()

	routes := make([]ManagedClientRoute, 0, len(c.routes))
	for _, route := range c.routes {
		routes = append(routes, ManagedClientRoute{
			Name:       route.Name,
			TargetAddr: route.TargetAddr,
			PublicPort: route.PublicPort,
			Enabled:    route.Enabled,
		})
	}
	return routes
}

func (c *ManagedClient) notifyDisconnected(err error) {
	c.replaceRoutes(nil)
	if c.OnDisconnected != nil {
		c.OnDisconnected(c.ClientName, err)
	}
}

func isPermanentManagedError(msg string) bool {
	switch msg {
	case errInvalidTunnelToken, errClientNameRequired, errUnsupportedTunnelMode:
		return true
	default:
		return false
	}
}
