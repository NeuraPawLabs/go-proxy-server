# Getting Started

[中文版本](getting-started.zh-CN.md)

This guide covers the main ways to run Go Proxy Server.

## Build

```bash
make build
```

The binary is written to `bin/go-proxy-server` on the current platform.

## Run Modes

### Default startup

```bash
./bin/go-proxy-server
```

- Linux and macOS start the local Web admin UI directly
- Windows prefers tray mode and falls back to the local Web admin UI on failure
- previously saved proxy services with `AutoStart` may be restored automatically
- tunnel server/client processes are not started automatically

### Web admin mode

```bash
./bin/go-proxy-server web
```

- binds to `localhost` only
- defaults to a random available port when `-port` is omitted or `0`
- prints the actual URL in the startup log

### SOCKS5 proxy

```bash
./bin/go-proxy-server socks -port 1080
```

### HTTP proxy

```bash
./bin/go-proxy-server http -port 8080
```

### Both SOCKS5 and HTTP

```bash
./bin/go-proxy-server both -socks-port 1080 -http-port 8080
```

## User and Allowlist Management

```bash
./bin/go-proxy-server adduser -username alice -password secret123
./bin/go-proxy-server deluser -username alice
./bin/go-proxy-server listuser
./bin/go-proxy-server addip -ip 203.0.113.10
./bin/go-proxy-server delip -ip 203.0.113.10
./bin/go-proxy-server listip
```

## Tunnel Commands

```bash
# centralized tunnel server
./bin/go-proxy-server tunnel-server -listen :7000 -token demo-secret -cert server.crt -key server.key

# long-lived client
./bin/go-proxy-server tunnel-client -server example.com:7000 -token demo-secret -client node-a -ca ca.pem

# route management
./bin/go-proxy-server tunnel-list-clients
./bin/go-proxy-server tunnel-list-routes
./bin/go-proxy-server tunnel-save-route -client node-a -name redis -target 127.0.0.1:6379 -public-port 16379 -enabled
./bin/go-proxy-server tunnel-del-route -client node-a -name redis
```

Certificate generation steps are documented in `tunnel.md`.

## Logging

- CLI mode logs to stdout/stderr by default.
- Windows tray mode logs to `app.log` in the app data directory.
- Core state is stored in `data.db`.

## Next Steps

- [Tunnel management](tunnel.md)
- [Chinese docs index](README.zh-CN.md)
