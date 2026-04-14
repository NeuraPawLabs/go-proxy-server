package tunnel

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/apeming/go-proxy-server/internal/models"
)

func newManagedTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.TunnelClient{}, &models.TunnelRoute{}); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	if err := models.EnsureTunnelConstraints(db); err != nil {
		t.Fatalf("apply tunnel constraints: %v", err)
	}
	return db
}

func TestManagedTunnelServerAndClientRelayTraffic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db := newManagedTestDB(t)
	serverTLS, clientTLS := newTestTLSConfigs(t)
	echoListener := startEchoServer(t)
	defer echoListener.Close()

	store := NewManagedStore(db)
	if err := store.SaveRoute("agent-1", "echo", echoListener.Addr().String(), 0, true, nil); err != nil {
		t.Fatalf("save route: %v", err)
	}

	server := NewManagedServer(db, "127.0.0.1:0", "127.0.0.1", "secret-token")
	server.TLSConfig = serverTLS
	server.SyncInterval = 100 * time.Millisecond
	server.HeartbeatInterval = 100 * time.Millisecond
	if err := server.Start(ctx); err != nil {
		t.Fatalf("start managed tunnel server: %v", err)
	}
	serverErrCh := make(chan error, 1)
	go func() { serverErrCh <- server.Wait() }()

	client := NewManagedClient(server.ControlAddr, "secret-token", "agent-1")
	client.TLSConfig = clientTLS
	client.ReconnectDelay = 100 * time.Millisecond
	clientErrCh := make(chan error, 1)
	go func() { clientErrCh <- client.Run(ctx) }()

	var activePort int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		routes, err := store.ListRoutes()
		if err != nil {
			t.Fatalf("list routes: %v", err)
		}
		if len(routes) == 1 && routes[0].ActivePublicPort > 0 {
			activePort = routes[0].ActivePublicPort
			break
		}
		select {
		case err := <-clientErrCh:
			t.Fatalf("managed client exited early: %v", err)
		default:
		}
		time.Sleep(100 * time.Millisecond)
	}
	if activePort == 0 {
		t.Fatal("timeout waiting for managed route to become active")
	}

	publicConn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", activePort), 5*time.Second)
	if err != nil {
		t.Fatalf("dial managed tunnel public port: %v", err)
	}
	defer publicConn.Close()
	if _, err := io.WriteString(publicConn, "ping\n"); err != nil {
		t.Fatalf("write to managed tunnel: %v", err)
	}
	line, err := bufio.NewReader(publicConn).ReadString('\n')
	if err != nil {
		t.Fatalf("read from managed tunnel: %v", err)
	}
	if line != "ping\n" {
		t.Fatalf("unexpected managed tunnel response: got %q want %q", line, "ping\\n")
	}

	cancel()
	select {
	case err := <-clientErrCh:
		if err != nil {
			t.Fatalf("client shutdown error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for managed client shutdown")
	}
	select {
	case err := <-serverErrCh:
		if err != nil {
			t.Fatalf("server shutdown error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for managed server shutdown")
	}
	if got := server.ListActiveSessions(); len(got) != 0 {
		t.Fatalf("expected managed sessions to be cleared after shutdown: %+v", got)
	}
}

func TestManagedTunnelServerListsActiveSessions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db := newManagedTestDB(t)
	serverTLS, clientTLS := newTestTLSConfigs(t)
	echoListener := startEchoServer(t)
	defer echoListener.Close()

	store := NewManagedStore(db)
	if err := store.SaveRoute("agent-1", "echo", echoListener.Addr().String(), 0, true, nil); err != nil {
		t.Fatalf("save route: %v", err)
	}

	server := NewManagedServer(db, "127.0.0.1:0", "127.0.0.1", "secret-token")
	server.TLSConfig = serverTLS
	server.SyncInterval = 100 * time.Millisecond
	server.HeartbeatInterval = 100 * time.Millisecond
	if err := server.Start(ctx); err != nil {
		t.Fatalf("start managed tunnel server: %v", err)
	}
	serverErrCh := make(chan error, 1)
	go func() { serverErrCh <- server.Wait() }()

	client := NewManagedClient(server.ControlAddr, "secret-token", "agent-1")
	client.TLSConfig = clientTLS
	client.ReconnectDelay = 100 * time.Millisecond
	clientErrCh := make(chan error, 1)
	go func() { clientErrCh <- client.Run(ctx) }()

	var activePort int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		routes, err := store.ListRoutes()
		if err != nil {
			t.Fatalf("list routes: %v", err)
		}
		if len(routes) == 1 && routes[0].ActivePublicPort > 0 {
			activePort = routes[0].ActivePublicPort
			break
		}
		select {
		case err := <-clientErrCh:
			t.Fatalf("managed client exited early: %v", err)
		default:
		}
		time.Sleep(100 * time.Millisecond)
	}
	if activePort == 0 {
		t.Fatal("timeout waiting for managed route to become active")
	}

	publicConn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", activePort), 5*time.Second)
	if err != nil {
		t.Fatalf("dial managed tunnel public port: %v", err)
	}
	if _, err := io.WriteString(publicConn, "ping\n"); err != nil {
		t.Fatalf("write to managed tunnel: %v", err)
	}
	if tcpConn, ok := publicConn.(*net.TCPConn); ok {
		_ = tcpConn.CloseWrite()
	}
	line, err := bufio.NewReader(publicConn).ReadString('\n')
	if err != nil {
		t.Fatalf("read from managed tunnel: %v", err)
	}
	if line != "ping\n" {
		t.Fatalf("unexpected managed tunnel response: got %q want %q", line, "ping\\n")
	}

	var sessions []ManagedSessionSnapshot
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		sessions = server.ListActiveSessions()
		if len(sessions) == 1 && sessions[0].BytesFromPublic > 0 && sessions[0].BytesToPublic > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one active managed session, got %+v", sessions)
	}
	if sessions[0].ClientName != "agent-1" || sessions[0].RouteName != "echo" {
		t.Fatalf("unexpected active managed session: %+v", sessions[0])
	}
	if sessions[0].PublicPort != activePort {
		t.Fatalf("unexpected managed session public port: got %d want %d", sessions[0].PublicPort, activePort)
	}
	if sessions[0].SourceAddr == "" {
		t.Fatalf("unexpected managed session source ip: %+v", sessions[0])
	}

	_ = publicConn.Close()

	cancel()
	select {
	case err := <-clientErrCh:
		if err != nil {
			t.Fatalf("client shutdown error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for managed client shutdown")
	}
	select {
	case err := <-serverErrCh:
		if err != nil {
			t.Fatalf("server shutdown error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for managed server shutdown")
	}
}

func TestManagedTunnelRouteRejectsNonWhitelistedSourceIP(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db := newManagedTestDB(t)
	serverTLS, clientTLS := newTestTLSConfigs(t)
	echoListener := startEchoServer(t)
	defer echoListener.Close()

	store := NewManagedStore(db)
	if err := store.SaveRoute("agent-1", "echo", echoListener.Addr().String(), 0, true, []string{"203.0.113.10"}); err != nil {
		t.Fatalf("save route with whitelist: %v", err)
	}

	server := NewManagedServer(db, "127.0.0.1:0", "127.0.0.1", "secret-token")
	server.TLSConfig = serverTLS
	server.SyncInterval = 100 * time.Millisecond
	server.HeartbeatInterval = 100 * time.Millisecond
	if err := server.Start(ctx); err != nil {
		t.Fatalf("start managed tunnel server: %v", err)
	}
	serverErrCh := make(chan error, 1)
	go func() { serverErrCh <- server.Wait() }()

	client := NewManagedClient(server.ControlAddr, "secret-token", "agent-1")
	client.TLSConfig = clientTLS
	client.ReconnectDelay = 100 * time.Millisecond
	clientErrCh := make(chan error, 1)
	go func() { clientErrCh <- client.Run(ctx) }()

	var activePort int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		routes, err := store.ListRoutes()
		if err != nil {
			t.Fatalf("list routes: %v", err)
		}
		if len(routes) == 1 && routes[0].ActivePublicPort > 0 {
			activePort = routes[0].ActivePublicPort
			break
		}
		select {
		case err := <-clientErrCh:
			t.Fatalf("managed client exited early: %v", err)
		default:
		}
		time.Sleep(100 * time.Millisecond)
	}
	if activePort == 0 {
		t.Fatal("timeout waiting for managed route to become active")
	}

	publicConn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", activePort), 5*time.Second)
	if err != nil {
		t.Fatalf("dial managed tunnel public port: %v", err)
	}
	defer publicConn.Close()
	_ = publicConn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.WriteString(publicConn, "ping\n"); err != nil {
		t.Fatalf("write to managed tunnel: %v", err)
	}

	buf := make([]byte, 16)
	_, err = publicConn.Read(buf)
	if err == nil {
		t.Fatal("expected non-whitelisted public connection to be closed")
	}

	cancel()
	select {
	case err := <-clientErrCh:
		if err != nil {
			t.Fatalf("client shutdown error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for managed client shutdown")
	}
	select {
	case err := <-serverErrCh:
		if err != nil {
			t.Fatalf("server shutdown error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for managed server shutdown")
	}
}

func TestManagedTunnelServerAssignsAutoPortWithinConfiguredRange(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db := newManagedTestDB(t)
	serverTLS, clientTLS := newTestTLSConfigs(t)
	echoListener := startEchoServer(t)
	defer echoListener.Close()

	store := NewManagedStore(db)
	if err := store.SaveRoute("agent-1", "echo", echoListener.Addr().String(), 0, true, nil); err != nil {
		t.Fatalf("save route: %v", err)
	}

	server := NewManagedServer(db, "127.0.0.1:0", "127.0.0.1", "secret-token")
	server.TLSConfig = serverTLS
	server.SyncInterval = 100 * time.Millisecond
	server.HeartbeatInterval = 100 * time.Millisecond
	server.AutoPortRangeStart = 34080
	server.AutoPortRangeEnd = 34082
	if err := server.Start(ctx); err != nil {
		t.Fatalf("start managed tunnel server: %v", err)
	}
	serverErrCh := make(chan error, 1)
	go func() { serverErrCh <- server.Wait() }()

	client := NewManagedClient(server.ControlAddr, "secret-token", "agent-1")
	client.TLSConfig = clientTLS
	client.ReconnectDelay = 100 * time.Millisecond
	clientErrCh := make(chan error, 1)
	go func() { clientErrCh <- client.Run(ctx) }()

	var activePort int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		routes, err := store.ListRoutes()
		if err != nil {
			t.Fatalf("list routes: %v", err)
		}
		if len(routes) == 1 && routes[0].ActivePublicPort > 0 {
			activePort = routes[0].ActivePublicPort
			break
		}
		select {
		case err := <-clientErrCh:
			t.Fatalf("managed client exited early: %v", err)
		default:
		}
		time.Sleep(100 * time.Millisecond)
	}
	if activePort < server.AutoPortRangeStart || activePort > server.AutoPortRangeEnd {
		t.Fatalf("expected active port in range %d-%d, got %d", server.AutoPortRangeStart, server.AutoPortRangeEnd, activePort)
	}

	cancel()
	select {
	case err := <-clientErrCh:
		if err != nil {
			t.Fatalf("client shutdown error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for managed client shutdown")
	}
	select {
	case err := <-serverErrCh:
		if err != nil {
			t.Fatalf("server shutdown error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for managed server shutdown")
	}
}

func TestManagedTunnelServerReportsAutoPortRangeExhausted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db := newManagedTestDB(t)
	serverTLS, clientTLS := newTestTLSConfigs(t)
	echoListener := startEchoServer(t)
	defer echoListener.Close()

	store := NewManagedStore(db)
	if err := store.SaveRoute("agent-1", "echo", echoListener.Addr().String(), 0, true, nil); err != nil {
		t.Fatalf("save route: %v", err)
	}

	occupied, err := net.Listen("tcp", "127.0.0.1:34180")
	if err != nil {
		t.Fatalf("occupy port: %v", err)
	}
	defer occupied.Close()

	server := NewManagedServer(db, "127.0.0.1:0", "127.0.0.1", "secret-token")
	server.TLSConfig = serverTLS
	server.SyncInterval = 100 * time.Millisecond
	server.HeartbeatInterval = 100 * time.Millisecond
	server.AutoPortRangeStart = 34180
	server.AutoPortRangeEnd = 34180
	if err := server.Start(ctx); err != nil {
		t.Fatalf("start managed tunnel server: %v", err)
	}
	serverErrCh := make(chan error, 1)
	go func() { serverErrCh <- server.Wait() }()

	client := NewManagedClient(server.ControlAddr, "secret-token", "agent-1")
	client.TLSConfig = clientTLS
	client.ReconnectDelay = 100 * time.Millisecond
	clientErrCh := make(chan error, 1)
	go func() { clientErrCh <- client.Run(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		routes, err := store.ListRoutes()
		if err != nil {
			t.Fatalf("list routes: %v", err)
		}
		if len(routes) == 1 && routes[0].LastError != "" {
			if routes[0].ActivePublicPort != 0 {
				t.Fatalf("expected no active public port when range is exhausted: %+v", routes[0])
			}
			if routes[0].LastError != "listen public port for route echo: no available public port in auto allocation range 34180-34180" {
				t.Fatalf("unexpected route error: %s", routes[0].LastError)
			}
			cancel()
			select {
			case err := <-clientErrCh:
				if err != nil {
					t.Fatalf("client shutdown error: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("timeout waiting for managed client shutdown")
			}
			select {
			case err := <-serverErrCh:
				if err != nil {
					t.Fatalf("server shutdown error: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("timeout waiting for managed server shutdown")
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatal("timeout waiting for managed route range exhaustion error")
}

func TestManagedStoreRejectsInvalidRouteWhitelist(t *testing.T) {
	db := newManagedTestDB(t)
	store := NewManagedStore(db)

	if err := store.SaveRoute("agent-1", "echo", "127.0.0.1:8080", 18080, true, []string{"not-an-ip"}); err == nil {
		t.Fatal("expected invalid route ip whitelist to be rejected")
	}
}

func TestManagedTunnelServerReusesAssignedAutoPortAfterRestart(t *testing.T) {
	db := newManagedTestDB(t)
	serverTLS, clientTLS := newTestTLSConfigs(t)
	echoListener := startEchoServer(t)
	defer echoListener.Close()

	store := NewManagedStore(db)
	if err := store.SaveRoute("agent-1", "echo", echoListener.Addr().String(), 0, true, nil); err != nil {
		t.Fatalf("save route: %v", err)
	}

	startServerAndClient := func(ctx context.Context) (*ManagedServer, chan error, chan error) {
		server := NewManagedServer(db, "127.0.0.1:0", "127.0.0.1", "secret-token")
		server.TLSConfig = serverTLS
		server.SyncInterval = 100 * time.Millisecond
		server.HeartbeatInterval = 100 * time.Millisecond
		server.AutoPortRangeStart = 34200
		server.AutoPortRangeEnd = 34210
		if err := server.Start(ctx); err != nil {
			t.Fatalf("start managed tunnel server: %v", err)
		}
		serverErrCh := make(chan error, 1)
		go func() { serverErrCh <- server.Wait() }()

		client := NewManagedClient(server.ControlAddr, "secret-token", "agent-1")
		client.TLSConfig = clientTLS
		client.ReconnectDelay = 100 * time.Millisecond
		clientErrCh := make(chan error, 1)
		go func() { clientErrCh <- client.Run(ctx) }()
		return server, serverErrCh, clientErrCh
	}

	waitForAssignedPort := func(clientErrCh chan error) int {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			routes, err := store.ListRoutes()
			if err != nil {
				t.Fatalf("list routes: %v", err)
			}
			if len(routes) == 1 && routes[0].AssignedPublicPort > 0 && routes[0].ActivePublicPort > 0 {
				return routes[0].AssignedPublicPort
			}
			select {
			case err := <-clientErrCh:
				t.Fatalf("managed client exited early: %v", err)
			default:
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("timeout waiting for managed route assignment")
		return 0
	}

	ctxA, cancelA := context.WithCancel(context.Background())
	_, serverErrA, clientErrA := startServerAndClient(ctxA)
	firstPort := waitForAssignedPort(clientErrA)
	cancelA()
	select {
	case err := <-clientErrA:
		if err != nil {
			t.Fatalf("first client shutdown error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for first managed client shutdown")
	}
	select {
	case err := <-serverErrA:
		if err != nil {
			t.Fatalf("first server shutdown error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for first managed server shutdown")
	}

	ctxB, cancelB := context.WithCancel(context.Background())
	_, serverErrB, clientErrB := startServerAndClient(ctxB)
	secondPort := waitForAssignedPort(clientErrB)
	cancelB()
	select {
	case err := <-clientErrB:
		if err != nil {
			t.Fatalf("second client shutdown error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for second managed client shutdown")
	}
	select {
	case err := <-serverErrB:
		if err != nil {
			t.Fatalf("second server shutdown error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for second managed server shutdown")
	}

	if firstPort != secondPort {
		t.Fatalf("expected managed tunnel to reuse assigned port after restart: first=%d second=%d", firstPort, secondPort)
	}
}

func TestManagedStorePreservesAssignedPortAfterRouteUpdate(t *testing.T) {
	db := newManagedTestDB(t)
	store := NewManagedStore(db)

	if err := store.SaveRoute("agent-1", "echo", "127.0.0.1:8080", 0, true, nil); err != nil {
		t.Fatalf("save route: %v", err)
	}
	if err := store.UpdateRouteRuntime("agent-1", "echo", 34201, ""); err != nil {
		t.Fatalf("assign runtime port: %v", err)
	}
	if err := store.SaveRoute("agent-1", "echo", "127.0.0.1:9090", 0, true, []string{"203.0.113.10"}); err != nil {
		t.Fatalf("update route after assignment: %v", err)
	}

	routes, err := store.ListRoutes()
	if err != nil {
		t.Fatalf("list routes: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("unexpected route count: got %d want 1", len(routes))
	}
	if routes[0].AssignedPublicPort != 34201 {
		t.Fatalf("assigned port changed unexpectedly: got %d want %d", routes[0].AssignedPublicPort, 34201)
	}
	if routes[0].TargetAddr != "127.0.0.1:9090" {
		t.Fatalf("target address did not update: got %q want %q", routes[0].TargetAddr, "127.0.0.1:9090")
	}
}

func TestManagedStoreRejectsPublicPortChangeAfterAssignment(t *testing.T) {
	db := newManagedTestDB(t)
	store := NewManagedStore(db)

	if err := store.SaveRoute("agent-1", "echo", "127.0.0.1:8080", 0, true, nil); err != nil {
		t.Fatalf("save route: %v", err)
	}
	if err := store.UpdateRouteRuntime("agent-1", "echo", 34201, ""); err != nil {
		t.Fatalf("assign runtime port: %v", err)
	}
	if err := store.SaveRoute("agent-1", "echo", "127.0.0.1:8080", 18080, true, nil); err == nil {
		t.Fatal("expected public port change after assignment to be rejected")
	}
}

func TestManagedStoreRejectsProtocolChangeAfterAssignment(t *testing.T) {
	db := newManagedTestDB(t)
	store := NewManagedStore(db)

	if err := store.SaveRouteWithOptions("agent-1", "echo", "127.0.0.1:8080", 0, true, nil, ProtocolTCP, 0, 0); err != nil {
		t.Fatalf("save route: %v", err)
	}
	if err := store.UpdateRouteRuntime("agent-1", "echo", 34201, ""); err != nil {
		t.Fatalf("assign runtime port: %v", err)
	}
	if err := store.SaveRouteWithOptions("agent-1", "echo", "127.0.0.1:8080", 0, true, nil, ProtocolUDP, 10, 1200); err == nil {
		t.Fatal("expected protocol change after assignment to be rejected")
	}
}

func TestManagedStoreAssignedPortUniquePerProtocol(t *testing.T) {
	db := newManagedTestDB(t)
	store := NewManagedStore(db)

	if err := store.SaveRouteWithOptions("agent-1", "tcp-route", "127.0.0.1:8080", 0, true, nil, ProtocolTCP, 0, 0); err != nil {
		t.Fatalf("save tcp route: %v", err)
	}
	if err := store.SaveRouteWithOptions("agent-2", "tcp-route", "127.0.0.1:8081", 0, true, nil, ProtocolTCP, 0, 0); err != nil {
		t.Fatalf("save second tcp route: %v", err)
	}
	if err := store.SaveRouteWithOptions("agent-3", "udp-route", "127.0.0.1:5353", 0, true, nil, ProtocolUDP, 10, 1200); err != nil {
		t.Fatalf("save udp route: %v", err)
	}

	if err := store.UpdateRouteRuntime("agent-1", "tcp-route", 34201, ""); err != nil {
		t.Fatalf("assign first tcp route: %v", err)
	}
	if err := store.UpdateRouteRuntime("agent-2", "tcp-route", 34201, ""); err == nil {
		t.Fatal("expected duplicate tcp assigned port to be rejected")
	}
	if err := store.UpdateRouteRuntime("agent-3", "udp-route", 34201, ""); err != nil {
		t.Fatalf("expected udp route to reuse same port number: %v", err)
	}
}

func TestManagedStoreNormalizesTargetAddrFromPortOnly(t *testing.T) {
	db := newManagedTestDB(t)
	store := NewManagedStore(db)

	if err := store.SaveRoute("agent-1", "mysql", "3306", 18080, true, nil); err != nil {
		t.Fatalf("save route with port-only target: %v", err)
	}

	routes, err := store.ListRoutes()
	if err != nil {
		t.Fatalf("list routes: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("unexpected route count: got %d want 1", len(routes))
	}
	if routes[0].TargetAddr != "127.0.0.1:3306" {
		t.Fatalf("unexpected normalized target address: got %q want %q", routes[0].TargetAddr, "127.0.0.1:3306")
	}
}

func TestManagedStoreNormalizesTargetAddrFromColonPort(t *testing.T) {
	db := newManagedTestDB(t)
	store := NewManagedStore(db)

	if err := store.SaveRoute("agent-1", "mysql", ":3306", 18080, true, nil); err != nil {
		t.Fatalf("save route with colon-port target: %v", err)
	}

	routes, err := store.ListRoutes()
	if err != nil {
		t.Fatalf("list routes: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("unexpected route count: got %d want 1", len(routes))
	}
	if routes[0].TargetAddr != "127.0.0.1:3306" {
		t.Fatalf("unexpected normalized target address: got %q want %q", routes[0].TargetAddr, "127.0.0.1:3306")
	}
}

func TestManagedStoreRejectsInvalidTargetAddr(t *testing.T) {
	db := newManagedTestDB(t)
	store := NewManagedStore(db)

	if err := store.SaveRoute("agent-1", "mysql", "localhost", 18080, true, nil); err == nil {
		t.Fatal("expected invalid target address to be rejected")
	}
}

func TestIsUniqueConstraintError(t *testing.T) {
	if !isUniqueConstraintError(errors.New("constraint failed: UNIQUE constraint failed: tunnel_routes.client_name, tunnel_routes.name (2067)")) {
		t.Fatal("expected sqlite unique constraint error to be detected")
	}
	if !isUniqueConstraintError(errors.New("duplicate key value violates unique constraint")) {
		t.Fatal("expected duplicate key error to be detected")
	}
	if isUniqueConstraintError(errors.New("some other database error")) {
		t.Fatal("did not expect unrelated error to be treated as unique constraint")
	}
}

func TestManagedTunnelServerRejectsInvalidAutoPortRange(t *testing.T) {
	db := newManagedTestDB(t)
	server := NewManagedServer(db, "127.0.0.1:0", "127.0.0.1", "secret-token")
	server.TLSConfig = &tls.Config{}
	server.AutoPortRangeStart = 35000
	server.AutoPortRangeEnd = 0

	if err := server.Start(context.Background()); err == nil {
		t.Fatal("expected invalid managed tunnel auto port range to be rejected")
	}
}
