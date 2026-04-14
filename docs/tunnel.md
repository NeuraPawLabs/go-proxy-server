# Tunnel Management

[中文版本](tunnel.zh-CN.md)

Go Proxy Server supports a centralized tunnel model:

- one `tunnel-server`
- many long-lived `tunnel-client` agents
- routes managed from Web UI or CLI
- `classic` engine for TCP
- `quic` engine for TCP and UDP

This replaces the older "start one client for one exposed port" workflow.

## Architecture

1. Start a public `tunnel-server`.
2. Connect one or more `tunnel-client` agents from private networks.
3. Create or update routes on the server side.
4. The server syncs route changes to online clients automatically.

## Engine Selection

- `classic`: stable TCP-only mode, compatible with existing deployments
- `quic`: modern transport with TCP-over-QUIC streams and UDP-over-QUIC datagrams

Use the same engine on both server and client. UDP routes require `quic`.

## QUIC Notes

- QUIC uses TLS 1.3 and a single long-lived encrypted connection between server and client.
- TCP routes are forwarded through QUIC bidirectional streams.
- UDP routes are forwarded through QUIC datagrams.
- If a malformed UDP datagram is received, it is ignored and logged, instead of tearing down the whole client session.
- When the QUIC connection closes, route listeners and per-route UDP sessions are cleaned up automatically on both server and client.

## Start the Server

Classic:

```bash
./bin/go-proxy-server tunnel-server \
  -engine classic \
  -listen :7000 \
  -token demo-secret \
  -cert server.crt \
  -key server.key
```

QUIC:

```bash
./bin/go-proxy-server tunnel-server \
  -engine quic \
  -listen :7443 \
  -token demo-secret \
  -cert server.crt \
  -key server.key
```

Use `-allow-insecure` only for trusted local testing from CLI. The Web UI intentionally does not expose plaintext or skip-verification tunnel settings.

## Generate Certificates

The tunnel server expects a TLS certificate and key, and the client currently requires an explicit `-ca` file.

For quick self-hosted deployment, you can create a small private CA and sign one server certificate with OpenSSL:

```bash
# 1) create a CA
openssl genrsa -out ca.key 4096
openssl req -x509 -new -nodes \
  -key ca.key \
  -sha256 \
  -days 3650 \
  -out ca.pem \
  -subj "/CN=go-proxy-server-ca"

# 2) create the server key and CSR
openssl genrsa -out server.key 4096
openssl req -new \
  -key server.key \
  -out server.csr \
  -subj "/CN=your.server.example"

# 3) add SAN entries that match how clients connect
cat > server.ext <<'EOF'
subjectAltName=DNS:your.server.example,IP:203.0.113.10
extendedKeyUsage=serverAuth
EOF

# 4) sign the server certificate
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

Notes:

- Replace `your.server.example` and `203.0.113.10` with the real public domain or IP used by clients.
- If clients connect by IP, that IP must appear in `subjectAltName`.
- If the certificate name and `-server` host differ, either fix the certificate SAN or pass `-server-name`.
- Keep `ca.key` private. Distribute only `ca.pem` to clients.

## Start a Client

Classic:

```bash
./bin/go-proxy-server tunnel-client \
  -engine classic \
  -server example.com:7000 \
  -token demo-secret \
  -client node-a \
  -ca ca.pem
```

QUIC:

```bash
./bin/go-proxy-server tunnel-client \
  -engine quic \
  -server example.com:7443 \
  -token demo-secret \
  -client node-a \
  -ca ca.pem
```

The client keeps a persistent control connection and waits for route assignments.

In the Web UI, client mode uses verified TLS only:

- upload a CA file, or
- let it automatically reuse the locally managed server CA when the same machine also runs tunnel server mode

## Manage Routes from CLI

TCP route:

```bash
./bin/go-proxy-server tunnel-save-route \
  -client node-a \
  -name redis \
  -protocol tcp \
  -target 127.0.0.1:6379 \
  -public-port 16379 \
  -enabled
```

UDP route:

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

UDP parameter reference:

- `-udp-idle-timeout`: idle timeout for one UDP public-side session, in seconds
  - default: `60`
  - cleanup runs periodically on the server side and removes expired UDP sessions
  - increase this for long-idle request/response protocols
- `-udp-max-payload`: maximum payload size for one UDP packet
  - default: `1200`
  - recommended: keep it around `1200` to avoid fragmentation on typical internet paths
  - upper bound: validated by the server before the route is accepted

```bash
./bin/go-proxy-server tunnel-list-clients
./bin/go-proxy-server tunnel-list-routes
./bin/go-proxy-server tunnel-del-route -client node-a -name redis
```

## Manage Routes from Web UI

Use the Web admin UI and open the `Tunnel` section to:

- switch between `server` and `client` workbench modes
- view connected clients
- inspect stale or offline clients
- create and update routes
- choose `tcp` or `udp` protocol when the server runs in `quic` mode
- enable or disable routes
- see requested public ports and active assigned ports
- inspect real-time sessions, traffic, and protocol/engine metadata

## Notes

- `public-port 0` means auto-assign a free public port.
- `tunnel-server` can also define an auto port range; when set, `public-port 0` only allocates inside that range.
- The server persists the last assigned auto port so the route can reuse the same port after restart when possible.
- UDP session cleanup is timer-based and runs continuously while the route is active.
- QUIC UDP forwarding uses route-level payload limits instead of a single fixed global packet size.
- Route state is persisted in SQLite.
- Client heartbeat and active public port state are written back by the server.
