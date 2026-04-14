package tunnel

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/apeming/go-proxy-server/internal/models"
)

type ManagedRoute struct {
	ClientName         string
	Name               string
	Protocol           string
	TargetAddr         string
	PublicPort         int
	IPWhitelist        []string
	UDPIdleTimeoutSec  int
	UDPMaxPayload      int
	AssignedPublicPort int
	ActivePublicPort   int
	Enabled            bool
	LastError          string
	UpdatedAt          time.Time
}

const (
	DefaultManagedUDPIdleTimeoutSec = 60
	DefaultManagedUDPMaxPayload     = 1200
)

type ManagedStore struct {
	db *gorm.DB
}

func NewManagedStore(db *gorm.DB) *ManagedStore {
	return &ManagedStore{db: db}
}

func (s *ManagedStore) InitializeRuntimeState() error {
	if s == nil || s.db == nil {
		return nil
	}
	if err := s.db.Model(&models.TunnelClient{}).Where("connected = ?", true).Updates(map[string]any{"connected": false}).Error; err != nil {
		return fmt.Errorf("reset tunnel clients: %w", err)
	}
	if err := s.db.Model(&models.TunnelRoute{}).Where("active_public_port <> ? OR last_error <> ?", 0, "").Updates(map[string]any{"active_public_port": 0, "last_error": ""}).Error; err != nil {
		return fmt.Errorf("reset tunnel routes: %w", err)
	}
	return nil
}

func (s *ManagedStore) UpsertClientHeartbeat(name, remoteAddr, engine string, connected bool) error {
	if s == nil || s.db == nil {
		return nil
	}
	if name == "" {
		return fmt.Errorf("client name is required")
	}
	engine = normalizeTunnelEngine(engine)
	return s.db.Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		var client models.TunnelClient
		err := tx.Where("name = ?", name).First(&client).Error
		switch {
		case err == nil:
			return tx.Model(&client).Updates(map[string]any{
				"remote_addr":  remoteAddr,
				"engine":       engine,
				"connected":    connected,
				"last_seen_at": &now,
			}).Error
		case err == gorm.ErrRecordNotFound:
			return tx.Create(&models.TunnelClient{
				Name:       name,
				RemoteAddr: remoteAddr,
				Engine:     engine,
				Connected:  connected,
				LastSeenAt: &now,
			}).Error
		default:
			return err
		}
	})
}

func (s *ManagedStore) MarkClientDisconnected(name string) error {
	if s == nil || s.db == nil || name == "" {
		return nil
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		if err := tx.Model(&models.TunnelClient{}).Where("name = ?", name).Updates(map[string]any{
			"connected":    false,
			"last_seen_at": &now,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&models.TunnelRoute{}).Where("client_name = ?", name).Updates(map[string]any{
			"active_public_port": 0,
			"last_error":         "",
		}).Error
	})
}

func (s *ManagedStore) EnsureClient(name string) error {
	if s == nil || s.db == nil || name == "" {
		return nil
	}
	return s.db.Where(models.TunnelClient{Name: name}).FirstOrCreate(&models.TunnelClient{Name: name, Engine: EngineClassic, Connected: false}).Error
}

