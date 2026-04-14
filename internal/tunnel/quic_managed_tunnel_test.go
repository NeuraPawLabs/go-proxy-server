package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"testing"
	"time"
)

func startUDPEchoServer(t *testing.T) *net.UDPConn {
	t.Helper()

	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("start udp echo server: %v", err)
	}

	go func() {
		buffer := make([]byte, 65535)
		for {
			n, addr, err := conn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			if _, err := conn.WriteToUDP(buffer[:n], addr); err != nil {
				return
			}
		}
	}()

	return conn
}

func waitForManagedRouteActivePort(t *testing.T, store *ManagedStore, clientErrCh <-chan error) int {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		routes, err := store.ListRoutes()
		if err != nil {
			t.Fatalf("list routes: %v", err)
		}
		if len(routes) == 1 && routes[0].ActivePublicPort > 0 {
			return routes[0].ActivePublicPort
		}
		select {
		case err := <-clientErrCh:
			t.Fatalf("managed client exited early: %v", err)
		default:
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatal("timeout waiting for managed route to become active")
	return 0
}

func TestQUICManagedTunnelServerAndClientRelayTCP(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db := newManagedTestDB(t)
	serverTLS, clientTLS := newTestTLSConfigs(t)
	echoListener := startEchoServer(t)
	defer echoListener.Close()

	store := NewManagedStore(db)
	if err := store.SaveRouteWithOptions("agent-quic", "echo", echoListener.Addr().String(), 0, true, nil, ProtocolTCP, 0, 0); err != nil {
		t.Fatalf("save route: %v", err)
	}

	server := NewQUICManagedServer(db, "127.0.0.1:0", "127.0.0.1", "secret-token")
	server.TLSConfig = serverTLS
	server.SyncInterval = 100 * time.Millisecond
	server.HeartbeatInterval = 100 * time.Millisecond
	if err := server.Start(ctx); err != nil {
		t.Fatalf("start quic managed tunnel server: %v", err)
	}
	serverErrCh := make(chan error, 1)
	go func() { serverErrCh <- server.Wait() }()

	client := NewQUICManagedClient(server.ControlAddr, "secret-token", "agent-quic")
	client.TLSConfig = clientTLS
	client.ReconnectDelay = 100 * time.Millisecond
	client.HeartbeatInterval = 100 * time.Millisecond
	clientErrCh := make(chan error, 1)
	go func() { clientErrCh <- client.Run(ctx) }()

	activePort := waitForManagedRouteActivePort(t, store, clientErrCh)

	publicConn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", activePort), 5*time.Second)
	if err != nil {
		t.Fatalf("dial quic managed tunnel public port: %v", err)
	}
	defer publicConn.Close()
	if _, err := publicConn.Write([]byte("ping\n")); err != nil {
		t.Fatalf("write tcp payload: %v", err)
	}
	line, err := bufio.NewReader(publicConn).ReadString('\n')
	if err != nil {
		t.Fatalf("read tcp payload: %v", err)
	}
	if line != "ping\n" {
		t.Fatalf("unexpected tcp relay response: got %q want %q", line, "ping\\n")
	}

	var sessions []ManagedSessionSnapshot
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		sessions = server.ListActiveSessions()
		if len(sessions) == 1 && sessions[0].BytesFromPublic > 0 && sessions[0].BytesToPublic > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one active session, got %+v", sessions)
	}
	if sessions[0].Engine != EngineQUIC || sessions[0].Protocol != ProtocolTCP {
		t.Fatalf("unexpected quic tcp session metadata: %+v", sessions[0])
	}

	cancel()
	select {
	case err := <-clientErrCh:
		if err != nil {
			t.Fatalf("client shutdown error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for quic client shutdown")
	}
	select {
	case err := <-serverErrCh:
		if err != nil {
			t.Fatalf("server shutdown error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for quic server shutdown")
	}
}

func TestQUICManagedTunnelServerAndClientRelayUDP(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db := newManagedTestDB(t)
	serverTLS, clientTLS := newTestTLSConfigs(t)
	echoConn := startUDPEchoServer(t)
	defer echoConn.Close()

	store := NewManagedStore(db)
	if err := store.SaveRouteWithOptions("agent-quic", "dns", echoConn.LocalAddr().String(), 0, true, nil, ProtocolUDP, 10, 1200); err != nil {
		t.Fatalf("save udp route: %v", err)
	}

	server := NewQUICManagedServer(db, "127.0.0.1:0", "127.0.0.1", "secret-token")
	server.TLSConfig = serverTLS
	server.SyncInterval = 100 * time.Millisecond
	server.HeartbeatInterval = 100 * time.Millisecond
	if err := server.Start(ctx); err != nil {
		t.Fatalf("start quic managed tunnel server: %v", err)
	}
	serverErrCh := make(chan error, 1)
	go func() { serverErrCh <- server.Wait() }()

	client := NewQUICManagedClient(server.ControlAddr, "secret-token", "agent-quic")
	client.TLSConfig = clientTLS
	client.ReconnectDelay = 100 * time.Millisecond
	client.HeartbeatInterval = 100 * time.Millisecond
	clientErrCh := make(chan error, 1)
	go func() { clientErrCh <- client.Run(ctx) }()

	activePort := waitForManagedRouteActivePort(t, store, clientErrCh)

	publicAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", activePort))
	if err != nil {
		t.Fatalf("resolve public udp addr: %v", err)
	}
	publicConn, err := net.DialUDP("udp", nil, publicAddr)
	if err != nil {
		t.Fatalf("dial public udp port: %v", err)
	}
	defer publicConn.Close()
	_ = publicConn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := publicConn.Write([]byte("ping")); err != nil {
		t.Fatalf("write udp payload: %v", err)
	}

	buffer := make([]byte, 64)
	n, err := publicConn.Read(buffer)
	if err != nil {
		t.Fatalf("read udp payload: %v", err)
	}
	if string(buffer[:n]) != "ping" {
		t.Fatalf("unexpected udp relay response: got %q want %q", string(buffer[:n]), "ping")
	}

	var sessions []ManagedSessionSnapshot
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		sessions = server.ListActiveSessions()
		if len(sessions) == 1 && sessions[0].PacketsFromPublic > 0 && sessions[0].PacketsToPublic > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one udp session, got %+v", sessions)
	}
	if sessions[0].Engine != EngineQUIC || sessions[0].Protocol != ProtocolUDP {
		t.Fatalf("unexpected quic udp session metadata: %+v", sessions[0])
	}

	cancel()
	select {
	case err := <-clientErrCh:
		if err != nil {
			t.Fatalf("client shutdown error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for quic client shutdown")
	}
	select {
	case err := <-serverErrCh:
		if err != nil {
			t.Fatalf("server shutdown error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for quic server shutdown")
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		client.udpMu.Lock()
		clientSessionCount := len(client.udpSessions)
		client.udpMu.Unlock()
		if clientSessionCount == 0 && server.ActiveSessionCount() == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	client.udpMu.Lock()
	clientSessionCount := len(client.udpSessions)
	client.udpMu.Unlock()
	t.Fatalf("expected udp sessions to be cleaned up, client=%d server=%d", clientSessionCount, server.ActiveSessionCount())
}

func TestQUICManagedClientIgnoresInvalidServerDatagram(t *testing.T) {
	client := NewQUICManagedClient("127.0.0.1:7443", "secret-token", "agent-quic")
	if err := client.handleServerDatagramPayload(nil, []byte("not-a-valid-datagram")); err != nil {
		t.Fatalf("expected invalid server datagram to be ignored, got %v", err)
	}
}

func TestQUICManagedServerIgnoresInvalidClientDatagram(t *testing.T) {
	server := &QUICManagedServer{}
	session := &quicManagedClientSession{name: "agent-quic", routes: map[string]quicManagedRouteBinding{}}
	if err := server.handleClientDatagramPayload(session, []byte("not-a-valid-datagram")); err != nil {
		t.Fatalf("expected invalid client datagram to be ignored, got %v", err)
	}
}

func TestQUICManagedClientCloseUDPSessionsClosesLocalSockets(t *testing.T) {
	listener := startUDPEchoServer(t)
	defer listener.Close()

	targetAddr, err := net.ResolveUDPAddr("udp", listener.LocalAddr().String())
	if err != nil {
		t.Fatalf("resolve udp addr: %v", err)
	}
	udpConn, err := net.DialUDP("udp", nil, targetAddr)
	if err != nil {
		t.Fatalf("dial udp addr: %v", err)
	}

	client := NewQUICManagedClient("127.0.0.1:7443", "secret-token", "agent-quic")
	client.udpSessions["session-1"] = &quicClientUDPSession{
		client: client,
		id:     "session-1",
		route: routeSync{
			Protocol:          ProtocolUDP,
			UDPIdleTimeoutSec: 60,
			UDPMaxPayload:     1200,
		},
		conn:         udpConn,
		lastActivity: time.Now(),
	}

	client.closeUDPSessions()

	client.udpMu.Lock()
	defer client.udpMu.Unlock()
	if len(client.udpSessions) != 0 {
		t.Fatalf("expected udp sessions to be cleared, got %d", len(client.udpSessions))
	}
	if _, err := udpConn.Write([]byte("ping")); err == nil {
		t.Fatal("expected closed udp conn write to fail")
	}
}
