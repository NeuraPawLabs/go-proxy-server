package tunnel

import (
	"io"
	"net"
	"testing"
	"time"
)

func TestRelayConnectionsWithIdleTimeoutRelaysTraffic(t *testing.T) {
	leftApp, leftRelay := net.Pipe()
	rightApp, rightRelay := net.Pipe()
	defer leftApp.Close()
	defer rightApp.Close()

	done := make(chan struct{})
	go func() {
		relayConnectionsWithIdleTimeout(leftRelay, rightRelay, time.Second)
		close(done)
	}()

	payload := []byte("hello tunnel")
	if _, err := leftApp.Write(payload); err != nil {
		t.Fatalf("write left app: %v", err)
	}

	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(rightApp, buf); err != nil {
		t.Fatalf("read right app: %v", err)
	}
	if string(buf) != string(payload) {
		t.Fatalf("unexpected payload: got %q want %q", string(buf), string(payload))
	}

	leftApp.Close()
	rightApp.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for relay shutdown")
	}
}

func TestRelayConnectionsWithIdleTimeoutClosesIdleConnections(t *testing.T) {
	leftApp, leftRelay := net.Pipe()
	rightApp, rightRelay := net.Pipe()
	defer leftApp.Close()
	defer rightApp.Close()

	done := make(chan struct{})
	go func() {
		relayConnectionsWithIdleTimeout(leftRelay, rightRelay, 50*time.Millisecond)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("idle relay did not close in time")
	}
}
