package proxy

import (
	"net"
	"strings"
	"testing"
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
