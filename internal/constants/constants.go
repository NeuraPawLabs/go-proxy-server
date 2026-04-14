package constants

import "time"

// Buffer sizes
const (
	// BufferSizeSmall is used for protocol handshake and small data transfers
	BufferSizeSmall = 8 * 1024 // 8KB

	// BufferSizeLarge is used for bulk data transfers
	BufferSizeLarge = 32 * 1024 // 32KB
)

// Configuration reload intervals
const (
	// ConfigReloadInterval is the interval for reloading configuration from database
	ConfigReloadInterval = 30 * time.Second

	// TimeoutReloadInterval is the interval for reloading timeout configuration
	TimeoutReloadInterval = 60 * time.Second
)

// Authentication and caching
const (
	// MaxRequestsBeforeReauth is the maximum number of requests before re-authentication
	// in HTTP Keep-Alive connections
	MaxRequestsBeforeReauth = 100

	// AuthCacheTTL is the time-to-live for authentication cache entries
	AuthCacheTTL = 5 * time.Minute

	// AuthCacheCleanupInterval is the interval for cleaning up expired auth cache entries
	AuthCacheCleanupInterval = 1 * time.Minute
)

// DNS caching
const (
	// DNSCacheTTL is the time-to-live for DNS cache entries
	DNSCacheTTL = 5 * time.Minute

	// DNSCacheCleanupInterval is the interval for cleaning up expired DNS cache entries
	DNSCacheCleanupInterval = 10 * time.Minute

	// DNSCacheMaxSize is the maximum number of entries in the DNS cache (LRU)
	DNSCacheMaxSize = 10000
)

// Connection pool settings
const (
	// HTTPPoolMaxIdleConns is the maximum number of idle connections in the HTTP pool
	HTTPPoolMaxIdleConns = 100

	// HTTPPoolMaxIdleConnsPerHost is the maximum number of idle connections per host
	HTTPPoolMaxIdleConnsPerHost = 10

	// HTTPPoolIdleConnTimeout is the timeout for idle connections in the pool
	HTTPPoolIdleConnTimeout = 90 * time.Second
)

// Database connection pool settings
const (
	// DBMaxIdleConns is the maximum number of idle database connections
	DBMaxIdleConns = 10

	// DBMaxOpenConns is the maximum number of open database connections
	DBMaxOpenConns = 100

	// DBConnMaxLifetime is the maximum lifetime of a database connection
	DBConnMaxLifetime = 1 * time.Hour
)

// Listener error handling
const (
	// MaxConsecutiveAcceptErrors is the maximum number of consecutive accept errors
	// before the listener is considered failed
	MaxConsecutiveAcceptErrors = 10

	// AcceptErrorBackoff is the backoff duration after an accept error
	AcceptErrorBackoff = 100 * time.Millisecond
)

// Concurrency limits
const (
	// MaxConcurrentConnections is the maximum number of concurrent connections
	// This prevents resource exhaustion under high load
	MaxConcurrentConnections = 10000

	// MaxConcurrentConnectionsPerIP is the maximum number of concurrent connections per IP
	// This prevents a single IP from consuming all resources
	MaxConcurrentConnectionsPerIP = 100
)
