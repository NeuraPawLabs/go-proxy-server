# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A dual-protocol proxy server implementation in Go supporting both SOCKS5 and HTTP/HTTPS protocols with username/password authentication, IP whitelist access control, and SQLite database storage. The server supports a special bind-listen mode for multi-IP exit routing and can run both proxy types simultaneously. Includes a web management interface that starts by default when run without arguments (suitable for Windows portable deployment).

## Project Structure

The project follows the standard Go project layout:

```
.
├── assets/                # Resource files (icons, etc.)
├── bin/                   # Build output directory
├── cmd/
│   └── server/           # Main application entry point
│       └── main.go
├── internal/             # Private application packages
│   ├── auth/            # Authentication and authorization
│   ├── autostart/       # Windows autostart management (COM interface)
│   ├── cache/           # Generic caching infrastructure
│   ├── config/          # Configuration management
│   ├── constants/       # Centralized configuration constants
│   ├── logger/          # Logging utilities
│   ├── models/          # Database models
│   ├── proxy/           # Proxy implementations (SOCKS5 and HTTP/HTTPS)
│   │   ├── socks5.go   # SOCKS5 protocol implementation
│   │   ├── http.go     # HTTP/HTTPS proxy implementation
│   │   ├── limiter.go  # Connection rate limiting
│   │   └── copy.go     # Data relay utilities
│   ├── security/        # SSRF and security protection
│   ├── singleinstance/  # Windows single instance check (named mutexes)
│   ├── tray/            # System tray (Windows only)
│   └── web/             # Web management server
│       ├── handlers.go  # HTTP API handlers
│       ├── manager.go   # Proxy server lifecycle management
│       ├── static.go    # Static file serving
│       └── dist/        # Frontend build artifacts (from web-ui/)
├── web-ui/               # Frontend source code (React + Vite + Ant Design)
│   ├── src/             # React components and application logic
│   │   ├── api/        # API client functions
│   │   ├── components/ # React components
│   │   ├── types/      # TypeScript type definitions
│   │   └── utils/      # Utility functions
│   ├── public/          # Static assets
│   ├── dist/            # Build output (copied to internal/web/dist/)
│   ├── package.json     # Node.js dependencies
│   └── vite.config.ts   # Vite build configuration
├── scripts/              # Build and utility scripts
├── docs/                 # Documentation files
│   └── archive/         # Archived documentation
├── Makefile             # Build automation
├── go.mod               # Go module definition
├── CLAUDE.md            # Claude Code project guide
└── README.md            # User documentation
```

## Build and Run Commands

### Build with Makefile (Recommended)
```bash
make build                  # Build for current platform (output: bin/go-proxy-server)
make build-linux           # Build for Linux (output: bin/go-proxy-server-linux-amd64)
make build-windows         # Build for Windows console mode (output: bin/go-proxy-server.exe)
make build-windows-gui     # Build for Windows GUI/tray mode (output: bin/go-proxy-server-gui.exe)
make build-darwin          # Build for macOS (output: bin/go-proxy-server-darwin-amd64)
make build-all             # Build for all platforms
make clean                 # Remove bin/ directory
```

**Note**: All binaries are output to the `bin/` directory to avoid confusion with the `cmd/server/` source directory.

### Build with Go directly
```bash
# Current platform (output to bin/ directory)
mkdir -p bin && go build -o bin/go-proxy-server ./cmd/server

# For Windows portable with system tray (recommended)
mkdir -p bin && GOOS=windows GOARCH=amd64 go build -ldflags "-H=windowsgui" -o bin/go-proxy-server-gui.exe ./cmd/server

# For Windows with console window visible
mkdir -p bin && GOOS=windows GOARCH=amd64 go build -o bin/go-proxy-server.exe ./cmd/server
```

### Default Behavior (No Arguments)

When run without arguments:
- **Windows**: Starts as system tray application (托盘程序)
  - Minimizes to system tray (notification area)
  - Shows icon in taskbar notification area
  - Right-click menu: "打开管理界面" (Open) and "退出" (Exit)
  - Web server runs in background on port 9090
  - Clicking "打开管理界面" opens browser to http://localhost:9090

- **Linux/macOS**: Starts web server directly on port 9090
  - Prints URL to console
  - Runs in foreground

```bash
./bin/go-proxy-server
# Windows: System tray application
# Other: Web server on http://localhost:9090
```

### Run Proxy Servers

