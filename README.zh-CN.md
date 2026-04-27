# Go Proxy Server

[English](README.md) | [文档索引](docs/README.zh-CN.md) | [更新日志](CHANGELOG.zh-CN.md)

Go Proxy Server 是一个使用 Go 编写的自托管服务，单个二进制同时覆盖本地代理、仅监听本机的 Web 管理后台，以及集中式内网穿透管理。

## 它能做什么

- **代理服务：** 支持 SOCKS5、HTTP，或两者同时运行。
- **Web 管理：** 通过本地 Web 后台管理用户、白名单、日志、代理配置和隧道路由。
- **隧道控制面：** 支持一个 `tunnel-server` 管理多个常驻 `tunnel-client`。
- **安全能力：** 提供用户名密码认证、IP 白名单、SSRF / DNS Rebinding 防护、审计日志与事件日志。
- **跨平台运行：** Linux / macOS 默认进入 Web 模式，Windows 优先托盘模式。

## 功能总览

### 代理与访问控制

- 支持 SOCKS5 代理
- 支持 HTTP/HTTPS 代理
- 用户名/密码认证，密码使用带随机盐的 SHA-256 存储
- 支持 IP 白名单
- 支持多出口 IP 场景下的 `-bind-listen`
- 认证、超时与限流配置支持运行时热更新

### Web 管理与运维

- Web 管理后台仅监听本机 `localhost`
- 支持代理启停与持久化配置管理
- Web 后台内置审计日志与事件日志中心
- 使用 SQLite 持久化，驱动为纯 Go 实现
- 在未嵌入完整前端资源的测试/构建环境下，会回退到轻量提示页

### 内网穿透管理

- 集中式模型：一个 `tunnel-server`，多个 `tunnel-client`
- 路由可通过 Web 后台或 CLI 管理
- `classic` 引擎支持 TCP
- `quic` 引擎支持 TCP 与 UDP
- 支持在指定端口范围内自动分配公网端口

### 配置驱动运行

- `go-proxy-server run` 会从 TOML 配置启动一个受管进程。
- 如果省略 `-config`，`run` 会读取当前平台的默认运行配置路径。
  Linux：`$XDG_CONFIG_HOME/go-proxy-server/config.toml` 或 `~/.config/go-proxy-server/config.toml`
  macOS：`~/Library/Application Support/go-proxy-server/config.toml`
  Windows：`%APPDATA%\go-proxy-server\config.toml` 或 `~/go-proxy-server/config.toml`
- 命令行参数会覆盖 TOML 中的值，因此临时覆盖仍然优先。
- `go-proxy-server service install` 会把 `go-proxy-server run` 作为系统服务入口安装。

### 平台行为

- Linux / macOS：无参数启动时进入本地 Web 管理模式
- Windows：无参数启动时优先尝试系统托盘模式
- 无参数启动会恢复已保存且启用了 `AutoStart` 的代理服务
- 无参数启动也可能恢复已保存且启用了 `AutoStart` 的 `tunnel-server` 与 `tunnel-client` 配置

## 运行模式总览

| 模式 | 命令 | 作用 |
| --- | --- | --- |
| 默认模式 | `./bin/go-proxy-server` | Linux/macOS 启动 Web 后台，Windows 启动托盘或回退到 Web 模式 |
| Web 管理 | `./bin/go-proxy-server web` | 启动仅监听 `localhost` 的 Web 后台 |
| SOCKS5 | `./bin/go-proxy-server socks` | 前台启动 SOCKS5 代理 |
| HTTP | `./bin/go-proxy-server http` | 前台启动 HTTP/HTTPS 代理 |
| 双代理 | `./bin/go-proxy-server both` | 同时启动 SOCKS5 和 HTTP/HTTPS |
| 隧道服务端 | `./bin/go-proxy-server tunnel-server ...` | 启动集中式内网穿透服务端 |
| 隧道客户端 | `./bin/go-proxy-server tunnel-client ...` | 启动受控的内网穿透客户端 |

## 快速开始

### 编译

```bash
make build
```

