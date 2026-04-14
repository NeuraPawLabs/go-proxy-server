package web

import (
	"fmt"
	"strings"

	"github.com/apeming/go-proxy-server/internal/tunnel"
)

func tunnelEngineOrDefault(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", tunnel.EngineClassic:
		return tunnel.EngineClassic
	case tunnel.EngineQUIC:
		return tunnel.EngineQUIC
	default:
		return tunnel.EngineClassic
	}
}

func validateTunnelEngine(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", tunnel.EngineClassic, tunnel.EngineQUIC:
		return nil
	default:
		return fmt.Errorf("unsupported tunnel engine")
	}
}

func tunnelRouteProtocolOrDefault(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", tunnel.ProtocolTCP:
		return tunnel.ProtocolTCP
	case tunnel.ProtocolUDP:
		return tunnel.ProtocolUDP
	default:
		return tunnel.ProtocolTCP
	}
}

func validateTunnelRouteProtocol(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", tunnel.ProtocolTCP, tunnel.ProtocolUDP:
		return nil
	default:
		return fmt.Errorf("unsupported tunnel protocol")
	}
}

func tunnelUDPIdleTimeoutOrDefault(value int) int {
	if value <= 0 {
		return tunnel.DefaultManagedUDPIdleTimeoutSec
	}
	return value
}

func tunnelUDPMaxPayloadOrDefault(value int) int {
	if value <= 0 {
		return tunnel.DefaultManagedUDPMaxPayload
	}
	return value
}
