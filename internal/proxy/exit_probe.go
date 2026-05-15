package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultExitProbeURL     = "https://api.ipify.org"
	DefaultExitProbeTimeout = 3 * time.Second
)

// ExitProbeConfig controls public egress IP probing for local source addresses.
type ExitProbeConfig struct {
	Enabled  bool
	ProbeURL string
	Timeout  time.Duration
}

// ExitProbeResult describes one local source IP probe attempt.
type ExitProbeResult struct {
	LocalIP  string
	PublicIP string
	Error    string
}

func normalizeExitProbeConfig(cfg ExitProbeConfig) ExitProbeConfig {
	cfg.ProbeURL = strings.TrimSpace(cfg.ProbeURL)
	if cfg.ProbeURL == "" {
		cfg.ProbeURL = DefaultExitProbeURL
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultExitProbeTimeout
	}
	return cfg
}

func isExitProbeCandidateIP(ip net.IP) bool {
	return ip != nil &&
		!ip.IsUnspecified() &&
		!ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast()
}

func probeExitIP(ctx context.Context, cfg ExitProbeConfig, localIP net.IP) ExitProbeResult {
	cfg = normalizeExitProbeConfig(cfg)
	result := ExitProbeResult{LocalIP: normalizedIP(localIP)}
	if !isExitProbeCandidateIP(localIP) && !localIP.IsLoopback() {
		result.Error = "local IP is not usable for exit probing"
		return result
	}

	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialer := &net.Dialer{
				LocalAddr: &net.TCPAddr{IP: localIP},
				Timeout:   cfg.Timeout,
			}
			return dialer.DialContext(ctx, network, addr)
		},
	}
	defer transport.CloseIdleConnections()

	client := &http.Client{
		Transport: transport,
		Timeout:   cfg.Timeout,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.ProbeURL, nil)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	resp, err := client.Do(req)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Error = fmt.Sprintf("probe returned HTTP %d", resp.StatusCode)
		return result
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	publicIP := strings.TrimSpace(string(body))
	if net.ParseIP(publicIP) == nil {
		result.Error = fmt.Sprintf("probe returned non-IP response: %q", publicIP)
		return result
	}

	result.PublicIP = publicIP
	return result
}

func probeExitIPs(ctx context.Context, cfg ExitProbeConfig, localIPs []string) []ExitProbeResult {
	results := make([]ExitProbeResult, 0, len(localIPs))
	for _, value := range localIPs {
		ip := net.ParseIP(strings.TrimSpace(value))
		if !isExitProbeCandidateIP(ip) {
			continue
		}
		results = append(results, probeExitIP(ctx, cfg, ip))
	}
	return results
}
