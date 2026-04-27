# Windows Usage and Build

[中文版本](windows.zh-CN.md)

This page covers the essential Windows workflow for Go Proxy Server.

## Daily Usage

For normal use, start the Windows build directly:

```powershell
bin\go-proxy-server.exe
```

Notes:
- it starts in tray mode by default
- the Web admin UI listens on `localhost` only
- the management port is chosen automatically unless specified manually
- you can open the admin UI from the tray menu

## Logs and Data Directory

Default application data directory:

```powershell
%APPDATA%\go-proxy-server\
```

Common files:
- `data.db`: SQLite database
- `app.log`: log file used by tray / GUI mode

Notes:
- the actual Web admin URL is shown in startup logs or tray hints

## Common Issues

### Tray icon is not visible

Check the hidden icons area in the taskbar notification tray.

### Need a fixed management port

```powershell
go-proxy-server.exe web -port 8888
```

### Application exits immediately

Check `app.log` in the app data directory first:

```powershell
Get-Content "$env:APPDATA\go-proxy-server\app.log" -Tail 200
```

## Build

Recommended commands:

```bash
make build-windows
```

Notes:
- `build-windows` creates the tray / GUI build
- `build-windows-gui` is kept as an alias to `build-windows`
- Windows resource metadata is embedded through the repository build scripts

## Antivirus and Signing

Go-built networking tools may trigger false positives on some Windows security products. Common mitigations include:
- embedding version information
- code signing
- submitting false-positive reports
- publishing SHA256 checksums for users to verify downloads