func (s *ManagedStore) ListDesiredRoutes() (map[string][]ManagedRoute, error) {
	if s == nil || s.db == nil {
		return map[string][]ManagedRoute{}, nil
	}
	var rows []models.TunnelRoute
	if err := s.db.Order("client_name ASC, name ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make(map[string][]ManagedRoute)
	for _, row := range rows {
		result[row.ClientName] = append(result[row.ClientName], ManagedRoute{
			ClientName:         row.ClientName,
			Name:               row.Name,
			Protocol:           normalizeManagedRouteProtocol(row.Protocol),
			TargetAddr:         row.TargetAddr,
			PublicPort:         row.PublicPort,
			IPWhitelist:        ParseStoredIPWhitelist(row.IPWhitelist),
			UDPIdleTimeoutSec:  normalizeManagedUDPIdleTimeout(row.UDPIdleTimeoutSec),
			UDPMaxPayload:      normalizeManagedUDPMaxPayload(row.UDPMaxPayload),
			AssignedPublicPort: row.AssignedPublicPort,
			ActivePublicPort:   row.ActivePublicPort,
			Enabled:            row.Enabled,
			LastError:          row.LastError,
			UpdatedAt:          row.UpdatedAt,
		})
	}
	return result, nil
}

func (s *ManagedStore) ListClients() ([]models.TunnelClient, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var clients []models.TunnelClient
	if err := s.db.Order("name ASC").Find(&clients).Error; err != nil {
		return nil, err
	}
	return clients, nil
}

func (s *ManagedStore) ListRoutes() ([]models.TunnelRoute, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var routes []models.TunnelRoute
	if err := s.db.Order("client_name ASC, name ASC").Find(&routes).Error; err != nil {
		return nil, err
	}
	return routes, nil
}

func (s *ManagedStore) DeleteClient(name string) error {
	if s == nil || s.db == nil {
		return nil
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("client name is required")
	}

	return s.db.Transaction(func(tx *gorm.DB) error {
		var client models.TunnelClient
		if err := tx.Where("name = ?", name).First(&client).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return fmt.Errorf("tunnel client not found")
			}
			return err
		}
		if client.Connected {
			return fmt.Errorf("connected tunnel client cannot be deleted")
		}
		// Delete routes explicitly so correctness does not depend on SQLite
		// foreign_keys being enabled on the specific pooled connection.
		if err := tx.Where("client_name = ?", name).Unscoped().Delete(&models.TunnelRoute{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Delete(&client).Error
	})
}

func (s *ManagedStore) SaveRoute(clientName, routeName, targetAddr string, publicPort int, enabled bool, ipWhitelist []string) error {
	return s.SaveRouteWithOptions(clientName, routeName, targetAddr, publicPort, enabled, ipWhitelist, ProtocolTCP, 0, 0)
}

func (s *ManagedStore) SaveRouteWithOptions(clientName, routeName, targetAddr string, publicPort int, enabled bool, ipWhitelist []string, protocol string, udpIdleTimeoutSec int, udpMaxPayload int) error {
	if s == nil || s.db == nil {
		return nil
	}
	if clientName == "" || routeName == "" {
		return fmt.Errorf("client name and route name are required")
	}
	protocol = normalizeManagedRouteProtocol(protocol)
	if err := validateManagedRouteProtocol(protocol); err != nil {
		return err
	}
	normalizedTargetAddr, err := normalizeManagedRouteTargetAddr(targetAddr)
	if err != nil {
		return err
	}
	if publicPort < 0 || publicPort > 65535 {
		return fmt.Errorf("public port must be between 0 and 65535")
	}
	udpIdleTimeoutSec = normalizeManagedUDPIdleTimeout(udpIdleTimeoutSec)
	udpMaxPayload = normalizeManagedUDPMaxPayload(udpMaxPayload)
	if err := validateManagedUDPRouteSettings(protocol, udpIdleTimeoutSec, udpMaxPayload); err != nil {
		return err
	}
	normalizedWhitelist, err := normalizeIPWhitelist(ipWhitelist)
	if err != nil {
		return err
	}
	storedWhitelist, err := marshalIPWhitelist(normalizedWhitelist)
	if err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where(models.TunnelClient{Name: clientName}).FirstOrCreate(&models.TunnelClient{Name: clientName, Connected: false}).Error; err != nil {
			return err
		}

		var route models.TunnelRoute
		err = tx.Where("client_name = ? AND name = ?", clientName, routeName).First(&route).Error
		switch {
		case err == nil:
			if route.AssignedPublicPort > 0 {
				if route.PublicPort != publicPort {
					return fmt.Errorf("assigned public port is immutable; delete the route to change its public port")
				}
				if route.Protocol != protocol {
					return fmt.Errorf("assigned public port is immutable; delete the route to change its protocol")
				}
			}
			updates := map[string]any{
				"protocol":             protocol,
				"target_addr":          normalizedTargetAddr,
				"public_port":          publicPort,
				"ip_whitelist":         storedWhitelist,
				"udp_idle_timeout_sec": udpIdleTimeoutSec,
				"udp_max_payload":      udpMaxPayload,
				"enabled":              enabled,
				"last_error":           "",
				"active_public_port":   0,
			}
			return tx.Model(&route).Updates(updates).Error
		case err == gorm.ErrRecordNotFound:
			createErr := tx.Create(&models.TunnelRoute{
				ClientName:        clientName,
				Name:              routeName,
				Protocol:          protocol,
				TargetAddr:        normalizedTargetAddr,
				PublicPort:        publicPort,
				IPWhitelist:       storedWhitelist,
				UDPIdleTimeoutSec: udpIdleTimeoutSec,
				UDPMaxPayload:     udpMaxPayload,
				Enabled:           enabled,
			}).Error
			if createErr != nil {
				if isUniqueConstraintError(createErr) {
					return fmt.Errorf("route '%s' already exists for client '%s'", routeName, clientName)
				}
				return createErr
			}
			return nil
		default:
			return err
		}
	})
}

func normalizeTunnelEngine(engine string) string {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "", EngineClassic:
		return EngineClassic
	case EngineQUIC:
		return EngineQUIC
	default:
		return EngineClassic
	}
}

func normalizeManagedRouteProtocol(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "", ProtocolTCP:
		return ProtocolTCP
	case ProtocolUDP:
		return ProtocolUDP
	default:
		return ProtocolTCP
	}
}

func validateManagedRouteProtocol(protocol string) error {
	switch protocol {
	case ProtocolTCP, ProtocolUDP:
		return nil
	default:
		return fmt.Errorf("unsupported tunnel protocol")
	}
}

func normalizeManagedUDPIdleTimeout(value int) int {
	if value <= 0 {
		return DefaultManagedUDPIdleTimeoutSec
	}
	return value
}

func normalizeManagedUDPMaxPayload(value int) int {
	if value <= 0 {
		return DefaultManagedUDPMaxPayload
	}
	return value
}

