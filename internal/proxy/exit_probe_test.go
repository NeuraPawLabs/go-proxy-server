package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIsExitProbeCandidateIPExcludesLoopback(t *testing.T) {
	if isExitProbeCandidateIP(net.ParseIP("127.0.0.1")) {
		t.Fatalf("127.0.0.1 should not be an exit probe candidate")
	}
	if isExitProbeCandidateIP(net.ParseIP("::1")) {
		t.Fatalf("::1 should not be an exit probe candidate")
	}
}

func TestIsExitProbeCandidateIPAllowsPrivateRoutableAddress(t *testing.T) {
	if !isExitProbeCandidateIP(net.ParseIP("172.16.0.10")) {
		t.Fatalf("172.16.0.10 should be an exit probe candidate")
	}
}

func TestNormalizeExitProbeConfigTrimsProbeURL(t *testing.T) {
	cfg := normalizeExitProbeConfig(ExitProbeConfig{
		ProbeURL: " https://api.ipify.org ",
		Timeout:  time.Second,
	})

	if cfg.ProbeURL != "https://api.ipify.org" {
		t.Fatalf("probe URL = %q, want trimmed URL", cfg.ProbeURL)
	}
}

func TestProbeExitIPUsesSpecifiedLocalIP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			t.Fatalf("split remote addr: %v", err)
		}
		if host != "127.0.0.1" {
			t.Fatalf("remote host = %q, want 127.0.0.1", host)
		}
		fmt.Fprint(w, "203.0.113.10\n")
	}))
	defer server.Close()

	result := probeExitIP(context.Background(), ExitProbeConfig{
		ProbeURL: server.URL,
		Timeout:  2 * time.Second,
	}, net.ParseIP("127.0.0.1"))

	if result.Error != "" {
		t.Fatalf("probe error = %q", result.Error)
	}
	if result.LocalIP != "127.0.0.1" {
		t.Fatalf("local IP = %q, want 127.0.0.1", result.LocalIP)
	}
	if result.PublicIP != "203.0.113.10" {
		t.Fatalf("public IP = %q, want 203.0.113.10", result.PublicIP)
	}
}

func TestProbeExitIPRejectsNilLocalIP(t *testing.T) {
	result := probeExitIP(context.Background(), ExitProbeConfig{}, nil)

	if result.Error == "" {
		t.Fatalf("probe error is empty, want unusable local IP error")
	}
}
