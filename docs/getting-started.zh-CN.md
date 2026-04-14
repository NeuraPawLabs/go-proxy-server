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
- 不会自动启动内网穿透服务端或客户端进程

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