```bash
# SOCKS5 only (standard mode)
./bin/go-proxy-server socks -port 1080

# HTTP only (standard mode)
./bin/go-proxy-server http -port 8080

# Both SOCKS5 and HTTP simultaneously
./bin/go-proxy-server both -socks-port 1080 -http-port 8080

# Bind-listen mode (multi-IP exit routing)
./bin/go-proxy-server socks -port 8888 -bind-listen
./bin/go-proxy-server http -port 8888 -bind-listen
./bin/go-proxy-server both -socks-port 1080 -http-port 8080 -bind-listen

# Web management interface
./bin/go-proxy-server web -port 9090
```

### User Management
```bash
# Add user (IP is optional, used for audit/logging only)
./bin/go-proxy-server adduser -username alice -password secret123 [-ip 192.168.1.100]

# Delete user (username is globally unique)
./bin/go-proxy-server deluser -username alice

# List users
./bin/go-proxy-server listuser
```

### IP Whitelist Management
```bash
# Add IP to whitelist
./bin/go-proxy-server addip -ip 192.168.1.100
```

## Architecture

### Core Components

**cmd/server/main.go** - Entry point and CLI command routing
- Initializes configuration with default paths in user data directory
- Initializes SQLite database with GORM
- **Default behavior (no arguments)**:
  - Windows: Attempts to start system tray application, falls back to web server if tray initialization fails
  - Non-Windows: Starts web server directly on port 9090 (or random port if 9090 is occupied)
  - Auto-starts proxies based on saved configuration (if `AutoStart` flag is set in database)
- Routes to subcommands: socks, http, both, web, adduser, deluser, listuser, addip
- Starts background goroutine for config reloading (every 30 seconds) in socks/http/both modes
- For `both` command: runs SOCKS5 in goroutine, HTTP in main thread, shared config reload
- For `web` command or no arguments: initializes web.Manager and starts web server
- Cross-platform data directory support via `config.GetDataDir()` (Windows/macOS/Linux/XDG)
- Windows single instance check prevents multiple instances from running simultaneously

**internal/proxy/socks5.go** - SOCKS5 protocol implementation
- `HandleSocks5Connection()`: Main connection handler with authentication flow
- Implements SOCKS5 handshake: method negotiation → authentication → request → relay
- Validates SOCKS5 version, CMD field, and authentication sub-protocol version
- Supports bind-listen mode: uses `net.Dialer{LocalAddr}` to bind outgoing connections to specific local IP
- Bidirectional data relay with proper TCP half-close handling (`CloseWrite()`)
- Returns precise error codes based on failure type (network unreachable, connection refused, etc.)

**internal/proxy/http.go** - HTTP/HTTPS proxy implementation
- `HandleHTTPConnection()`: Main HTTP connection handler
- `handleHTTPSConnect()`: HTTPS tunneling via CONNECT method (transparent tunnel)
- `handleHTTPRequest()`: Regular HTTP request forwarding (GET, POST, etc.)
- HTTP Basic authentication via Proxy-Authorization header
- Returns 407 Proxy Authentication Required on auth failure
- Supports bind-listen mode for both CONNECT and regular requests
- HTTP connection pooling for improved performance

