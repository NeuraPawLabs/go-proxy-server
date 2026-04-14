# 内网穿透说明

[English](tunnel.md)

Go Proxy Server 现在采用集中式内网穿透模型：

- 一个 `tunnel-server`
- 多个常驻 `tunnel-client`
- 通过 Web 后台或 CLI 管理路由
- `classic` 引擎支持 TCP
- `quic` 引擎支持 TCP 与 UDP

这取代了过去“每开放一个端口就单独启动一个客户端命令”的方式。

## 工作方式

1. 在公网启动 `tunnel-server`。
2. 在内网机器上启动一个或多个 `tunnel-client` 常驻连接。
3. 在服务端侧新增或修改路由。
4. 在线客户端会自动收到路由同步，无需手动重启或追加命令。

## 引擎选择

- `classic`：稳定的 TCP 单协议模式，兼容当前已有部署
- `quic`：基于 QUIC，支持 TCP over QUIC 与 UDP over QUIC

服务端与客户端要使用同一种引擎。`udp` 路由要求双方都使用 `quic`。

## QUIC 说明

- QUIC 使用 TLS 1.3，在服务端与客户端之间保持一条长连接。
- TCP 路由通过 QUIC 双向流转发。
- UDP 路由通过 QUIC Datagram 转发。
- 如果收到损坏或无法解析的 UDP 数据报，只会记录日志并忽略，不会直接把整条客户端连接打断。
- 当 QUIC 连接断开时，服务端与客户端上的路由监听器、UDP 会话都会自动清理。

## 启动服务端

Classic：

```bash
./bin/go-proxy-server tunnel-server \
  -engine classic \
  -listen :7000 \
  -token demo-secret \
  -cert server.crt \
  -key server.key
```

QUIC：

```bash
./bin/go-proxy-server tunnel-server \
  -engine quic \
  -listen :7443 \
  -token demo-secret \
  -cert server.crt \
  -key server.key
```

除 CLI 下的可信测试场景外，不建议使用 `-allow-insecure`。Web 后台不会提供明文连接或跳过校验的配置项。

## 生成证书

内网穿透服务端需要 TLS 证书和私钥，客户端当前也要求显式传入 `-ca`。

如果你是自托管部署，最直接的方式是先生成一个私有 CA，再签发服务端证书：

```bash
# 1) 生成 CA
openssl genrsa -out ca.key 4096
openssl req -x509 -new -nodes \
  -key ca.key \
  -sha256 \
  -days 3650 \
  -out ca.pem \
  -subj "/CN=go-proxy-server-ca"

# 2) 生成服务端私钥和 CSR
openssl genrsa -out server.key 4096
openssl req -new \
  -key server.key \
  -out server.csr \
  -subj "/CN=your.server.example"

# 3) 添加和客户端访问方式一致的 SAN
cat > server.ext <<'EOF'
subjectAltName=DNS:your.server.example,IP:203.0.113.10
extendedKeyUsage=serverAuth
EOF

# 4) 用 CA 签发服务端证书
openssl x509 -req \
  -in server.csr \
  -CA ca.pem \
  -CAkey ca.key \
  -CAcreateserial \
  -out server.crt \
  -days 825 \
  -sha256 \
  -extfile server.ext
```

说明：

- 把 `your.server.example` 和 `203.0.113.10` 替换成客户端实际连接使用的公网域名或 IP。
- 如果客户端是通过 IP 连接，那么这个 IP 必须出现在 `subjectAltName` 里。
- 如果证书里的名称和 `-server` 指定的主机名不一致，要么重签证书，要么显式传 `-server-name`。
- `ca.key` 只保留在签发环境中，不要下发到客户端；客户端只需要 `ca.pem`。

## 启动客户端

Classic：

```bash
./bin/go-proxy-server tunnel-client \
  -engine classic \
  -server example.com:7000 \
  -token demo-secret \
  -client node-a \
  -ca ca.pem
```

QUIC：

```bash
./bin/go-proxy-server tunnel-client \
  -engine quic \
  -server example.com:7443 \
  -token demo-secret \
  -client node-a \
  -ca ca.pem
```

客户端会保持控制连接，并等待服务端下发路由。

在 Web 后台中，客户端模式只支持校验证书后的 TLS：

- 可以直接上传 CA 文件
- 也可以在本机同时启用了服务端模式时，自动复用本机托管 CA

## 通过 CLI 管理路由

TCP 路由：

```bash
./bin/go-proxy-server tunnel-save-route \
  -client node-a \
  -name redis \
  -protocol tcp \
  -target 127.0.0.1:6379 \
  -public-port 16379 \
  -enabled
```

UDP 路由：

```bash
./bin/go-proxy-server tunnel-save-route \
  -client node-a \
  -name dns \
  -protocol udp \
  -target 127.0.0.1:53 \
  -public-port 1053 \
  -udp-idle-timeout 60 \
  -udp-max-payload 1200 \
  -enabled
```

UDP 参数说明：

- `-udp-idle-timeout`：单个 UDP 公网会话的空闲超时，单位秒
  - 默认值：`60`
  - 服务端会周期性清理超时会话
  - 如果你的协议会长时间空闲后再继续收发，可以适当调大
- `-udp-max-payload`：单个 UDP 报文允许的最大负载
  - 默认值：`1200`
  - 推荐值：保持在 `1200` 左右，尽量避免公网路径分片
  - 上限会在服务端保存路由前做校验

```bash
./bin/go-proxy-server tunnel-list-clients
./bin/go-proxy-server tunnel-list-routes
./bin/go-proxy-server tunnel-del-route -client node-a -name redis
```

## 通过 Web 后台管理

在 Web 管理界面的“内网穿透”页面可以：

- 在“服务端模式”和“客户端模式”之间切换
- 查看客户端在线状态
- 查看离线或心跳过期的客户端
- 新增、修改、删除透传路由
- 在服务端运行 `quic` 引擎时，为路由选择 `tcp` 或 `udp`
- 启用或停用路由
- 查看期望公网端口和实际生效端口
- 查看实时连接、流量、协议和引擎信息

## 补充说明

- `public-port 0` 表示自动分配一个可用公网端口。
- `tunnel-server` 可额外配置自动端口范围；配置后，`public-port 0` 只会在该范围内选择端口。
- 服务端会持久化自动分配后的端口，后续重启时会优先复用原来的端口。
- UDP 会话采用定时清理机制，只要路由处于激活状态就会持续回收过期会话。
- QUIC 下的 UDP 转发使用路由级负载限制，而不是一个固定的全局包大小。
- 路由配置保存在 SQLite 中。
- 客户端心跳和实际生效端口由服务端回写。
