# Configuration-Driven Runtime and Service Management Design

## Goal

Add a configuration-driven runtime for Linux and macOS deployment so one foreground process can start multiple built-in services from a TOML file, while OS-native service management installs that runtime through `systemd` or `launchd`.

## Scope

This design covers:

- a new `run` command that starts services from TOML
- CLI override rules where explicit flags win over TOML
- one-process multi-service runtime for `web`, `socks`, `http`, `tunnel-server`, and `tunnel-client`
- Linux system-level service installation through `systemd`
- macOS user-level service installation through `launchd`

This design does not cover:

- self-daemonizing with `--background` or `--daemon`
- web UI based service management
- Windows service management
- multiple independent OS services from one install command
- multiple instances of the same built-in service type inside one process

## Problem

The current binary has two separate operating models:

- direct CLI commands such as `web`, `socks`, `http`, `both`, `tunnel-server`, and `tunnel-client`
- manual OS integration for Linux and macOS, which is currently incomplete

That makes long-running deployment awkward. Service definitions need to know exact startup arguments, but operators often want one declarative file that describes what should run. At the same time, the existing direct CLI commands are useful and should not be broken.

## Recommended Approach

Keep the current CLI commands as explicit one-shot or direct-run entrypoints, and add one new configuration-driven entrypoint:

```bash
go-proxy-server run [-config /path/config.toml] [overrides...]
```

The `run` command becomes the only runtime that OS service installation uses. `service install` will register `go-proxy-server run ...` with the platform service manager instead of encoding low-level proxy arguments directly into the service definition.

This keeps direct CLI usage intact while giving Linux and macOS a stable, declarative deployment path.

## User Experience

The binary will support three operating styles:

### Existing direct commands

Examples:

```bash
go-proxy-server web -port 8080
go-proxy-server socks -port 1080
go-proxy-server tunnel-server -engine classic -listen :7000 -token secret -allow-insecure
```

These continue to work exactly as they do today.

### Configuration-driven runtime

Examples:

```bash
go-proxy-server run
go-proxy-server run -config /etc/go-proxy-server/config.toml
go-proxy-server run --web-port 8081 --socks-port 1081
```

Behavior rules:

- `run` enters configuration-driven mode
- if `-config` is omitted, the program reads the default config path
- if the selected TOML file does not exist, `run` fails with a clear error
- `run` does not silently fall back to default `web` mode
- command-line overrides win over TOML values

### Service management

Examples:

```bash
sudo go-proxy-server service install
sudo go-proxy-server service install -config /etc/go-proxy-server/config.toml
go-proxy-server service status
```

Behavior rules:

- Linux installs a system-level `systemd` service
- macOS installs a user-level `launchd` LaunchAgent
- the service definition always starts `go-proxy-server run`
- the service definition may include `-config <path>` if the operator selected a non-default config path
- the service manager does not encode per-service proxy or tunnel flags directly

## Configuration File Model

The runtime configuration file will be TOML.

Default path rules:

- Linux: `~/.config/go-proxy-server/config.toml`, respecting `XDG_CONFIG_HOME` when set
- macOS: `~/Library/Application Support/go-proxy-server/config.toml`
- Windows: a matching per-user config path may be implemented for completeness, but non-Windows service installation is the focus of this design

Recommended TOML structure:

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

Rules:

- each block describes at most one built-in service instance
- `enabled = true` means the runtime must start that service
- `both` is not represented in TOML; enabling both `socks` and `http` replaces it
- if every component is disabled, `run` fails with a clear error
- validation rules for tunnel TLS and credential requirements remain strict

## Override Model

The runtime merges configuration sources in this order:

1. code defaults
2. TOML file values
3. explicit CLI overrides

CLI overrides should stay narrow and practical instead of mirroring every legacy subcommand flag.

Recommended first-pass overrides:

- `-config <path>`
- `--web-port`
- `--socks-port`
- `--socks-bind-listen`
- `--http-port`
- `--http-bind-listen`

