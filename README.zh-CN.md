# Go Proxy Server

[English](README.md) | [文档索引](docs/README.zh-CN.md) | [更新日志](CHANGELOG.zh-CN.md)

Go Proxy Server 是一个使用 Go 编写的轻量级自托管代理与内网穿透服务，支持 SOCKS5、HTTP/HTTPS、本地 Web 管理界面、Windows 托盘，以及“一个服务端 + 多个客户端”的集中式内网穿透管理模型。

## 亮点特性

- 支持 SOCKS5 与 HTTP/HTTPS 代理
- 用户名/密码认证，密码使用带随机盐的 SHA-256 存储
- 支持 IP 白名单
- SQLite 持久化，使用纯 Go 驱动
- Web 管理后台仅监听本机 `localhost`
- 内置 Web 日志中心，支持审计日志与事件日志
- 支持 Windows 托盘模式与命令行模式
- 集中式内网穿透管理
  - 一个 `tunnel-server`
  - 多个常驻 `tunnel-client`
  - 通过 Web 后台或 CLI 管理透传路由
  - `classic` 引擎支持 TCP
  - `quic` 引擎支持 TCP 与 UDP
- 用户、白名单、超时和限流配置支持运行时热更新

## 快速开始

### 编译

```bash
make build
```

- `internal/web/dist` 属于构建产物，不会提交到 Git。
- `make build` 与 GitHub Actions 会先编译前端，再带 `frontend_embed` 标签编译 Go 二进制。
- 干净仓库里直接执行 `go test ./...` 也能通过，此时会使用一个轻量提示页替代完整 Web UI 资源。

### 启动 Web 管理界面

```bash
./bin/go-proxy-server web
```

Web 管理界面只监听 `localhost`。未指定端口时会自动分配一个可用随机端口，并在启动日志中输出实际访问地址。

### 使用 `.env` 配置环境变量

服务端现在支持在启动早期自动加载本地 `.env` 文件。

- 查找顺序：当前工作目录下的 `.env`，然后是可执行文件同目录下的 `.env`
- 如果系统环境变量已存在，则优先使用系统环境变量，不会被 `.env` 覆盖
- 本地开发可直接复制 `.env.example` 作为模板

示例：

```bash
cp .env.example .env
```

```env
GEETEST_ID=your_geetest_id
GEETEST_KEY=your_geetest_key
```

建议仅在本地开发中使用 `.env`；生产环境优先使用真实环境变量或部署系统的 Secret 注入方式。

行为说明：

- 如果 `GEETEST_ID` 和 `GEETEST_KEY` 都未配置，则不会启用验证码，管理后台使用密码直接登录
- 如果两者都已配置，则管理后台登录会启用验证码
- 如果只配置了其中一个，则会被视为配置错误，登录会被拒绝

### 审计日志与事件日志

Web 管理后台现在提供独立的 `日志中心` 页面，日志会持久化到 SQLite。

- `审计日志` 用于记录管理面上的关键操作是谁、在什么时候、对什么对象做了什么
  - 初始化管理密码、后台登录/退出
  - 用户与白名单维护
  - 代理启停与代理配置更新
  - 系统配置修改、退出应用请求
  - 内网穿透服务端、证书、路由等后台操作
- `事件日志` 用于记录运行时与安全侧的重要事件
  - 隧道客户端连接/断开、路由暴露成功或失败
  - 登录失败、验证码校验失败等认证异常
  - 限流拦截、SSRF / DNS Rebinding 防护命中
  - 隧道数据面失败及其他运维告警

两类日志都写入同一个应用 SQLite 数据库（应用数据目录下的 `data.db`），可在 Web UI 中按时间范围、分类、级别、状态和关键字进行过滤查看。

### 无参数直接启动

```bash
./bin/go-proxy-server
```

- Linux / macOS：直接启动本地 Web 管理后台
- Windows：优先尝试系统托盘模式，失败后回退到本地 Web 管理后台
- 会恢复之前保存为 `AutoStart` 的代理服务
- 不会自动启动 `tunnel-server` 或 `tunnel-client`

### 添加用户并启动 SOCKS5

```bash
./bin/go-proxy-server adduser -username alice -password secret123
./bin/go-proxy-server socks -port 1080
```

### 启动集中式内网穿透

Classic 引擎示例：

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

# 通过命令行下发一条 TCP 路由
./bin/go-proxy-server tunnel-save-route \
  -client node-shanghai-01 \
  -name mysql-prod \
  -protocol tcp \
  -target 127.0.0.1:3306 \
  -public-port 33060 \
  -enabled
```

QUIC 引擎 + UDP 示例：

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

# 通过命令行下发一条 UDP 路由
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

证书生成示例见 `docs/tunnel.zh-CN.md`。

说明：

- 路由的 `-public-port 0` 表示自动分配端口。
- 如果配置了 `-auto-port-start` / `-auto-port-end`，自动分配会只在该端口范围内挑选可用端口。
- 这很适合与云平台安全组预留的固定端口段配合使用。
- `udp` 路由要求服务端和客户端都运行在 `quic` 引擎下。
- Web 后台同时提供服务端模式和客户端模式。
- Web 后台的客户端模式只支持校验证书后的 TLS 连接：可以上传 CA，也可以在本机同时启用服务端模式时自动复用本机托管 CA。
- CLI 仍保留 `-allow-insecure` 与 `-insecure-skip-verify` 作为测试或兼容参数，但不建议在生产环境使用。

## 推荐文档入口

- [文档索引](docs/README.zh-CN.md)
- [快速开始](docs/getting-started.zh-CN.md)
- [内网穿透说明](docs/tunnel.zh-CN.md)
- [Windows 使用与构建](docs/windows.zh-CN.md)
- [英文文档入口](docs/README.md)

## 贡献与安全

- [贡献指南](CONTRIBUTING.zh-CN.md)
- [安全策略](SECURITY.zh-CN.md)
- [架构说明](CLAUDE.md)

## 平台说明

- Linux / macOS 在无参数启动时默认进入 Web 管理模式。
- Windows 在无参数启动时优先进入系统托盘模式。
- 无参数启动时，可能会自动恢复已保存且启用了 `AutoStart` 的代理服务。
- 无参数启动时，不会自动启动内网穿透服务端或客户端进程。
- CLI 模式默认输出日志到标准输出/标准错误。
- Windows 托盘模式默认写入应用数据目录中的 `app.log`。
