# Go Proxy Server

[中文说明](README.zh-CN.md) | [Documentation](docs/README.md) | [Change Log](CHANGELOG.md) | [中文更新日志](CHANGELOG.zh-CN.md)

Go Proxy Server is a self-hosted Go service that combines local proxy access, a localhost-only Web admin UI, and centralized tunnel management in one binary.

## What It Does

- **Proxy services:** Run SOCKS5, HTTP, or both at the same time.
- **Web admin:** Manage users, allowlists, logs, proxy config, and tunnel routes from a local Web UI.
- **Tunnel control plane:** Run one `tunnel-server` and manage multiple long-lived `tunnel-client` agents.
- **Security features:** Username/password auth, IP allowlist, SSRF and DNS rebinding protection, audit logs, and event logs.
- **Cross-platform runtime:** Linux and macOS default to Web mode, Windows prefers tray mode.

## Capability Overview

### Proxy and Access Control

- SOCKS5 proxy support
- HTTP/HTTPS proxy support
- Username/password authentication with salted SHA-256 storage
- IP allowlist support
- Optional `-bind-listen` mode for multi-address hosts
- Runtime reload for auth, timeout, and limiter configuration

### Web Admin and Operations

- Local-only Web admin UI bound to `localhost`
- Proxy start/stop and saved configuration management
- Audit log and event log center in the Web UI
- SQLite persistence with a pure Go driver
- Default fallback page for test/build environments without embedded frontend assets

### Tunnel Management

- Centralized model: one `tunnel-server`, multiple `tunnel-client` agents
- Route management from Web UI or CLI
- `classic` engine for TCP
- `quic` engine for TCP and UDP
- Auto-assigned public ports within a configured port range

### Platform Behavior

- Linux/macOS: no-argument startup launches the local Web admin UI
- Windows: no-argument startup prefers tray mode, then falls back to Web mode
- No-argument startup restores saved proxy services marked with `AutoStart`
- No-argument startup does not auto-start `tunnel-server` or `tunnel-client`

## Runtime Modes

| Mode | Command | What it does |
| --- | --- | --- |
| Default | `./bin/go-proxy-server` | Starts Web admin on Linux/macOS, tray or Web mode on Windows |
| Web admin | `./bin/go-proxy-server web` | Starts the localhost-only Web UI |
| SOCKS5 | `./bin/go-proxy-server socks` | Starts a foreground SOCKS5 proxy |
| HTTP | `./bin/go-proxy-server http` | Starts a foreground HTTP/HTTPS proxy |
| Both proxies | `./bin/go-proxy-server both` | Starts SOCKS5 and HTTP/HTTPS together |
| Tunnel server | `./bin/go-proxy-server tunnel-server ...` | Starts centralized tunnel server mode |
| Tunnel client | `./bin/go-proxy-server tunnel-client ...` | Starts a managed tunnel client agent |

## Quick Start

### Build

```bash
make build
```

- `make build` builds the frontend first, then compiles Go with the `frontend_embed` tag.
- `internal/web/dist` is a build artifact and is not committed.
- `go test ./...` still works from a clean checkout and serves a small fallback page instead of the full UI bundle.

### Start the Web Admin UI

```bash
./bin/go-proxy-server web
```

- The Web UI binds to `localhost` only.
- If no port is specified, the server selects a random available port and prints the actual URL.

### Start Proxy Services Directly

```bash
# SOCKS5 only
./bin/go-proxy-server socks

# HTTP/HTTPS only
./bin/go-proxy-server http

# Both proxy types
./bin/go-proxy-server both
```

```bash
# Explicit ports
./bin/go-proxy-server socks -port 1080
./bin/go-proxy-server http -port 8080
./bin/go-proxy-server both -socks-port 1080 -http-port 8080
```

- These CLI modes run in the foreground until `Ctrl+C`.
- These modes use current CLI flags only. They do not restore saved proxy ports from the Web UI.
- They still load users and allowlist state from SQLite.

### Add a User and Start a SOCKS5 Proxy

