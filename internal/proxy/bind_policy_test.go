package proxy

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestBindPolicyResolveUsesExplicitExitBinding(t *testing.T) {
	policy := BindPolicy{
		Enabled: true,
		ExitBindings: []ExitBinding{
			{
				IngressLocalIP:  "172.16.0.10",
				OutboundLocalIP: "172.16.0.20",
			},
		},
	}

	addr, decision := policy.ResolveOutboundLocalAddr(&net.TCPAddr{IP: net.ParseIP("172.16.0.10")})

	if addr == nil || !addr.IP.Equal(net.ParseIP("172.16.0.20")) {
		t.Fatalf("outbound local addr = %v, want 172.16.0.20", addr)
	}
	if !decision.Mapped {
		t.Fatalf("decision.Mapped = false, want true")
	}
	if decision.IngressLocalIP != "172.16.0.10" || decision.OutboundLocalIP != "172.16.0.20" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestBindPolicyResolveFallsBackToIngressLocalIP(t *testing.T) {
	policy := BindPolicy{Enabled: true}

	addr, decision := policy.ResolveOutboundLocalAddr(&net.TCPAddr{IP: net.ParseIP("172.16.0.10")})

	if addr == nil || !addr.IP.Equal(net.ParseIP("172.16.0.10")) {
		t.Fatalf("outbound local addr = %v, want 172.16.0.10", addr)
	}
	if decision.Mapped {
		t.Fatalf("decision.Mapped = true, want false")
	}
}

func TestBindPolicyResolveDisabledReturnsNil(t *testing.T) {
	policy := BindPolicy{Enabled: false}

	addr, decision := policy.ResolveOutboundLocalAddr(&net.TCPAddr{IP: net.ParseIP("172.16.0.10")})

	if addr != nil {
		t.Fatalf("outbound local addr = %v, want nil", addr)
	}
	if decision.BindEnabled {
		t.Fatalf("decision.BindEnabled = true, want false")
	}
}

func TestBindPolicyWarnsForUnspecifiedIngress(t *testing.T) {
	policy := BindPolicy{Enabled: true}

	_, decision := policy.ResolveOutboundLocalAddr(&net.TCPAddr{IP: net.IPv4zero})

	if !strings.Contains(decision.Warning, "unspecified") {
		t.Fatalf("warning = %q, want unspecified warning", decision.Warning)
	}
}

func TestBindPolicyResolveLoopbackIngressUsesDefaultRoute(t *testing.T) {
	policy := BindPolicy{Enabled: true}

	addr, decision := policy.ResolveOutboundLocalAddr(&net.TCPAddr{IP: net.ParseIP("127.0.0.1")})

	if addr != nil {
		t.Fatalf("outbound local addr = %v, want nil for default route", addr)
	}
	if decision.IngressLocalIP != "127.0.0.1" {
		t.Fatalf("decision.IngressLocalIP = %q, want 127.0.0.1", decision.IngressLocalIP)
	}
	if decision.OutboundLocalIP != "" {
		t.Fatalf("decision.OutboundLocalIP = %q, want empty default-route decision", decision.OutboundLocalIP)
	}
	if decision.Mapped {
		t.Fatalf("decision.Mapped = true, want false")
	}
}

func TestBindPolicyResolveLoopbackIngressUsesExplicitExitBinding(t *testing.T) {
	policy := BindPolicy{
		Enabled: true,
		ExitBindings: []ExitBinding{
			{
				IngressLocalIP:  "127.0.0.1",
				OutboundLocalIP: "172.16.0.10",
			},
		},
	}

	addr, decision := policy.ResolveOutboundLocalAddr(&net.TCPAddr{IP: net.ParseIP("127.0.0.1")})

	if addr == nil || !addr.IP.Equal(net.ParseIP("172.16.0.10")) {
		t.Fatalf("outbound local addr = %v, want 172.16.0.10", addr)
	}
	if !decision.Mapped {
		t.Fatalf("decision.Mapped = false, want true")
	}
}

func TestLogExitProbeDiagnosticsFailsWithoutExplicitBindings(t *testing.T) {
	origProbe := probeExitIPsForDiagnostics
	defer func() { probeExitIPsForDiagnostics = origProbe }()
	probeExitIPsForDiagnostics = func(context.Context, ExitProbeConfig, []string) []ExitProbeResult {
		return []ExitProbeResult{{LocalIP: "172.16.0.10", Error: "probe failed"}}
	}

	err := logExitProbeDiagnostics(context.Background(), "SOCKS5", ExitProbeConfig{Enabled: true}, []string{"172.16.0.10"}, false)

	if err == nil || !strings.Contains(err.Error(), "exit probe failed") {
		t.Fatalf("error = %v, want exit probe failure", err)
	}
}

func TestLogExitProbeDiagnosticsSkipsProbeWithExplicitBindings(t *testing.T) {
	origProbe := probeExitIPsForDiagnostics
	defer func() { probeExitIPsForDiagnostics = origProbe }()
	called := make(chan struct{})
	probeExitIPsForDiagnostics = func(context.Context, ExitProbeConfig, []string) []ExitProbeResult {
		close(called)
		return nil
	}

	err := logExitProbeDiagnostics(context.Background(), "SOCKS5", ExitProbeConfig{Enabled: true}, []string{"172.16.0.10"}, true)

	if err != nil {
		t.Fatalf("error = %v, want nil with explicit bindings", err)
	}
	select {
	case <-called:
		t.Fatal("probe should not run when explicit exit bindings are configured")
	case <-time.After(50 * time.Millisecond):
	}
}
