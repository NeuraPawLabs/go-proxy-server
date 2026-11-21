package tunnel

import (
	"bytes"
	"testing"
	"time"
)

func TestManagedQUICConfigUsesLargeInitialPacketSizeForWrappedUDPPackets(t *testing.T) {
	config := managedQUICConfig(10 * time.Second)

	if config.InitialPacketSize != managedQUICInitialPacketSize {
		t.Fatalf("unexpected initial packet size: got %d want %d", config.InitialPacketSize, managedQUICInitialPacketSize)
	}
	if !config.EnableDatagrams {
		t.Fatal("managed QUIC config must enable datagrams")
	}
}

func TestQUICUDPServerDatagramDoesNotEncodeUnusedSourceAddress(t *testing.T) {
	frame := quicUDPServerDatagram{
		SessionID:  "0011223344556677",
		RouteName:  "route-name",
		SourceAddr: "198.51.100.10:65535",
		Payload:    []byte("payload"),
	}

	encoded, err := marshalQUICUDPServerDatagram(frame)
	if err != nil {
		t.Fatalf("marshal server datagram: %v", err)
	}

	expectedLength := 1 + 8 + 2 + len(frame.RouteName) + 2 + len(frame.Payload)
	if len(encoded) != expectedLength {
		t.Fatalf("unexpected encoded length: got %d want %d", len(encoded), expectedLength)
	}

	decoded, err := unmarshalQUICUDPServerDatagram(encoded)
	if err != nil {
		t.Fatalf("unmarshal server datagram: %v", err)
	}
	if decoded.SourceAddr != "" {
		t.Fatalf("source address should not be encoded: %q", decoded.SourceAddr)
	}
	if !bytes.Equal(decoded.Payload, frame.Payload) {
		t.Fatalf("unexpected payload: got %q want %q", decoded.Payload, frame.Payload)
	}
}

func TestManagedConnectionIDUsesCompactEncoding(t *testing.T) {
	connectionID, err := newManagedConnectionID()
	if err != nil {
		t.Fatalf("generate managed connection ID: %v", err)
	}
	if len(connectionID) != 16 {
		t.Fatalf("unexpected managed connection ID length: got %d want %d", len(connectionID), 16)
	}
}
