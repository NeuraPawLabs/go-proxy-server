package tunnel

import (
	"time"

	"github.com/quic-go/quic-go"
)

const managedQUICInitialPacketSize uint16 = 1452

func managedQUICConfig(heartbeatInterval time.Duration) *quic.Config {
	return &quic.Config{
		EnableDatagrams:   true,
		KeepAlivePeriod:   heartbeatInterval,
		MaxIdleTimeout:    2 * heartbeatInterval,
		InitialPacketSize: managedQUICInitialPacketSize,
	}
}
