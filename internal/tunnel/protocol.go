package tunnel

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	modeControl = "control"
	modeData    = "data"

	messageOpen       = "open"
	messageSyncRoutes = "sync_routes"
	messageHeartbeat  = "heartbeat"

	maxMessageSize = 4 * 1024
)

const (
	EngineClassic = "classic"
	EngineQUIC    = "quic"

	ProtocolTCP = "tcp"
	ProtocolUDP = "udp"
)

var errMessageTooLarge = errors.New("tunnel message too large")

const (
	responseStatusOK    = "ok"
	responseStatusError = "error"

	errInvalidTunnelToken             = "invalid tunnel token"
	errTunnelNameRequired             = "tunnel name is required"
	errClientNameRequired             = "client name is required"
	errUnsupportedTunnelMode          = "unsupported tunnel mode"
	errTunnelNameConnectionIDRequired = "tunnel name and connection ID are required"
	errManagedDataFieldsRequired      = "client name, route name and connection ID are required"
)

type handshake struct {
	Mode         string `json:"mode"`
	Token        string `json:"token"`
	ClientName   string `json:"clientName,omitempty"`
	TunnelName   string `json:"tunnelName,omitempty"`
	RouteName    string `json:"routeName,omitempty"`
	PublicPort   int    `json:"publicPort,omitempty"`
	ConnectionID string `json:"connectionId,omitempty"`
}

type response struct {
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	TunnelName string `json:"tunnelName,omitempty"`
	PublicPort int    `json:"publicPort,omitempty"`
}

type controlMessage struct {
	Type         string      `json:"type"`
	RouteName    string      `json:"routeName,omitempty"`
	ConnectionID string      `json:"connectionId,omitempty"`
	Routes       []routeSync `json:"routes,omitempty"`
}

type routeSync struct {
	Name              string `json:"name"`
	TargetAddr        string `json:"targetAddr"`
	PublicPort        int    `json:"publicPort"`
	Enabled           bool   `json:"enabled"`
	Protocol          string `json:"protocol,omitempty"`
	UDPIdleTimeoutSec int    `json:"udpIdleTimeoutSec,omitempty"`
	UDPMaxPayload     int    `json:"udpMaxPayload,omitempty"`
}

func readJSONLine(r *bufio.Reader, dst any) error {
	for {
		line, err := readLimitedLine(r, maxMessageSize)
		if err != nil {
			return err
		}

		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if err := json.Unmarshal(line, dst); err != nil {
			return fmt.Errorf("decode json line: %w", err)
		}
		return nil
	}
}

func readLimitedLine(r *bufio.Reader, limit int) ([]byte, error) {
	var line []byte

	for {
		fragment, err := r.ReadSlice('\n')
		line = append(line, fragment...)
		if len(line) > limit {
			return nil, errMessageTooLarge
		}

		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			return nil, err
		}

		return line, nil
	}
}

func writeJSONLine(w io.Writer, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal json line: %w", err)
	}
	data = append(data, '\n')
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write json line: %w", err)
	}
	return nil
}
