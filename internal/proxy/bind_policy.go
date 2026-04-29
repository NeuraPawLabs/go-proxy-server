package proxy

import (
	"net"
	"sort"
	"strings"
	"sync"

	"github.com/apeming/go-proxy-server/internal/logger"
)

// ExitBinding maps the local IP that accepted a proxy connection to the local
// IP used as the source address for outbound connections.
type ExitBinding struct {
	Name            string
	IngressLocalIP  string
	OutboundLocalIP string
}

// BindPolicy controls bind-listen source address selection.
type BindPolicy struct {
	Enabled      bool
	ExitBindings []ExitBinding
}

// BindDecision describes how an outbound local address was selected.
type BindDecision struct {
	BindEnabled     bool
	IngressLocalIP  string
	OutboundLocalIP string
	Mapped          bool
	Warning         string
}

var bindPolicyStore = struct {
	sync.RWMutex
	policy BindPolicy
}{}

// SetBindPolicy replaces the process-wide bind-listen mapping policy.
func SetBindPolicy(policy BindPolicy) {
	bindPolicyStore.Lock()
	defer bindPolicyStore.Unlock()
	bindPolicyStore.policy = policy
}

func currentBindPolicy(enabled bool) BindPolicy {
	bindPolicyStore.RLock()
	defer bindPolicyStore.RUnlock()
	policy := bindPolicyStore.policy
	policy.Enabled = enabled
	policy.ExitBindings = append([]ExitBinding(nil), policy.ExitBindings...)
	return policy
}

// ResolveOutboundLocalAddr returns the local address to use for outbound dials.
func (p BindPolicy) ResolveOutboundLocalAddr(ingressLocalAddr *net.TCPAddr) (*net.TCPAddr, BindDecision) {
	decision := BindDecision{BindEnabled: p.Enabled}
	if !p.Enabled {
		return nil, decision
	}
	if ingressLocalAddr == nil || ingressLocalAddr.IP == nil {
		decision.Warning = "bind-listen enabled but ingress local address is missing"
		return nil, decision
	}

	ingressIP := normalizedIP(ingressLocalAddr.IP)
	decision.IngressLocalIP = ingressIP
	if ingressLocalAddr.IP.IsUnspecified() {
		decision.Warning = "bind-listen enabled but ingress local address is unspecified"
		return nil, decision
	}
	if ingressLocalAddr.IP.IsLoopback() {
		decision.Warning = "bind-listen enabled with loopback ingress local address"
	}

	for _, binding := range p.ExitBindings {
		if !ipStringEqual(binding.IngressLocalIP, ingressIP) {
			continue
		}
		outboundIP := net.ParseIP(strings.TrimSpace(binding.OutboundLocalIP))
		if outboundIP == nil || outboundIP.IsUnspecified() {
			decision.Warning = "bind-listen exit binding has unusable outbound local IP"
			return nil, decision
		}
		decision.OutboundLocalIP = normalizedIP(outboundIP)
		decision.Mapped = true
		return &net.TCPAddr{IP: outboundIP}, decision
	}

	decision.OutboundLocalIP = ingressIP
	return &net.TCPAddr{IP: ingressLocalAddr.IP}, decision
}

func resolveOutboundLocalAddr(bindListen bool, ingressLocalAddr *net.TCPAddr) (*net.TCPAddr, BindDecision) {
	return currentBindPolicy(bindListen).ResolveOutboundLocalAddr(ingressLocalAddr)
}

func logBindDecision(proxyType, clientIP string, decision BindDecision) {
	if !decision.BindEnabled {
		return
	}
	if decision.Warning != "" {
		logger.Warn("%s bind-listen warning for client %s: %s (ingress_local_ip=%s outbound_local_ip=%s)",
			proxyType, clientIP, decision.Warning, decision.IngressLocalIP, decision.OutboundLocalIP)
		return
	}
	if decision.Mapped {
		logger.Info("%s bind-listen mapped client %s ingress_local_ip=%s outbound_local_ip=%s",
			proxyType, clientIP, decision.IngressLocalIP, decision.OutboundLocalIP)
		return
	}
	logger.Debug("%s bind-listen using ingress local IP for client %s: %s",
		proxyType, clientIP, decision.OutboundLocalIP)
}

// LogBindListenStartupDiagnostics logs host-side information useful for cloud
// EIP/NAT deployments where public EIPs may not exist on the guest NIC.
func LogBindListenStartupDiagnostics(proxyType string, port int, bindListen bool) {
	if !bindListen {
		return
	}

	ips, err := localInterfaceIPs()
	localIPs := make(map[string]struct{}, len(ips))
	for _, ip := range ips {
		localIPs[ip] = struct{}{}
	}
	if err != nil {
		logger.Warn("%s bind-listen enabled on port %d; failed to list local interface IPs: %v", proxyType, port, err)
	} else {
		logger.Info("%s bind-listen enabled on port %d; local interface IPs: %s", proxyType, port, strings.Join(ips, ", "))
	}

	policy := currentBindPolicy(true)
	if len(policy.ExitBindings) == 0 {
		logger.Warn("%s bind-listen has no explicit exit bindings; outbound source IP will follow the ingress local IP", proxyType)
		return
	}
	for _, binding := range policy.ExitBindings {
		name := strings.TrimSpace(binding.Name)
		if name == "" {
			name = "-"
		}
		logger.Info("%s bind-listen exit binding name=%s ingress_local_ip=%s outbound_local_ip=%s",
			proxyType, name, binding.IngressLocalIP, binding.OutboundLocalIP)
		if err == nil && !configuredIPIsLocal(binding.IngressLocalIP, localIPs) {
			logger.Warn("%s bind-listen exit binding name=%s ingress_local_ip=%s is not present on local interfaces",
				proxyType, name, binding.IngressLocalIP)
		}
		if err == nil && !configuredIPIsLocal(binding.OutboundLocalIP, localIPs) {
			logger.Warn("%s bind-listen exit binding name=%s outbound_local_ip=%s is not present on local interfaces",
				proxyType, name, binding.OutboundLocalIP)
		}
	}
}

func configuredIPIsLocal(value string, localIPs map[string]struct{}) bool {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return false
	}
	_, ok := localIPs[normalizedIP(ip)]
	return ok
}

func localInterfaceIPs() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, err
		}
		for _, addr := range addrs {
			var ip net.IP
			switch value := addr.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			if ip == nil || ip.IsUnspecified() {
				continue
			}
			seen[normalizedIP(ip)] = struct{}{}
		}
	}

	ips := make([]string, 0, len(seen))
	for ip := range seen {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	return ips, nil
}

func ipStringEqual(a, b string) bool {
	left := net.ParseIP(strings.TrimSpace(a))
	right := net.ParseIP(strings.TrimSpace(b))
	return left != nil && right != nil && left.Equal(right)
}

func normalizedIP(ip net.IP) string {
	if ip == nil {
		return ""
	}
	return ip.String()
}
