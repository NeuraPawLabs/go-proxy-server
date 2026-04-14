# Changelog

[中文版本](CHANGELOG.zh-CN.md)

All notable changes to this project are documented in this file.

## [Unreleased]

### Added

- Centralized TCP tunnel management with one `tunnel-server`, multiple `tunnel-client` agents, and route control from Web UI or CLI
- Web tunnel management page and related API endpoints
- Bilingual public documentation entry points

### Changed

- Simplified the public documentation set into a smaller open-source friendly structure
- Updated tunnel workflow from per-port client commands to persistent client agents with server-side route management
- Web admin documentation now reflects random-port startup behavior by default

### Fixed

- Improved tunnel transport safety with TLS-first defaults, handshake limits, message size limits, and pending connection caps
- Fixed stale or outdated command and logging examples in the main documentation

## [1.4.0] - 2026-01-18

### Added

- Authentication cache for SOCKS5 requests
- HTTP connection pooling and improved DNS cache behavior
- Hot reload for timeout-related configuration
- Centralized constants and database connection pool settings

### Changed

- Reduced configuration reload pressure and cleaned up duplicate startup logic
- Improved maintainability of proxy startup and runtime configuration code

### Fixed

- Listener shutdown handling and error backoff behavior
- SOCKS5 and HTTP runtime stability under higher concurrency

## [1.3.0] - 2026-01-17

### Added

- SSRF protection for proxy targets
- Timing-attack mitigation in credential verification
- IPv6 support in SOCKS5 request handling

### Changed

- Unified user model around globally unique usernames
- Simplified user deletion semantics and related APIs

### Fixed

- SOCKS5 protocol validation gaps
- HTTP proxy request handling issues with long-lived connections
- Race conditions around user and allowlist persistence

## [1.2.1] - 2026-01-17

### Changed

- Adjusted connection timeout strategy to avoid premature termination of long-lived requests
- Enforced global username uniqueness in persisted user data

### Fixed

- Connection establishment timeout handling
- HTTP request body forwarding bugs
- Missing write-error checks and panic-prone type assertions

## [1.2.0] - 2026-01-17

### Changed

- Replaced script-based Windows shortcut creation with a pure Go implementation
- Reduced Windows antivirus false positives related to startup shortcut handling

## [1.1.0] - Previous Release

### Changed

- Switched from registry-based autostart to Startup-folder shortcuts
- Added Windows resource metadata and improved build automation

## [1.0.0] - Initial Release

### Added

- SOCKS5 and HTTP/HTTPS proxy support
- Username/password authentication and IP allowlist support
- Web management interface and Windows tray mode
- SQLite-based persistence