- `make build` 会先构建前端，再带 `frontend_embed` 标签编译 Go 二进制。
- `internal/web/dist` 是构建产物，不会提交到仓库。
- 干净仓库直接执行 `go test ./...` 也能工作，此时会使用轻量提示页替代完整前端资源。

### 启动 Web 管理后台

```bash
./bin/go-proxy-server web
```

- Web 后台只监听 `localhost`。
- 未指定端口时会自动选择一个可用随机端口，并在日志中输出实际访问地址。

### 直接启动代理服务

```bash
# 只启动 SOCKS5
./bin/go-proxy-server socks

# 只启动 HTTP/HTTPS
./bin/go-proxy-server http

# 同时启动两种代理
./bin/go-proxy-server both
```

```bash
# 显式指定端口
./bin/go-proxy-server socks -port 1080
./bin/go-proxy-server http -port 8080
./bin/go-proxy-server both -socks-port 1080 -http-port 8080
```

- 这些 CLI 模式都会以前台运行，停止时直接 `Ctrl+C`。
- 这些模式只使用当前命令行参数，不会恢复 Web 后台保存的代理端口配置。
- 但它们仍会加载 SQLite 中的用户和白名单状态。

### 配置驱动运行

```bash
./bin/go-proxy-server run
./bin/go-proxy-server run -config /etc/go-proxy-server/config.toml
./bin/go-proxy-server run -web-port 8081 -socks-port 1081
```

使用 `run` 前，请先把下面的示例保存到当前平台默认配置路径，或者通过 `-config` 明确指定配置文件路径。

示例 `config.toml`：

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

- `-config` 省略时，`run` 会读取平台默认的运行配置路径。
  Linux：`$XDG_CONFIG_HOME/go-proxy-server/config.toml` 或 `~/.config/go-proxy-server/config.toml`
  macOS：`~/Library/Application Support/go-proxy-server/config.toml`
  Windows：`%APPDATA%\go-proxy-server\config.toml` 或 `~/go-proxy-server/config.toml`
- `run` 先读取 TOML，再把命令行覆盖项应用到配置之上。
- 即使只想通过命令行临时覆盖端口，`run` 也仍然要求对应的 TOML 配置文件已经存在。
- TOML 中的 tunnel TLS 规则：
  `[tunnel_server]` 要么提供 `cert` 和 `key`，要么设置 `allow_insecure = true`
  `[tunnel_client]` 必须在 `ca`、`insecure_skip_verify = true`、`allow_insecure = true` 三者中选择一种
  `allow_insecure = true` 不能和 `cert`/`key`、`ca`、`server_name`、`insecure_skip_verify` 同时使用
  `insecure_skip_verify = true` 仍然走 TLS，只是跳过证书校验，因此不要求 `ca`
  服务端配置 `allow_insecure = true` 时，即使磁盘上已经存在托管证书文件，也不需要先删除
- 这样可以在不改变 `socks`、`http` 或 `both` 直接命令的前提下，使用单进程方式启动。

### Linux 和 macOS 服务安装

`service install` 始终会安装 `go-proxy-server run` 作为受管服务命令。

Linux 使用系统级 `systemd` 服务：

```bash
sudo ./bin/go-proxy-server service install
sudo ./bin/go-proxy-server service install -config /etc/go-proxy-server/config.toml
sudo ./bin/go-proxy-server service status
```

- Linux 下如果 `service install` 省略 `-config`，安装出的 unit 会按以下顺序解析运行配置：
  优先使用保留下来的 `$XDG_CONFIG_HOME/go-proxy-server/config.toml`
  否则回退到 `SUDO_USER` 对应用户的 `~/.config/go-proxy-server/config.toml`
- 如果要做稳定的系统部署，建议显式传 `-config /etc/go-proxy-server/config.toml`。

macOS 使用当前用户级别的 `launchd` LaunchAgent：

```bash
./bin/go-proxy-server service install
./bin/go-proxy-server service status
```

- macOS 下省略 `-config` 时，LaunchAgent 会在运行时读取当前用户的默认配置路径。

### 添加用户并启动 SOCKS5

```bash
./bin/go-proxy-server adduser -username alice -password secret123
./bin/go-proxy-server socks -port 1080
```

