package proxy

import (
	"sync"
	"sync/atomic"

	"github.com/apeming/go-proxy-server/internal/config"
)

// ConnectionLimiter limits the number of concurrent connections globally and per IP
type ConnectionLimiter struct {
	// Global semaphore for total connection limit
	globalSem chan struct{}
	// Per-IP connection counters
	perIPCounters sync.Map // map[string]*int32
	// Current total connections (for metrics)
	totalConnections atomic.Int64
}

// NewConnectionLimiter creates a new connection limiter
func NewConnectionLimiter() *ConnectionLimiter {
	cfg := config.GetLimiterConfig()
	// If MaxConcurrentConnections is 0 (unlimited), use max int32 for channel size
	semSize := cfg.MaxConcurrentConnections
	if semSize == 0 {
		semSize = 2147483647 // int32 max value for unlimited
	}
	return &ConnectionLimiter{
		globalSem: make(chan struct{}, semSize),
	}
}

// Acquire attempts to acquire a connection slot for the given IP
// Returns true if successful, false if the limit is reached
func (cl *ConnectionLimiter) Acquire(clientIP string) bool {
	// Get current limit from configuration
	cfg := config.GetLimiterConfig()

	// Check global limit (skip if 0 = unlimited)
	if cfg.MaxConcurrentConnections > 0 {
		// Try to acquire global semaphore (non-blocking)
		select {
		case cl.globalSem <- struct{}{}:
			// Global limit not reached, continue to per-IP check
		default:
			// Global limit reached
			return false
		}
	}

	// Check per-IP limit (skip if 0 = unlimited)
	if cfg.MaxConcurrentConnectionsPerIP > 0 {
		// Check and increment per-IP counter
		counterInterface, _ := cl.perIPCounters.LoadOrStore(clientIP, new(int32))
		counter := counterInterface.(*int32)

		// Atomically increment and check if limit exceeded
		newCount := atomic.AddInt32(counter, 1)
		if newCount > cfg.MaxConcurrentConnectionsPerIP {
			// Per-IP limit exceeded, rollback
			atomic.AddInt32(counter, -1)
			// Release global semaphore if it was acquired
			if cfg.MaxConcurrentConnections > 0 {
				<-cl.globalSem
			}
			return false
		}
	}

	// Successfully acquired, increment total counter
	cl.totalConnections.Add(1)
	return true
}

// Release releases a connection slot for the given IP
func (cl *ConnectionLimiter) Release(clientIP string) {
	// Get current configuration to check if limits are enabled
	cfg := config.GetLimiterConfig()

	// Decrement per-IP counter (only if per-IP limit is enabled)
	if cfg.MaxConcurrentConnectionsPerIP > 0 {
		if counterInterface, ok := cl.perIPCounters.Load(clientIP); ok {
			counter := counterInterface.(*int32)

			// Use CAS loop to ensure we don't go below zero
			for {
				oldCount := atomic.LoadInt32(counter)
				if oldCount <= 0 {
					// Already at zero or negative (shouldn't happen), don't decrement
					break
				}
				newCount := oldCount - 1
				if atomic.CompareAndSwapInt32(counter, oldCount, newCount) {
					// Successfully decremented
					// Clean up counter if it reaches zero to prevent memory leak
					if newCount == 0 {
						cl.perIPCounters.Delete(clientIP)
					}
					break
				}
				// CAS failed, retry
			}
		}
	}

	// Release global semaphore (only if global limit is enabled)
	if cfg.MaxConcurrentConnections > 0 {
		select {
		case <-cl.globalSem:
		default:
			// Should not happen, but handle gracefully
		}
	}

	// Decrement total counter (use Add with negative value, which is atomic)
	newTotal := cl.totalConnections.Add(-1)
	// Ensure total doesn't go negative
	if newTotal < 0 {
		cl.totalConnections.Store(0)
	}
}

// GetTotalConnections returns the current number of active connections
func (cl *ConnectionLimiter) GetTotalConnections() int64 {
	return cl.totalConnections.Load()
}

// GetPerIPConnections returns the current number of connections for a given IP
func (cl *ConnectionLimiter) GetPerIPConnections(clientIP string) int32 {
	if counterInterface, ok := cl.perIPCounters.Load(clientIP); ok {
		counter := counterInterface.(*int32)
		return atomic.LoadInt32(counter)
	}
	return 0
}

// Global connection limiter instances
var (
	socks5Limiter atomic.Pointer[ConnectionLimiter]
	httpLimiter   atomic.Pointer[ConnectionLimiter]
)

func init() {
	socks5Limiter.Store(NewConnectionLimiter())
	httpLimiter.Store(NewConnectionLimiter())
}

// GetSOCKS5Limiter returns the global SOCKS5 connection limiter
func GetSOCKS5Limiter() *ConnectionLimiter {
	return socks5Limiter.Load()
}

// GetHTTPLimiter returns the global HTTP connection limiter
func GetHTTPLimiter() *ConnectionLimiter {
	return httpLimiter.Load()
}

// RecreateLimiters recreates the global limiters with new configuration
// This should be called when the connection limit configuration is updated
func RecreateLimiters() {
	socks5Limiter.Store(NewConnectionLimiter())
	httpLimiter.Store(NewConnectionLimiter())
}