**internal/auth/** - Authentication and authorization package (modular design)

**internal/auth/auth.go** - Core authentication logic
- `VerifyCredentials()`: Validates username/password with timing attack protection
- Uses constant-time comparison to prevent timing attacks
- Shared by both SOCKS5 and HTTP proxies

**internal/auth/user.go** - User management
- `LoadCredentialsFromDB()`: Loads user credentials from database with hot-reloading support
- `AddUser()`: Creates new user with password strength validation
- `DeleteUser()`: Removes user from database
- `ListUsers()`: Lists all users
- `validatePasswordStrength()`: Enforces password requirements (min 8 chars, letter + digit)
- Thread-safe credential storage using atomic.Value for lock-free reads

**internal/auth/whitelist.go** - IP whitelist management
- `CheckIPWhitelist()`: Checks if client IP is whitelisted (no automatic local bypass)
- `LoadWhitelistFromDB()`: Loads IP whitelist from database with hot-reloading
- `AddIPToWhitelist()`: Adds IP to whitelist with validation
- `DeleteIPFromWhitelist()`: Removes IP from whitelist
- `GetWhitelistIPs()`: Returns all whitelisted IPs
- Thread-safe whitelist storage using atomic.Value for lock-free reads

**internal/security/security.go** - SSRF and DNS rebinding protection
- `CheckSSRF()`: Validates target hosts to prevent SSRF attacks (blocks private IPs)
- `IsPrivateIP()`: Detects private/internal IP addresses (RFC 1918, RFC 3927, RFC 4193)
- `VerifyConnectedIP()`: Verifies actual connected IP to prevent DNS rebinding attacks
- `cleanupDNSCache()`: Periodic cleanup of expired DNS cache entries (10-minute interval)
- DNS caching with 5-minute TTL to reduce lookup overhead
- Used by both SOCKS5 and HTTP proxy implementations

**internal/cache/lru.go** - Generic caching infrastructure
- `ShardedLRU`: High-performance sharded LRU cache implementation
- `NewShardedLRU()`: Creates cache with configurable capacity and shard count
- 16 shards by default for reduced lock contention in high-concurrency scenarios
- Generic `Entry` type with expiration support
- Automatic eviction of least-recently-used entries when capacity is reached
- Thread-safe with per-shard locking
- Reusable by any package needing caching (currently used for DNS caching)

**internal/models/user.go** - Database schema
- `User` model with GORM: Username globally unique, IP field for audit/logging only
- `Whitelist` model with GORM: IP unique index
- `ProxyConfig` model: Stores proxy configuration (type, port, bind-listen, auto-start)
- `SystemConfig` model: Stores system-level configuration (key-value pairs)
- Password stored as SHA-256 hash with salt ([]byte, format: `$sha256$<salt>$<hash>`)

**internal/config/config.go** - Configuration management
- `Config` struct: DbPath
- `Load()`: Initializes configuration with default paths in user data directory
- `GetDataDir()`: Returns platform-specific user data directory
- `GlobalConfig`: Global configuration instance
- No external config file needed - all paths are automatically determined

**internal/constants/constants.go** - Centralized configuration constants
- `ConfigReloadInterval`: 30 seconds (config hot-reload interval)
- `TimeoutReloadInterval`: 60 seconds (timeout config reload interval)
- `DNSCacheCleanupInterval`: 10 minutes (DNS cache cleanup interval)
- `DNSCacheTTL`: 5 minutes (DNS cache entry TTL)
- `AuthCacheTTL`: 5 minutes (SOCKS5 authentication cache TTL)
- `MaxDNSCacheSize`: 10,000 entries (DNS cache capacity)
- Centralized constants for easy maintenance and consistency

**internal/autostart/autostart.go** - Windows autostart management
- `Enable()`: Creates shortcut in Windows startup folder via COM interface
- `Disable()`: Removes shortcut from startup folder
- Uses `github.com/go-ole/go-ole` for COM automation (no VBScript)
- Startup folder path: `%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup`
- Pure Go implementation to avoid antivirus false positives

**internal/singleinstance/singleinstance.go** - Windows single instance check
- `Check()`: Ensures only one instance of the application runs
- Uses Windows named mutexes for inter-process synchronization
- Prevents multiple instances from conflicting on ports or database
- Windows-only implementation (build tag: `// +build windows`)

**internal/proxy/limiter.go** - Connection rate limiting
- Per-IP connection rate limiting with configurable limits
- Global connection limit enforcement
- Thread-safe implementation with sync.Map
- Automatic cleanup of expired rate limit entries
- Configurable via database (system_configs table)

**internal/proxy/copy.go** - Data relay utilities
- `copyWithTimeout()`: Bidirectional data relay with timeout support
- Proper TCP half-close handling (CloseWrite/CloseRead)
- Error channel synchronization for goroutine coordination
- Used by both SOCKS5 and HTTP proxy implementations

**internal/logger/logger.go** - Logging utilities
- `Init()`: Initializes logging to file for Windows GUI mode
- `Close()`: Closes the log file
- `Info()`, `Error()`: Logging functions with level prefixes

**internal/web/manager.go** - Web management server
- `ProxyServer` struct: Manages individual proxy server lifecycle (Running flag, Listener, Port, BindListen)
- `Manager` struct: Central manager for web interface and proxy servers
- `NewManager()`: Factory function to create web manager
- `StartServer()`: Starts HTTP server on localhost only (security feature) on specified port
- `startProxy()`: Starts proxy in goroutine, manages listener and config reload
- `stopProxy()`: Stops proxy server and closes listener
- Thread-safe proxy state management with sync.RWMutex

**internal/web/handlers.go** - HTTP API handlers
- API endpoints:
  - `GET /`: Serves the HTML interface
  - `GET /api/status`: Returns current proxy server status (running/stopped, ports, bindListen)
  - `GET /api/users`: Lists all users from database
  - `POST /api/users`: Adds new user
  - `DELETE /api/users`: Deletes user
  - `GET /api/whitelist`: Lists all whitelist IPs
  - `POST /api/whitelist`: Adds IP to whitelist
  - `DELETE /api/whitelist`: Deletes IP from whitelist
  - `POST /api/proxy/start`: Dynamically starts proxy server (socks5 or http)
  - `POST /api/proxy/stop`: Dynamically stops proxy server
  - `POST /api/proxy/config`: Updates proxy configuration (port, bind-listen)
  - `POST /api/config`: Updates system configuration (rate limits, timeouts)
  - `POST /api/shutdown`: Gracefully shuts down the application
- RESTful API design with JSON request/response
- Error handling with appropriate HTTP status codes

**internal/web/static.go** - Static file serving
- Serves frontend build artifacts from `internal/web/dist/`
- Embedded filesystem using Go 1.16+ embed directive
- Serves React SPA with proper routing fallback

**web-ui/** - Frontend application (React + TypeScript + Vite + Ant Design)
- Modern React-based web interface built with Vite
- TypeScript for type safety
- Ant Design component library for UI
- Features:
  - Proxy control cards for SOCKS5 and HTTP (start/stop, port config, bind-listen toggle)
  - Real-time status updates (polls every 5 seconds)
  - User management table (add, delete, list with creation time)
  - IP whitelist management (add, delete, list)
  - System configuration (rate limits, timeouts)
  - Success/error message display with notifications
- Build output copied to `internal/web/dist/` for embedding
- Development server: `npm run dev` (in web-ui/ directory)
- Production build: `npm run build` (outputs to web-ui/dist/)

**internal/tray/tray_windows.go** (Windows only, build tag: `// +build windows`)
- System tray application for Windows
- `Start()`: Entry point for tray application
- `onReady()`: Initializes tray icon and menu
  - Sets tray icon (green dot)
  - Creates menu items: "打开管理界面" and "退出"
  - Starts web server in background goroutine
- `onExit()`: Cleanup when application exits
- `openBrowser()`: Opens default browser to management interface
- `getIcon()`: Returns embedded icon data (ICO format)
- Uses `github.com/getlantern/systray` library

**internal/tray/tray_other.go** (Non-Windows, build tag: `// +build !windows`)
- Stub implementation for non-Windows platforms
- `Start()`: Prints message that tray is Windows-only

### Authentication Flow

**SOCKS5:**
1. Client connects → Check IP whitelist first
2. If IP whitelisted → Skip authentication (method 0x00)
3. If not whitelisted → Require username/password (method 0x02)
4. Validate credentials against in-memory map (loaded from database)
5. Proceed with SOCKS5 request handling

**HTTP:**
1. Client connects → Parse HTTP request
2. Check IP whitelist first
3. If IP whitelisted → Skip authentication
4. If not whitelisted → Check Proxy-Authorization header (HTTP Basic)
5. If auth missing/invalid → Return 407 Proxy Authentication Required
6. Handle CONNECT (HTTPS tunnel) or regular HTTP request

### Bind-Listen Mode

When `-bind-listen` flag is enabled:
- Server binds to 0.0.0.0 but has multiple public IPs (e.g., IPa, IPb, IPc)
- Client connects to specific IP (e.g., IPa:8888)
- Server uses that local IP as source for outgoing connections via `net.Dialer{LocalAddr}`
- Enables per-client exit IP routing without multiple server instances
- Works for both SOCKS5 and HTTP proxies

### Both Mode (Simultaneous Proxies)

When using `both` command:
- Single shared config reload goroutine (30-second interval)
- SOCKS5 server runs in separate goroutine
- HTTP server runs in main goroutine
- Both share same credentials and whitelist (thread-safe access)
- Independent port configuration: `-socks-port` and `-http-port`
- Single `-bind-listen` flag applies to both proxies

### Web Management Mode

When using `web` command:
- Starts HTTP web server on specified port (default: 9090, uses random port if occupied)
- **Security**: Listens only on localhost (127.0.0.1), not accessible from external network
- Proxies can be started/stopped dynamically via API or auto-started based on saved configuration
- Provides browser-based management interface
- Proxies can be started/stopped dynamically via API:
  - Each proxy runs in separate goroutine when started
  - Independent config reload goroutine per running proxy
  - Clean shutdown with listener.Close() on stop
- Thread-safe proxy state management with sync.RWMutex
- Suitable for Windows portable deployment:
  - No console interaction required
  - All configuration via web browser
  - Can compile with `-ldflags -H=windowsgui` to hide console
- Web server blocks in main goroutine (http.ListenAndServe)
- Auto-starts proxies if `AutoStart` flag is set in database (proxy_configs table)

### Concurrency Model

- One goroutine per client connection (`handleConnection` or `handleHTTPConnection`)
- Background goroutine for config reloading (30-second interval) in socks/http/both modes
- Thread-safe credential and whitelist access with RWMutex
- Bidirectional relay uses two goroutines with error channel synchronization
- In `both` mode: SOCKS5 listener in goroutine, HTTP listener in main thread
- In `web` mode:
  - Web server runs in main goroutine (blocking)
  - Each dynamically started proxy runs in separate goroutine
  - Config reload goroutine per running proxy
  - Thread-safe proxy state access with WebManager.mu (RWMutex)

## Configuration

### Data Directory

Data files (database and logs) are stored in the user data directory:
- **Windows**: `%APPDATA%\go-proxy-server\` (e.g., `C:\Users\Username\AppData\Roaming\go-proxy-server\`)
- **macOS**: `~/Library/Application Support/go-proxy-server/`
- **Linux/Unix**: `~/.local/share/go-proxy-server/`
- **XDG compliant**: `$XDG_DATA_HOME/go-proxy-server/`

The data directory is automatically created on first run.

### Database

**data.db**: SQLite database with GORM
- Auto-migrated tables: `users`, `whitelists`, `proxy_configs`, and `system_configs`
- `users` table: Stores user credentials with globally unique username (IP field for audit only)
- `whitelists` table: Stores IP whitelist entries
- `proxy_configs` table: Stores proxy configuration (port, bind-listen, auto-start)
- `system_configs` table: Stores system-level configuration (key-value pairs)
- Password stored as SHA-256 hash with salt

**Note**: All data (users, passwords, IP whitelist, proxy configurations) is stored in the database for easy management and backup. No separate text files are used.

## Dependencies

- `gorm.io/gorm` - Database ORM
- `github.com/glebarez/sqlite` - Pure Go SQLite driver (no CGO required)
- `modernc.org/sqlite` - SQLite implementation in pure Go
- `github.com/getlantern/systray` - System tray icon for Windows
- `github.com/go-ole/go-ole` - COM automation for Windows (autostart management)
- Standard library for networking, HTTP, concurrency, and cryptography (crypto/sha256, crypto/rand)

**Important**: This project uses a pure Go implementation of SQLite (`github.com/glebarez/sqlite`), which does not require CGO. This makes cross-compilation much easier, especially for Windows targets.

## Key Implementation Details

- Passwords are hashed with SHA-256 + random 32-byte salt (format: `$sha256$<salt>$<hash>`)
- **Performance**: Password verification completes in ~0.22 microseconds (222,000× faster than bcrypt)
- **Security**: All connections require explicit authentication (whitelist or credentials)
- No automatic bypass for local connections - must be explicitly whitelisted if needed
- **SSRF Protection**: Blocks access to private IP addresses (127.0.0.0/8, 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 169.254.0.0/16, IPv6 private ranges)
- **Timing Attack Protection**: Constant-time credential verification to prevent username enumeration
- **Connection Rate Limiting**: Per-IP and global connection limits with configurable thresholds
- **Authentication Cache**: SOCKS5 authentication results cached for 5 minutes to reduce database load
- **DNS Caching**: LRU cache with 10,000 entry limit and 5-minute TTL to reduce DNS lookup overhead
- **HTTP Connection Pooling**: Reuses connections to destination servers for improved performance
- **Single Instance Check**: Windows-only feature prevents multiple instances from running simultaneously
- **Autostart Management**: Windows startup folder integration via COM interface (no registry modification)
- SOCKS5 supports IPv4 (0x01), IPv6 (0x04), and domain name (0x03) address types
- SOCKS5 validates protocol version, CMD field, and authentication sub-protocol version
- SOCKS5 returns precise error codes (network unreachable, host unreachable, connection refused, timeout, etc.)
- HTTP proxy supports CONNECT method (HTTPS tunneling) and regular HTTP methods
- HTTP authentication uses Proxy-Authorization header with Basic scheme
- Proper TCP connection cleanup with half-close support
- Config hot-reload without server restart (30-second interval for credentials/whitelist, 60-second interval for timeouts)
- Both proxy types share same user database and whitelist
- Database operations use unique constraints to prevent race conditions
