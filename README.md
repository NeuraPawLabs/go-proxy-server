# Go Proxy Server

[中文说明](README.zh-CN.md) | [Documentation](docs/README.md) | [Change Log](CHANGELOG.md) | [中文更新日志](CHANGELOG.zh-CN.md)

Go Proxy Server is a compact self-hosted proxy and tunnel service written in Go. It provides SOCKS5 and HTTP proxying, local-only Web administration, Windows tray support, and a centralized tunnel control plane for managing multiple connected clients from a single server.

## Highlights

- SOCKS5 and HTTP/HTTPS proxy support
- Username/password auth with salted SHA-256 storage
- IP allowlist support
- SQLite storage with a pure Go driver
- Local-only Web admin UI
- Built-in audit log and event log center in the Web UI
- Windows tray mode and CLI mode
- Centralized tunnel management
  - one `tunnel-server`
  - multiple long-lived `tunnel-client` agents
  - route management from Web UI or CLI
  - `classic` engine for TCP
  - `quic` engine for TCP and UDP
- Runtime config reload for auth, timeout, and limiter settings

## Quick Start

### Build

```bash
make build
```

- `internal/web/dist` is generated during build and is not committed to Git.
- `make build` and the GitHub workflows build the frontend first, then compile Go with the `frontend_embed` tag.
- A plain `go test ./...` from a clean checkout still works and serves a small fallback page instead of the full Web UI bundle.

### Start the Web admin UI

```bash
./bin/go-proxy-server web
```

The Web UI binds to `localhost` only. If no port is specified, it uses a random available port and prints the actual URL in the startup log.

### Environment configuration with `.env`

The server now supports loading a local `.env` file before startup configuration is initialized.

- Lookup order: `.env` in the current working directory, then `.env` next to the executable
- Existing shell environment variables win over `.env`
- Use `.env.example` as the template for local development

Example:

```bash
cp .env.example .env
```

```env
GEETEST_ID=your_geetest_id
GEETEST_KEY=your_geetest_key
```

Use `.env` for local development only. In production, prefer real environment variables or your service manager's secret injection.

Behavior summary:

- If both `GEETEST_ID` and `GEETEST_KEY` are unset, captcha is disabled and admin login uses password only
- If both are set, captcha is enabled for admin login
- If only one is set, login is rejected as a configuration error

### Audit and event logs

The Web admin now includes a dedicated `日志中心` / Logs page backed by SQLite persistence.

- `Audit logs` capture who changed what in the admin plane
  - bootstrap and admin login/logout
  - user and allowlist maintenance
  - proxy start/stop and proxy config changes
  - system config updates, shutdown requests
  - tunnel server, certificates, and route management
- `Event logs` capture important runtime and security signals
  - tunnel client connect/disconnect and route expose results
  - auth and captcha failures
  - rate-limit blocks, SSRF / DNS rebinding protection hits
  - tunnel data-plane failures and other operational warnings

Both log streams are stored in the same application SQLite database (`data.db` in the app data directory) and can be filtered by time window, category, severity, status, and keyword from the Web UI.

### Start with no arguments

```bash
./bin/go-proxy-server
```

- Linux and macOS: starts the local Web admin UI directly
- Windows: tries tray mode first, then falls back to the local Web admin UI if tray startup fails
- restores proxy services that were previously saved with `AutoStart`
- does not start `tunnel-server` or `tunnel-client` automatically

### Add a user and start a SOCKS5 proxy

```bash
./bin/go-proxy-server adduser -username alice -password secret123
./bin/go-proxy-server socks -port 1080
```

### Start centralized tunnel mode

Classic engine example:

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

# create a TCP route from CLI
./bin/go-proxy-server tunnel-save-route \
  -client node-shanghai-01 \
  -name mysql-prod \
  -protocol tcp \
  -target 127.0.0.1:3306 \
  -public-port 33060 \
  -enabled
```

QUIC engine example with UDP:

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

# create a UDP route from CLI
./bin/go-proxy-server tunnel-save-route \
  -client node-shanghai-01 \
  -name dns-local \
  -protocol udp \
  -target 127.0.0.1:53 \
  -public-port 1053 \
  -udp-idle-timeout 60 \
  -udp-max-payload 1200 \
  -enabled
```

Certificate generation example: see `docs/tunnel.md`.

Notes:

- `-public-port 0` means auto-assign a public port for the route.
- If `-auto-port-start` and `-auto-port-end` are set on `tunnel-server`, auto-assignment stays inside that range.
- This is useful when your cloud firewall or security group reserves a dedicated tunnel port block.
- `udp` routes require the `quic` engine on both server and client.
- The Web UI supports both `server` and `client` workbench modes.
- The Web UI client mode uses verified TLS only. Upload a CA file, or let it automatically reuse the locally managed server CA when the same node also runs tunnel server mode.
- CLI still exposes `-allow-insecure` and `-insecure-skip-verify` for testing and migration, but they are not recommended for production.

## Documentation

- [Documentation index](docs/README.md)
- [Getting started](docs/getting-started.md)
- [Tunnel management](docs/tunnel.md)
- [Chinese documentation index](docs/README.zh-CN.md)
- [Windows guide (Chinese)](docs/windows.zh-CN.md)

## Project Status

This repository is organized as an open-source project, but some historical operational notes in `docs/` are still more detailed than typical public-facing docs. The main entry points above are the recommended starting points.

## For Contributors

- [Contributing guide](CONTRIBUTING.md)
- [Security policy](SECURITY.md)
- [Architecture notes](CLAUDE.md)

## Platform Notes

- Linux and macOS default to Web admin mode when no command is provided.
- Windows prefers tray mode when no command is provided.
- No-argument startup may automatically restore saved proxy services marked with `AutoStart`.
- No-argument startup does not automatically start tunnel server/client processes.
- CLI mode logs to stdout/stderr by default.
- Windows tray mode writes logs to `app.log` in the application data directory.