## 常用命令

### 用户与白名单管理

```bash
./bin/go-proxy-server adduser -username alice -password secret123
./bin/go-proxy-server deluser -username alice
./bin/go-proxy-server listuser

./bin/go-proxy-server addip -ip 192.168.1.100
./bin/go-proxy-server delip -ip 192.168.1.100
./bin/go-proxy-server listip
```

### 代理命令

```bash
./bin/go-proxy-server socks -port 1080 [-bind-listen]
./bin/go-proxy-server http -port 8080 [-bind-listen]
./bin/go-proxy-server both -socks-port 1080 -http-port 8080 [-bind-listen]
```

### 帮助与版本

```bash
./bin/go-proxy-server --help
./bin/go-proxy-server --version
```

## 内网穿透概览

Go Proxy Server 提供两种集中式隧道引擎：

- **`classic`：** 仅支持 TCP，部署模型更直接
- **`quic`：** 支持 TCP 与 UDP，适合混合协议场景

Classic 示例：

```bash
# 服务端
./bin/go-proxy-server tunnel-server \
  -engine classic \
  -listen :7000 \
  -auto-port-start 30000 \
  -auto-port-end 30999 \
  -token demo-secret \
  -cert server.crt \
  -key server.key

# 客户端
./bin/go-proxy-server tunnel-client \
  -engine classic \
  -server your.server.example:7000 \
  -token demo-secret \
  -client node-shanghai-01 \
  -ca ca.pem
```

QUIC 示例：

```bash
# 服务端
./bin/go-proxy-server tunnel-server \
  -engine quic \
  -listen :7443 \
  -auto-port-start 30000 \
  -auto-port-end 30999 \
  -token demo-secret \
  -cert server.crt \
  -key server.key

# 客户端
./bin/go-proxy-server tunnel-client \
  -engine quic \
  -server your.server.example:7443 \
  -token demo-secret \
  -client node-shanghai-01 \
  -ca ca.pem
```

补充说明：

- `-public-port 0` 表示自动分配公网端口。
- `udp` 路由要求服务端和客户端都运行在 `quic` 引擎下。
- Web 后台同时支持服务端工作台和客户端工作台。
- CLI 仍保留不安全连接参数用于测试或迁移，但默认推荐使用校验证书后的 TLS。
- 更完整的证书、路由与操作示例见 [docs/tunnel.zh-CN.md](docs/tunnel.zh-CN.md)。

## 配置与运行说明

### `.env` 支持

程序会在初始化配置前尝试加载本地 `.env` 文件。

- 查找顺序：当前工作目录，其次是可执行文件所在目录
- 如果系统环境变量已存在，则优先使用系统环境变量
- 建议仅用于本地开发

示例：

```bash
cat > .env <<'EOF'
GPS_ADMIN_BOOTSTRAP_TOKEN=change-me
EOF
```

### 数据与日志

- 应用数据保存在平台对应的应用数据目录
- 主 SQLite 数据库文件为 `data.db`
- CLI 与 Web 模式都会输出日志到 stdout/stderr，并写入 `app.log`
- Windows 托盘模式也会把运行日志写入应用数据目录下的 `app.log`

### Web 后台日志中心

Web 管理后台内置基于 SQLite 的日志中心：

- **审计日志：** 记录登录登出、用户维护、白名单修改、代理配置变更、隧道管理等管理面操作
- **事件日志：** 记录认证失败、验证码失败、SSRF / DNS 防护命中、隧道失败及其他运行时安全/运维事件

## 文档导航

如果你需要更详细的说明，建议从这里继续：

- [文档索引](docs/README.zh-CN.md)
- [快速开始](docs/getting-started.zh-CN.md)
- [内网穿透说明](docs/tunnel.zh-CN.md)
- [Windows 使用与构建](docs/windows.zh-CN.md)
- [English documentation index](docs/README.md)

## 贡献与安全

- [贡献指南](CONTRIBUTING.zh-CN.md)
- [安全策略](SECURITY.zh-CN.md)
- [架构说明](CLAUDE.md)
