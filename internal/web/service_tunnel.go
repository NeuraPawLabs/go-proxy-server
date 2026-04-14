package web

import (
	"sort"
	"time"

	"github.com/apeming/go-proxy-server/internal/models"
	"github.com/apeming/go-proxy-server/internal/tunnel"
)

type tunnelClientResponse struct {
	ID               uint       `json:"id"`
	Name             string     `json:"name"`
	RemoteAddr       string     `json:"remoteAddr"`
	Engine           string     `json:"engine"`
	Connected        bool       `json:"connected"`
	Stale            bool       `json:"stale"`
	LastSeenAt       *time.Time `json:"lastSeenAt,omitempty"`
	RouteCount       int        `json:"routeCount"`
	ActiveRouteCount int        `json:"activeRouteCount"`
}

type tunnelRouteResponse struct {
	ID                 uint      `json:"id"`
	ClientName         string    `json:"clientName"`
	Name               string    `json:"name"`
	Protocol           string    `json:"protocol"`
	TargetAddr         string    `json:"targetAddr"`
	PublicPort         int       `json:"publicPort"`
	IPWhitelist        []string  `json:"ipWhitelist"`
	UDPIdleTimeoutSec  int       `json:"udpIdleTimeoutSec"`
	UDPMaxPayload      int       `json:"udpMaxPayload"`
	AssignedPublicPort int       `json:"assignedPublicPort"`
	ActivePublicPort   int       `json:"activePublicPort"`
	Enabled            bool      `json:"enabled"`
	LastError          string    `json:"lastError"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

func (wm *Manager) listTunnelClients() ([]tunnelClientResponse, error) {
	store := tunnel.NewManagedStore(wm.db)
	clients, err := store.ListClients()
	if err != nil {
		return nil, err
	}
	routes, err := store.ListRoutes()
	if err != nil {
		return nil, err
	}

	routeCount := make(map[string]int)
	activeRouteCount := make(map[string]int)
	for _, route := range routes {
		routeCount[route.ClientName]++
		if route.ActivePublicPort > 0 {
			activeRouteCount[route.ClientName]++
		}
	}

	response := make([]tunnelClientResponse, 0, len(clients))
	now := time.Now()
	for _, client := range clients {
		response = append(response, tunnelClientResponse{
			ID:               client.ID,
			Name:             client.Name,
			RemoteAddr:       client.RemoteAddr,
			Engine:           tunnelEngineOrDefault(client.Engine),
			Connected:        client.Connected,
			Stale:            tunnelClientStale(client, now),
			LastSeenAt:       client.LastSeenAt,
			RouteCount:       routeCount[client.Name],
			ActiveRouteCount: activeRouteCount[client.Name],
		})
	}
	return response, nil
}

func (wm *Manager) listTunnelRoutes() ([]tunnelRouteResponse, error) {
	store := tunnel.NewManagedStore(wm.db)
	routes, err := store.ListRoutes()
	if err != nil {
		return nil, err
	}
	response := make([]tunnelRouteResponse, 0, len(routes))
	for _, route := range routes {
		response = append(response, tunnelRouteResponse{
			ID:                 route.ID,
			ClientName:         route.ClientName,
			Name:               route.Name,
			Protocol:           tunnelRouteProtocolOrDefault(route.Protocol),
			TargetAddr:         route.TargetAddr,
			PublicPort:         route.PublicPort,
			IPWhitelist:        tunnel.ParseStoredIPWhitelist(route.IPWhitelist),
			UDPIdleTimeoutSec:  tunnelUDPIdleTimeoutOrDefault(route.UDPIdleTimeoutSec),
			UDPMaxPayload:      tunnelUDPMaxPayloadOrDefault(route.UDPMaxPayload),
			AssignedPublicPort: route.AssignedPublicPort,
			ActivePublicPort:   route.ActivePublicPort,
			Enabled:            route.Enabled,
			LastError:          route.LastError,
			UpdatedAt:          route.UpdatedAt,
		})
	}
	sort.Slice(response, func(i, j int) bool {
		if response[i].ClientName != response[j].ClientName {
			return response[i].ClientName < response[j].ClientName
		}
		return response[i].Name < response[j].Name
	})
	return response, nil
}

func (wm *Manager) saveTunnelRoute(clientName, routeName, targetAddr string, publicPort int, enabled bool, ipWhitelist []string) error {
	return wm.saveTunnelRouteWithOptions(clientName, routeName, targetAddr, publicPort, enabled, ipWhitelist, tunnel.ProtocolTCP, 0, 0)
}

func (wm *Manager) saveTunnelRouteWithOptions(clientName, routeName, targetAddr string, publicPort int, enabled bool, ipWhitelist []string, protocol string, udpIdleTimeoutSec int, udpMaxPayload int) error {
	store := tunnel.NewManagedStore(wm.db)
	return store.SaveRouteWithOptions(clientName, routeName, targetAddr, publicPort, enabled, ipWhitelist, protocol, udpIdleTimeoutSec, udpMaxPayload)
}

func (wm *Manager) deleteTunnelRoute(clientName, routeName string) error {
	store := tunnel.NewManagedStore(wm.db)
	return store.DeleteRoute(clientName, routeName)
}

func (wm *Manager) deleteTunnelClient(name string) error {
	store := tunnel.NewManagedStore(wm.db)
	return store.DeleteClient(name)
}

func tunnelClientStale(client models.TunnelClient, now time.Time) bool {
	if client.LastSeenAt == nil {
		return !client.Connected
	}
	return now.Sub(*client.LastSeenAt) > 30*time.Second
}