func validateManagedUDPRouteSettings(protocol string, idleTimeoutSec, maxPayload int) error {
	if protocol != ProtocolUDP {
		return nil
	}
	if idleTimeoutSec < 10 || idleTimeoutSec > 3600 {
		return fmt.Errorf("udp idle timeout must be between 10 and 3600 seconds")
	}
	if maxPayload < 256 || maxPayload > 65507 {
		return fmt.Errorf("udp max payload must be between 256 and 65507 bytes")
	}
	return nil
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "UNIQUE constraint failed") ||
		strings.Contains(message, "duplicate key")
}

func normalizeManagedRouteTargetAddr(targetAddr string) (string, error) {
	value := strings.TrimSpace(targetAddr)
	if value == "" {
		return "", fmt.Errorf("target address is required")
	}

	if isDigitsOnly(value) {
		port, err := strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			return "", fmt.Errorf("target port must be between 1 and 65535")
		}
		return net.JoinHostPort("127.0.0.1", value), nil
	}

	if strings.HasPrefix(value, ":") && isDigitsOnly(strings.TrimPrefix(value, ":")) {
		port := strings.TrimPrefix(value, ":")
		parsed, err := strconv.Atoi(port)
		if err != nil || parsed < 1 || parsed > 65535 {
			return "", fmt.Errorf("target port must be between 1 and 65535")
		}
		return net.JoinHostPort("127.0.0.1", port), nil
	}

	if _, _, err := net.SplitHostPort(value); err != nil {
		return "", fmt.Errorf("target address must be host:port or port")
	}
	return value, nil
}

func isDigitsOnly(value string) bool {
	if value == "" {
		return false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func (s *ManagedStore) GetAssignedPorts(protocol string) ([]int, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var ports []int
	if err := s.db.Model(&models.TunnelRoute{}).
		Where("protocol = ? AND assigned_public_port > 0", normalizeManagedRouteProtocol(protocol)).
		Pluck("assigned_public_port", &ports).Error; err != nil {
		return nil, err
	}
	return ports, nil
}

func (s *ManagedStore) DeleteRoute(clientName, routeName string) error {
	if s == nil || s.db == nil {
		return nil
	}
	if clientName == "" || routeName == "" {
		return fmt.Errorf("client name and route name are required")
	}
	return s.db.Where("client_name = ? AND name = ?", clientName, routeName).Unscoped().Delete(&models.TunnelRoute{}).Error
}

func (s *ManagedStore) UpdateRouteRuntime(clientName, routeName string, activePublicPort int, lastError string) error {
	if s == nil || s.db == nil || clientName == "" || routeName == "" {
		return nil
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var route models.TunnelRoute
		if err := tx.Where("client_name = ? AND name = ?", clientName, routeName).First(&route).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil
			}
			return err
		}

		updates := map[string]any{
			"active_public_port": activePublicPort,
			"last_error":         lastError,
		}
		if activePublicPort > 0 {
			switch {
			case route.AssignedPublicPort == 0:
				updates["assigned_public_port"] = activePublicPort
			case route.AssignedPublicPort != activePublicPort:
				return fmt.Errorf(
					"assigned public port is immutable for route %s/%s: stored=%d new=%d",
					clientName,
					routeName,
					route.AssignedPublicPort,
					activePublicPort,
				)
			}
		}

		err := tx.Model(&route).Updates(updates).Error
		if err != nil && isUniqueConstraintError(err) && activePublicPort > 0 {
			return fmt.Errorf("assigned public port %d for protocol %s is already in use", activePublicPort, route.Protocol)
		}
		return err
	})
}

func (s *ManagedStore) ClearClientRuntimeRoutes(clientName string) error {
	if s == nil || s.db == nil || clientName == "" {
		return nil
	}
	return s.db.Model(&models.TunnelRoute{}).Where("client_name = ?", clientName).Updates(map[string]any{
		"active_public_port": 0,
		"last_error":         "",
	}).Error
}

func SortManagedRoutes(routes []ManagedRoute) {
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].ClientName != routes[j].ClientName {
			return routes[i].ClientName < routes[j].ClientName
		}
		return routes[i].Name < routes[j].Name
	})
}

func normalizeIPWhitelist(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}

		if strings.Contains(value, "/") {
			_, prefix, err := net.ParseCIDR(value)
			if err != nil {
				return nil, fmt.Errorf("invalid route ip whitelist entry: %s", value)
			}
			value = prefix.String()
		} else {
			ip := net.ParseIP(value)
			if ip == nil {
				return nil, fmt.Errorf("invalid route ip whitelist entry: %s", value)
			}
			value = ip.String()
		}

		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}

	sort.Strings(normalized)
	return normalized, nil
}

func marshalIPWhitelist(values []string) (string, error) {
	if len(values) == 0 {
		return "", nil
	}

	data, err := json.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("marshal route ip whitelist: %w", err)
	}
	return string(data), nil
}

func ParseStoredIPWhitelist(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err == nil {
		return values
	}

	parts := strings.Split(raw, ",")
	parsed := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value != "" {
			parsed = append(parsed, value)
		}
	}
	return parsed
}
