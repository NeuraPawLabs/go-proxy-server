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
- previously saved tunnel server/client configs with `AutoStart` may also be restored automatically

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

## Configuration-Driven Runtime

Use `go-proxy-server run` to start one managed process from TOML.

```bash
./bin/go-proxy-server run
./bin/go-proxy-server run -config /etc/go-proxy-server/config.toml
./bin/go-proxy-server run -web-port 8081 -socks-port 1081
```

If `-config` is omitted, `run` uses the platform default runtime config path:

- Linux: `$XDG_CONFIG_HOME/go-proxy-server/config.toml` or `~/.config/go-proxy-server/config.toml`
- macOS: `~/Library/Application Support/go-proxy-server/config.toml`
- Windows: `%APPDATA%\go-proxy-server\config.toml` or `~/go-proxy-server/config.toml`

CLI flags override TOML values, so you can keep a shared file and still apply one-off overrides at launch.

Before using `run`, save the sample below to that default path or pass its path with `-config`.

Example `config.toml`:

```toml
[web]
enabled = true
port = 8080

[socks]
enabled = true
port = 1080
bind_listen = false

[http]
enabled = false
port = 8081
bind_listen = false

[[exit_bindings]]
name = "aliyun-eip-a"
ingress_local_ip = "172.16.0.10"
outbound_local_ip = "172.16.0.10"

[[exit_bindings]]
name = "aliyun-eip-b"
ingress_local_ip = "172.16.0.11"
outbound_local_ip = "172.16.0.11"

[tunnel_server]
enabled = false
engine = "classic"
listen = ":7000"
public_bind = "0.0.0.0"
token = ""
cert = ""
key = ""
allow_insecure = false
auto_port_start = 0
auto_port_end = 0

[tunnel_client]
enabled = false
engine = "classic"
server = ""
token = ""
client = ""
ca = ""
server_name = ""
insecure_skip_verify = false
allow_insecure = false
```

When `bind_listen = true`, the proxy uses the local IP that accepted the ingress connection as the outbound source address. If `[[exit_bindings]]` is configured, `ingress_local_ip` is mapped to `outbound_local_ip` first. On Alibaba Cloud EIP NAT deployments, use the ECS private IPs assigned to the host, not the public EIPs; multiple EIP exits require distinct secondary private IPs and source-based routing.

- Tunnel TLS rules in TOML:
  `[tunnel_server]` must either provide `cert` and `key`, or set `allow_insecure = true`
  `[tunnel_client]` must use one of `ca`, `insecure_skip_verify = true`, or `allow_insecure = true`
  `allow_insecure = true` cannot be combined with `cert`/`key`, `ca`, `server_name`, or `insecure_skip_verify`
  `insecure_skip_verify = true` keeps TLS enabled but skips certificate verification, so it does not require `ca`
  server-side `allow_insecure = true` still starts even if managed certificate files are present on disk

This pattern keeps the direct `socks`, `http`, and `both` commands available while making one-process startup config-driven.

## Service Workflow

`service install` always installs `go-proxy-server run` as the managed service command.

Linux installs a system-level `systemd` service:

```bash
sudo ./bin/go-proxy-server service install
sudo ./bin/go-proxy-server service install -config /etc/go-proxy-server/config.toml
sudo ./bin/go-proxy-server service status
```

- If Linux `service install` omits `-config`, the installed unit resolves the runtime config in this order:
  preserved `$XDG_CONFIG_HOME/go-proxy-server/config.toml`
  otherwise `~SUDO_USER/.config/go-proxy-server/config.toml`
- For predictable system deployment, prefer `-config /etc/go-proxy-server/config.toml`.

macOS installs a user-level `launchd` LaunchAgent:

```bash
./bin/go-proxy-server service install
./bin/go-proxy-server service status
```

- On macOS, omitting `-config` makes the LaunchAgent use the current user's default config path at runtime.

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
