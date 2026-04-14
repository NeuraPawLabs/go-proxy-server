package security

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/apeming/go-proxy-server/internal/activity"
	"github.com/apeming/go-proxy-server/internal/cache"
	"github.com/apeming/go-proxy-server/internal/config"
	"github.com/apeming/go-proxy-server/internal/constants"
	"github.com/apeming/go-proxy-server/internal/logger"
)

// DNSCacheEntry stores DNS lookup results
type DNSCacheEntry struct {
	IPs []net.IP
}

var (
	// DNS cache with sharded LRU eviction for better concurrency
	dnsLRUCache *cache.ShardedLRU
	// DNS cache cleanup started flag
	dnsCacheCleanupStarted atomic.Bool
)

func init() {
	// Initialize sharded DNS LRU cache with 16 shards for better concurrency
	dnsLRUCache = cache.NewShardedLRU(constants.DNSCacheMaxSize, 16)
}

// cleanupDNSCache periodically removes expired entries from the DNS cache
func cleanupDNSCache() {
	ticker := time.NewTicker(constants.DNSCacheCleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		removed := dnsLRUCache.CleanExpired()
		if removed > 0 {
			logger.Info("Cleaned up %d expired DNS cache entries", removed)
		}
	}
}

// IsPrivateIP checks if an IP address is private/internal
// Uses Go standard library methods for reliable detection
func IsPrivateIP(ip net.IP) bool {
	// Check for loopback addresses (127.0.0.0/8, ::1)
	if ip.IsLoopback() {
		return true
	}

	// Check for link-local addresses (169.254.0.0/16, fe80::/10)
	if ip.IsLinkLocalUnicast() {
		return true
	}

	// Check for private addresses (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, fc00::/7)
	// Note: IsPrivate() was added in Go 1.17
	if ip.IsPrivate() {
		return true
	}

	return false
}

// CheckSSRF validates that the target host is not a private/internal address
// Returns error if the host is private or cannot be resolved
// Note: This is the initial check before connection. Use VerifyConnectedIP() after
// establishing connection to prevent DNS rebinding attacks.
func CheckSSRF(host string) error {
	// Check if private IP access is allowed
	if config.GetAllowPrivateIPAccess() {
		return nil // Allow access, skip SSRF check
	}

	// Start DNS cache cleanup goroutine on first call
	if dnsCacheCleanupStarted.CompareAndSwap(false, true) {
		go cleanupDNSCache()
	}

	// Parse host to extract IP or hostname
	// host can be "example.com:80" or "192.168.1.1:80" or just "example.com"
	hostOnly := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostOnly = h
	}

	// Try to parse as IP first
	if ip := net.ParseIP(hostOnly); ip != nil {
		if IsPrivateIP(ip) {
			return fmt.Errorf("access to private IP addresses is not allowed")
		}
		return nil
	}

	// If not an IP, resolve the hostname with caching
	var ips []net.IP

	// Check DNS LRU cache first
	if entry, ok := dnsLRUCache.Get(hostOnly); ok {
		// Cache hit and not expired (Get already checks expiration)
		if dnsEntry, ok := entry.Value.(DNSCacheEntry); ok {
			ips = dnsEntry.IPs
		}
	}

	// Cache miss or expired, perform DNS lookup
	if ips == nil {
		resolver := &net.Resolver{}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var err error
		ips, err = resolver.LookupIP(ctx, "ip", hostOnly)
		if err != nil {
			// DNS resolution failure could be used to bypass SSRF protection
			// Return error to prevent potential security bypass
			// Note: Don't log the hostname or error details to avoid leaking user's target destinations
			logger.Warn("DNS resolution failed during SSRF check")
			activity.RecordEvent(activity.EventRecord{
				Category:  "security",
				EventType: "dns_resolution_failed_during_ssrf_check",
				Severity:  activity.SeverityWarn,
				Source:    "security",
				Message:   "DNS resolution failed during SSRF validation",
			})
			return fmt.Errorf("failed to resolve hostname: %v", err)
		}

		// Store in LRU cache with TTL
		dnsLRUCache.Put(hostOnly, cache.Entry{
			Value:     DNSCacheEntry{IPs: ips},
			ExpiresAt: time.Now().Add(constants.DNSCacheTTL),
		})
	}

	// Check all resolved IPs
	for _, ip := range ips {
		if IsPrivateIP(ip) {
			return fmt.Errorf("hostname resolves to private IP address, access not allowed")
		}
	}

	return nil
}

// VerifyConnectedIP verifies that the actual connected IP is not private
// This prevents DNS rebinding attacks where DNS resolves to public IP initially
// but later resolves to private IP when connection is established
func VerifyConnectedIP(conn net.Conn) error {
	// Check if private IP access is allowed
	if config.GetAllowPrivateIPAccess() {
		return nil // Allow access, skip verification
	}

	if conn == nil {
		return fmt.Errorf("connection is nil")
	}

	remoteAddr := conn.RemoteAddr()
	if remoteAddr == nil {
		return fmt.Errorf("remote address is nil")
	}

	// Extract IP from remote address
	var ip net.IP
	switch addr := remoteAddr.(type) {
	case *net.TCPAddr:
		ip = addr.IP
	case *net.UDPAddr:
		ip = addr.IP
	default:
		// Try to parse address string
		host, _, err := net.SplitHostPort(remoteAddr.String())
		if err != nil {
			return fmt.Errorf("failed to parse remote address: %v", err)
		}
		ip = net.ParseIP(host)
		if ip == nil {
			return fmt.Errorf("failed to parse IP from remote address")
		}
	}

	// Verify the connected IP is not private
	if IsPrivateIP(ip) {
		return fmt.Errorf("connected to private IP address: %s (possible DNS rebinding attack)", ip.String())
	}

	return nil
}
