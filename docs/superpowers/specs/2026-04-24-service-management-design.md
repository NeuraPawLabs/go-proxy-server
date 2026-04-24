# Linux/macOS Service Management Design

## Goal

Add a cross-platform `service` management command for Linux and macOS so the binary can be installed and managed through the operating system's native service manager instead of self-daemonizing.

## Scope

This design covers:

- CLI command design for `service install`, `uninstall`, `start`, `stop`, `restart`, and `status`
- Linux system-level service integration through `systemd`
- macOS user-level service integration through `launchd` LaunchAgent
- Service definition generation from the current binary path and selected startup mode

This design does not cover:

- `--background` or `--daemon` style self-backgrounding
- Web UI integration for service management
- Windows service management
- Multiple installed services per platform beyond an optional explicit `--name`

## Problem

The current binary supports foreground operation and Windows tray/autostart behavior, but Linux and macOS do not have a native CLI path to install the program as an OS-managed service. Existing non-Windows autostart support is explicitly unimplemented, and background execution is currently a manual responsibility of the operator.

For Linux and macOS, the recommended operational model is not to fork into the background. The program should continue to run as a normal foreground process, and `systemd` or `launchd` should manage lifecycle, startup, restart, and status reporting.

## User Experience

The CLI will gain a top-level command family:

```bash
go-proxy-server service install [--mode default|web|socks|http|both] [--name <service-name>] [-- <extra args>]
go-proxy-server service uninstall [--name <service-name>]
go-proxy-server service start [--name <service-name>]
go-proxy-server service stop [--name <service-name>]
go-proxy-server service restart [--name <service-name>]
go-proxy-server service status [--name <service-name>]
```

Behavior rules:

- `service install` registers the service with the OS service manager and optionally enables/starts it
- Service execution always reuses the current foreground binary entrypoint
- No command will attempt to fork or detach the current process
- The service will run the selected command mode through `ExecStart` or the equivalent launchd plist program arguments

Examples:

```bash
go-proxy-server service install --mode default
go-proxy-server service install --mode web
go-proxy-server service install --mode socks -- socks -port 1080
go-proxy-server service status
```

## Service Modes

The install command needs two inputs:

1. A logical mode
2. The actual argument list that the service should run

Mode rules:

- `default` means run `go-proxy-server`
- `web` means run `go-proxy-server web`
- `socks`, `http`, and `both` are service-oriented shortcuts, but the final executed arguments should come from the explicit post-`--` argument list

To avoid ambiguous combinations, the implementation should normalize the final service command into:

- `ExecPath`
- `Args`

The implementation should reject invalid combinations such as:

- `service install --mode socks` without an explicit command payload after `--`
- `service install --mode web -- socks -port 1080`

The simplest consistent rule is:

- `default` and `web` may infer arguments
- all other modes require `-- <extra args>`

## Platform Model

### Linux

Linux will use a system-level `systemd` service.

Characteristics:

- install target is system-wide
- requires sufficient privilege to write unit files and run `systemctl`
- service file should be written to a standard systemd unit location
- service should be enabled and started through `systemctl enable --now`

Expected operational flow:

1. Build a service spec from the current executable path, working directory, and final command arguments
2. Write a unit file
3. Run `systemctl daemon-reload`
4. Run `systemctl enable --now <name>`

If privileges are insufficient, fail fast with a clear error that the command must be re-run with `sudo`.

### macOS

macOS will use a user-level `launchd` LaunchAgent.

Characteristics:

- install target is the current user only
- no root requirement
- plist should live in the standard user LaunchAgents directory
- service should be loaded through `launchctl bootstrap` and started through `launchctl kickstart` or equivalent

Expected operational flow:

1. Build a service spec from the current executable path, working directory, and final command arguments
2. Write a LaunchAgent plist
3. Load/bootstrap the plist into the current user domain
4. Start the service if needed

## Internal Design

Add a dedicated internal package for service management, for example:

- `internal/service`

The package should expose a small interface:

```go
type Manager interface {
    Install(ServiceSpec) error
    Uninstall(name string) error
    Start(name string) error
    Stop(name string) error
    Restart(name string) error
    Status(name string) (Status, error)
}
```

Core shared structures:

```go
type ServiceSpec struct {
    Name             string
    Description      string
    ExecPath         string
    Args             []string
    WorkingDirectory string
}

type Status struct {
    Name    string
    State   string
    Enabled bool
    Running bool
    Detail  string
}
```

Platform-specific implementations should live behind build tags, similar to existing platform split packages in this repository.

Suggested file layout:

- `internal/service/service.go`
- `internal/service/service_linux.go`
- `internal/service/service_darwin.go`
- `internal/service/service_other.go`

## CLI Integration

The CLI routing layer will add a new top-level command:

- `service`

The subcommand parser should live in `cmd/server/app.go` alongside existing command handlers.

The implementation should:

- parse service subcommands with `flag.FlagSet`
- normalize the requested execution payload into a `ServiceSpec`
- delegate all platform behavior to `internal/service`
- print human-readable success and status output to stdout
- return regular Go errors for failures so the existing stderr/error handling remains consistent

## Error Handling

The implementation should surface practical operator-facing failures:

- unsupported platform
- missing privilege for Linux system service install
- executable path resolution failure
- invalid mode/argument combinations
- existing installed service conflicts
- missing service on start/stop/status/uninstall
- failed `systemctl` or `launchctl` subprocess execution

The CLI should not hide platform command stderr if a service manager call fails.

## Testing Strategy

Tests should focus on deterministic logic and avoid requiring real system service installation.

Required coverage:

- CLI argument parsing for each `service` subcommand
- mode normalization into the final executable argument list
- validation of invalid `install` combinations
- Linux unit content generation
- macOS plist content generation
- unsupported-platform behavior

The platform managers should isolate shell execution behind testable helpers so unit tests can validate generated commands without mutating the local machine service registry.

## Documentation Impact

After implementation, the following docs should be updated:

- `README.md`
- `README.zh-CN.md`
- `docs/getting-started.md`
- `docs/getting-started.zh-CN.md`

Documentation should explain:

- foreground mode remains the only direct execution model
- Linux and macOS background/autostart are achieved through `service` subcommands
- Linux uses system services and likely needs `sudo`
- macOS uses per-user LaunchAgent installation

## Recommended First Implementation Scope

The first implementation should include:

- `service install`
- `service uninstall`
- `service start`
- `service stop`
- `service status`

`restart` can be added in the same pass if trivial, but it is not required to make the feature useful.
