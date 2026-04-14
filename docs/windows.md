# Windows Usage and Build

[中文版本](windows.zh-CN.md)

This page covers the essential Windows workflow for Go Proxy Server.

## Daily Usage

For normal use, start the GUI / tray build:

```powershell
bin\go-proxy-server-gui.exe
```

Notes:
- it starts in tray mode by default
- the Web admin UI listens on `localhost` only
- the management port is chosen automatically unless specified manually
- you can open the admin UI from the tray menu

For debugging, use the console build:

```powershell
bin\go-proxy-server.exe
```

## Logs and Data Directory

Default application data directory:

```powershell
%APPDATA%\go-proxy-server\
```

Common files:
- `data.db`: SQLite database
- `app.log`: log file used by tray / GUI mode

Notes:
- console mode logs to the terminal by default
- the actual Web admin URL is shown in startup logs or tray hints

## Common Issues

### Tray icon is not visible

Check the hidden icons area in the taskbar notification tray.

### Need a fixed management port

```powershell
go-proxy-server.exe web -port 8888
```

### Application exits immediately

Run the console build first and inspect the error output:

```powershell
go-proxy-server.exe
```

## Build

Recommended commands:

```bash
make build-windows
make build-windows-gui
```

Notes:
- `build-windows` creates the console build
- `build-windows-gui` creates the tray / GUI build
- Windows resource metadata is embedded through the repository build scripts

## Antivirus and Signing

Go-built networking tools may trigger false positives on some Windows security products. Common mitigations include:
- embedding version information
- code signing
- submitting false-positive reports
- publishing SHA256 checksums for users to verify downloads