Tunnel-specific overrides can remain TOML-only in the first pass. This keeps the new `run` surface small while still allowing common deployment adjustments from the command line.

## Runtime Model

The `run` command starts one process that can host multiple components at once.

The runtime should:

- initialize the existing database, logger, cleanup hooks, and auth state through the current bootstrap path
- load and validate runtime config before starting components
- start only the enabled components
- reuse current production implementations instead of creating parallel copies
- shut down cleanly on SIGINT or SIGTERM

Reuse strategy:

- `web` should continue to use the existing web manager startup path
- `socks` and `http` should reuse the current proxy listener logic
- tunnel server and tunnel client should reuse the current tunnel startup logic
- shared validation helpers should be extracted where needed so direct commands and TOML runtime use the same rules

## Service Management Model

Add an internal package for service management:

- `internal/service`

The service layer should manage OS-specific installation but treat runtime execution as opaque. It only needs:

- service name
- executable path
- working directory
- runtime arguments, which will be `run` plus optional `-config`

Suggested interface:

```go
type Manager interface {
    Install(ServiceSpec) error
    Uninstall(name string) error
    Start(name string) error
    Stop(name string) error
    Status(name string) (Status, error)
}

type ServiceSpec struct {
    Name             string
    Description      string
    ExecPath         string
    Args             []string
    WorkingDirectory string
}
```

Platform behavior:

### Linux

- write a unit file under a standard systemd unit location
- run `systemctl daemon-reload`
- run `systemctl enable --now <name>`
- fail clearly when privileges are insufficient

### macOS

- write a LaunchAgent plist in the current user's LaunchAgents directory
- bootstrap it with `launchctl`
- start it after loading

## CLI Integration

Top-level commands become:

- existing commands such as `web`, `socks`, `http`, `both`, and tunnel commands
- new `run`
- new `service`

The `service` family becomes:

```bash
go-proxy-server service install [-config /path/config.toml] [--name <service-name>]
go-proxy-server service uninstall [--name <service-name>]
go-proxy-server service start [--name <service-name>]
go-proxy-server service stop [--name <service-name>]
go-proxy-server service status [--name <service-name>]
```

Rules:

- `service install` no longer accepts mode-specific payloads after `--`
- service definitions always point to `run`
- if `-config` is omitted during install, the installed service uses the default config path resolution of `run`

## Error Handling

The implementation should surface clear operator-facing errors for:

- missing runtime config file
- invalid TOML
- invalid merged config after applying CLI overrides
- no enabled services in runtime config
- unsupported platform for `service`
- insufficient Linux privilege to install/uninstall system services
- missing installed service on `start`, `stop`, `status`, or `uninstall`
- failed `systemctl` or `launchctl` subprocess execution

## Testing Strategy

Tests should remain deterministic and avoid mutating the host service registry.

Required coverage:

- TOML decoding and normalization
- config path discovery on Linux and macOS path rules
- CLI override merging
- `run` argument parsing and validation
- runtime startup planning for enabled services
- Linux systemd unit rendering
- macOS plist rendering
- unsupported platform behavior
- service install argument generation for `run` and optional `-config`

## Documentation Impact

After implementation, update:

- `README.md`
- `README.zh-CN.md`
- `docs/getting-started.md`
- `docs/getting-started.zh-CN.md`

Documentation should explain:

- direct CLI commands still exist
- `run` is the new configuration-driven multi-service runtime
- Linux and macOS service installation always targets `run`
- command-line overrides take precedence over TOML
- the default config file location and a full example TOML

## Recommended First Implementation Scope

The first implementation should include:

- `run`
- TOML loading and validation
- common CLI overrides for `web`, `socks`, and `http`
- `service install`
- `service uninstall`
- `service start`
- `service stop`
- `service status`

`restart` can be added later as a thin follow-up around stop/start behavior.
