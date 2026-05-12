package proxy

import (
	"encoding/binary"
	"net"
	"testing"
)

func TestReadSocks5RequestFormatsIPv6HostPort(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()

	go func() {
		defer client.Close()

		req := []byte{socks5Version, cmdConnect, 0x00, addrTypeIPv6}
		req = append(req, net.ParseIP("2606:4700:20::681a:d1f").To16()...)
		port := make([]byte, 2)
		binary.BigEndian.PutUint16(port, 80)
		req = append(req, port...)

		_, _ = client.Write(req)
	}()

	got, err := readSocks5Request(server)
	if err != nil {
		t.Fatalf("readSocks5Request: %v", err)
	}

	want := "[2606:4700:20::681a:d1f]:80"
	if got != want {
		t.Fatalf("host = %q, want %q", got, want)
	}
}