```bash
./bin/go-proxy-server adduser -username alice -password secret123
./bin/go-proxy-server socks -port 1080
```

## Common Commands

### User and Allowlist Management

```bash
./bin/go-proxy-server adduser -username alice -password secret123
./bin/go-proxy-server deluser -username alice
./bin/go-proxy-server listuser

./bin/go-proxy-server addip -ip 192.168.1.100
./bin/go-proxy-server delip -ip 192.168.1.100
./bin/go-proxy-server listip
```

### Proxy Commands

```bash
./bin/go-proxy-server socks -port 1080 [-bind-listen]
./bin/go-proxy-server http -port 8080 [-bind-listen]
./bin/go-proxy-server both -socks-port 1080 -http-port 8080 [-bind-listen]
```

### Help and Version

```bash
./bin/go-proxy-server --help
./bin/go-proxy-server --version
```

## Tunnel Overview

Go Proxy Server supports centralized tunnel management with two engines:

- **`classic`:** TCP only, simpler deployment model
- **`quic`:** TCP and UDP, better fit for mixed protocols and UDP routes

Classic example:

```bash
# server
./bin/go-proxy-server tunnel-server \
  -engine classic \
  -listen :7000 \
  -auto-port-start 30000 \
  -auto-port-end 30999 \
  -token demo-secret \
  -cert server.crt \
  -key server.key

# client
./bin/go-proxy-server tunnel-client \
  -engine classic \
  -server your.server.example:7000 \
  -token demo-secret \
  -client node-shanghai-01 \
  -ca ca.pem
```

QUIC example:

```bash
# server
./bin/go-proxy-server tunnel-server \
  -engine quic \
  -listen :7443 \
  -auto-port-start 30000 \
  -auto-port-end 30999 \
  -token demo-secret \
  -cert server.crt \
  -key server.key

# client
./bin/go-proxy-server tunnel-client \
  -engine quic \
  -server your.server.example:7443 \
  -token demo-secret \
  -client node-shanghai-01 \
  -ca ca.pem
```

Tunnel notes:

- `-public-port 0` means auto-assign a public port.
- `udp` routes require the `quic` engine on both server and client.
- The Web UI supports both server-side and client-side tunnel workbenches.
- CLI still exposes insecure flags for testing and migration, but verified TLS is the recommended default.
- Full certificate and route examples live in [docs/tunnel.md](docs/tunnel.md).

## Configuration and Runtime Notes

### `.env` Support

The binary loads a local `.env` before startup configuration is initialized.

- Search order: current working directory, then the executable directory
- Existing shell environment variables win over `.env`
- Recommended for local development only

Example:

```bash
cat > .env <<'EOF'
GEETEST_ID=your_geetest_id
GEETEST_KEY=your_geetest_key
EOF
```

Captcha behavior:

- If both `GEETEST_ID` and `GEETEST_KEY` are unset, captcha is disabled
- If both are set, captcha is enabled
- If only one is set, login is rejected as a configuration error

### Data and Logs

- Application data is stored in the platform-specific app data directory
- Main SQLite file: `data.db`
- CLI and Web modes log to stdout/stderr and `app.log`
- Windows tray mode writes operational logs to `app.log` in the app data directory

### Logs in the Web UI

The Web admin includes a dedicated Logs page backed by SQLite:

- **Audit logs:** admin-plane changes such as login/logout, user updates, allowlist changes, proxy config changes, tunnel management actions
- **Event logs:** runtime and security signals such as auth failures, captcha failures, SSRF/DNS protection hits, tunnel failures, and operational warnings

## Documentation Map

Start here if you want more detail:

- [Documentation index](docs/README.md)
- [Getting started](docs/getting-started.md)
- [Tunnel management](docs/tunnel.md)
- [Windows guide](docs/windows.md)
- [Chinese documentation index](docs/README.zh-CN.md)

## Contributing and Security

- [Contributing guide](CONTRIBUTING.md)
- [Security policy](SECURITY.md)
- [Architecture notes](CLAUDE.md)
