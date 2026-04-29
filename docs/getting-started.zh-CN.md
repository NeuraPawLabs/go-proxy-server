# 快速开始

[English](getting-started.md)

本页介绍 Go Proxy Server 最常见的启动方式。

## 编译

```bash
make build
```

当前平台的可执行文件会输出到 `bin/go-proxy-server`。

## 运行模式

### 默认启动方式

```bash
./bin/go-proxy-server
```

- Linux / macOS 会直接启动本地 Web 管理后台
- Windows 优先进入系统托盘模式，失败后回退到本地 Web 管理后台
- 之前保存且启用了 `AutoStart` 的代理服务可能会自动恢复
- 之前保存且启用了 `AutoStart` 的内网穿透服务端或客户端配置也可能会自动恢复

### Web 管理模式

```bash
./bin/go-proxy-server web
```

- 仅监听 `localhost`
- 未指定 `-port` 或传入 `0` 时自动分配可用随机端口
- 启动日志会输出实际访问地址

### SOCKS5 代理

```bash
./bin/go-proxy-server socks -port 1080
```

### HTTP 代理

```bash
./bin/go-proxy-server http -port 8080
```

### 同时启动 SOCKS5 和 HTTP

```bash
./bin/go-proxy-server both -socks-port 1080 -http-port 8080
```

## 配置驱动运行

使用 `go-proxy-server run` 可以从 TOML 配置启动一个受管进程。

```bash
./bin/go-proxy-server run
./bin/go-proxy-server run -config /etc/go-proxy-server/config.toml
./bin/go-proxy-server run -web-port 8081 -socks-port 1081
```

如果省略 `-config`，`run` 会使用平台默认的运行配置路径：

- Linux：`$XDG_CONFIG_HOME/go-proxy-server/config.toml` 或 `~/.config/go-proxy-server/config.toml`
- macOS：`~/Library/Application Support/go-proxy-server/config.toml`
- Windows：`%APPDATA%\go-proxy-server\config.toml` 或 `~/go-proxy-server/config.toml`

命令行参数会覆盖 TOML 中的值，因此可以保留共享配置文件，同时在启动时做一次性覆盖。

使用 `run` 前，请先把下面的示例保存到该默认路径，或者通过 `-config` 显式指定配置文件。

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

`bind_listen = true` 时，代理默认使用入口连接命中的本机 IP 作为出站源地址。配置 `[[exit_bindings]]` 后，会先将 `ingress_local_ip` 映射到 `outbound_local_ip`。在阿里云 EIP NAT 模式下，这里应填写 ECS 网卡上的私网 IP，而不是公网 EIP；多 EIP 出口需要不同辅助私网 IP 和对应的源地址策略路由。

- TOML 中的 tunnel TLS 规则：
  `[tunnel_server]` 要么提供 `cert` 和 `key`，要么设置 `allow_insecure = true`
  `[tunnel_client]` 必须在 `ca`、`insecure_skip_verify = true`、`allow_insecure = true` 三者中选择一种
  `allow_insecure = true` 不能和 `cert`/`key`、`ca`、`server_name`、`insecure_skip_verify` 同时使用
  `insecure_skip_verify = true` 仍然走 TLS，只是跳过证书校验，因此不要求 `ca`
  服务端 `allow_insecure = true` 时，即使磁盘上已经存在托管证书文件，也仍然可以启动

这种方式保留了直接使用 `socks`、`http`、`both` 命令的能力，同时把单进程启动改成配置驱动。

## 服务工作流

`service install` 始终会把 `go-proxy-server run` 作为受管服务命令安装进去。

Linux 会安装系统级 `systemd` 服务：

```bash
sudo ./bin/go-proxy-server service install
sudo ./bin/go-proxy-server service install -config /etc/go-proxy-server/config.toml
sudo ./bin/go-proxy-server service status
```

- Linux 下如果 `service install` 省略 `-config`，安装出的 unit 会按以下顺序解析运行配置：
  优先使用保留下来的 `$XDG_CONFIG_HOME/go-proxy-server/config.toml`
  否则回退到 `SUDO_USER` 对应用户的 `~/.config/go-proxy-server/config.toml`
- 如果要做稳定的系统部署，建议显式传 `-config /etc/go-proxy-server/config.toml`。

macOS 会安装当前用户级别的 `launchd` LaunchAgent：

```bash
./bin/go-proxy-server service install
./bin/go-proxy-server service status
```

- macOS 下省略 `-config` 时，LaunchAgent 会在运行时读取当前用户的默认配置路径。

## 用户与白名单管理

```bash
./bin/go-proxy-server adduser -username alice -password secret123
./bin/go-proxy-server deluser -username alice
./bin/go-proxy-server listuser
./bin/go-proxy-server addip -ip 203.0.113.10
./bin/go-proxy-server delip -ip 203.0.113.10
./bin/go-proxy-server listip
```

## 内网穿透命令

```bash
# 集中式内网穿透服务端
./bin/go-proxy-server tunnel-server -listen :7000 -token demo-secret -cert server.crt -key server.key

# 常驻客户端
./bin/go-proxy-server tunnel-client -server example.com:7000 -token demo-secret -client node-a -ca ca.pem

# 路由管理
./bin/go-proxy-server tunnel-list-clients
./bin/go-proxy-server tunnel-list-routes
./bin/go-proxy-server tunnel-save-route -client node-a -name redis -target 127.0.0.1:6379 -public-port 16379 -enabled
./bin/go-proxy-server tunnel-del-route -client node-a -name redis
```

证书生成步骤见 `tunnel.zh-CN.md`。

## 日志说明

- CLI 模式默认输出到标准输出/标准错误。
- Windows 托盘模式默认写入应用数据目录中的 `app.log`。
- 核心状态数据存储在 `data.db` 中。

## 下一步

- [内网穿透说明](tunnel.zh-CN.md)
- [文档索引](README.zh-CN.md)
