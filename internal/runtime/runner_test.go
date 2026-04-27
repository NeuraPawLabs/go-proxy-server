package runtime

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeManager struct {
	mu           sync.Mutex
	calls        []string
	webStarted   chan struct{}
	webRelease   chan struct{}
	socksStarted chan struct{}
	socksRelease chan struct{}
	shutdownCnt  int
	httpErr      error
}

type fakeWebStarter struct {
	serverAllowInsecure bool
	clientAllowInsecure bool
}

func (m *fakeManager) StartWeb(port int) error {
	m.mu.Lock()
	m.calls = append(m.calls, "web")
	started := m.webStarted
	release := m.webRelease
	m.mu.Unlock()
	if started != nil {
		close(started)
	}
	if release != nil {
		<-release
	}
	return nil
}

func (m *fakeManager) StartSocks(port int, bindListen bool) error {
	m.mu.Lock()
	m.calls = append(m.calls, "socks")
	started := m.socksStarted
	release := m.socksRelease
	m.mu.Unlock()
	if started != nil {
		close(started)
	}
	if release != nil {
		<-release
	}
	return nil
}

func (m *fakeManager) StartHTTP(port int, bindListen bool) error {
	m.mu.Lock()
	m.calls = append(m.calls, "http")
	err := m.httpErr
	m.mu.Unlock()
	return err
}

func (m *fakeManager) StartTunnelServer(TunnelServerConfig) error {
	m.mu.Lock()
	m.calls = append(m.calls, "tunnel-server")
	m.mu.Unlock()
	return nil
}

func (m *fakeManager) StartTunnelClient(TunnelClientConfig) error {
	m.mu.Lock()
	m.calls = append(m.calls, "tunnel-client")
	m.mu.Unlock()
	return nil
}

func (m *fakeManager) snapshot() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.calls...)
}

func (m *fakeManager) Shutdown() error {
	m.mu.Lock()
	m.shutdownCnt++
	release := m.webRelease
	m.mu.Unlock()
	if release != nil {
		select {
		case <-release:
		default:
			close(release)
		}
	}
	return nil
}

func (m *fakeManager) shutdownCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.shutdownCnt
}

func (m *fakeWebStarter) StartWeb(port int) error { return nil }

func (m *fakeWebStarter) StartSocks(port int, bindListen bool) error { return nil }

func (m *fakeWebStarter) StartHTTP(port int, bindListen bool) error { return nil }

func (m *fakeWebStarter) StartTunnelServerRuntime(engine, listen, publicBind, token, cert, key string, allowInsecure bool, autoPortStart, autoPortEnd int) error {
	m.serverAllowInsecure = allowInsecure
	return nil
}

func (m *fakeWebStarter) StartTunnelClientRuntime(engine, server, token, client, ca, serverName string, insecureSkipVerify, allowInsecure bool) error {
	m.clientAllowInsecure = allowInsecure
	return nil
}

func (m *fakeWebStarter) ShutdownApplication() error { return nil }

func TestBuildStartupPlanIncludesEnabledServices(t *testing.T) {
	cfg := Config{
		Web:   WebConfig{Enabled: true, Port: 8080},
		Socks: ProxyConfig{Enabled: true, Port: 1080},
		TunnelServer: TunnelServerConfig{
			Enabled:       true,
			Engine:        "classic",
			Listen:        ":7000",
			PublicBind:    "0.0.0.0",
			Token:         "secret",
			AllowInsecure: true,
		},
	}

	plan := BuildStartupPlan(cfg)

	if len(plan) != 3 {
		t.Fatalf("plan length = %d, want 3", len(plan))
	}
	if plan[0].Name != "socks" || plan[1].Name != "tunnel-server" || plan[2].Name != "web" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestRunnerStartsEnabledServicesInStableOrder(t *testing.T) {
	cfg := Config{
		Web:   WebConfig{Enabled: true, Port: 8080},
		Socks: ProxyConfig{Enabled: true, Port: 1080},
		HTTP:  ProxyConfig{Enabled: true, Port: 8081},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager := &fakeManager{
		webStarted: make(chan struct{}),
		webRelease: make(chan struct{}),
	}
	runner := Runner{Manager: manager}
	errCh := make(chan error, 1)
	go func() {
		errCh <- runner.Start(ctx, cfg)
	}()

	<-manager.webStarted
	waitForCalls(t, manager, []string{"socks", "http", "web"})
	cancel()

	select {
	case err := <-errCh:
		if err != context.Canceled {
			t.Fatalf("Start error = %v, want %v", err, context.Canceled)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for runner to stop")
	}

	if got := manager.snapshot(); len(got) != 3 || got[0] != "socks" || got[1] != "http" || got[2] != "web" {
		t.Fatalf("calls = %v, want [socks http web]", got)
	}
	if got := manager.shutdownCount(); got != 1 {
		t.Fatalf("shutdown count = %d, want 1", got)
	}
}

func TestRunnerBlocksWithoutWebUntilContextCancel(t *testing.T) {
	cfg := Config{
		Socks: ProxyConfig{Enabled: true, Port: 1080},
		HTTP:  ProxyConfig{Enabled: true, Port: 8081},
	}

	ctx, cancel := context.WithCancel(context.Background())
	manager := &fakeManager{}
	runner := Runner{Manager: manager}
	errCh := make(chan error, 1)
	go func() {
		errCh <- runner.Start(ctx, cfg)
	}()

	waitForCalls(t, manager, []string{"socks", "http"})
	select {
	case err := <-errCh:
		t.Fatalf("runner returned early: %v", err)
	default:
	}

	cancel()

	select {
	case err := <-errCh:
		if err != context.Canceled {
			t.Fatalf("Start error = %v, want %v", err, context.Canceled)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for runner to stop")
	}

	if got := manager.shutdownCount(); got != 1 {
		t.Fatalf("shutdown count = %d, want 1", got)
	}
}

func TestRunnerStopsWebEnabledRuntimeOnContextCancel(t *testing.T) {
	cfg := Config{
		Web:   WebConfig{Enabled: true, Port: 8080},
		Socks: ProxyConfig{Enabled: true, Port: 1080},
	}

	ctx, cancel := context.WithCancel(context.Background())
	manager := &fakeManager{
		webStarted: make(chan struct{}),
		webRelease: make(chan struct{}),
	}
	runner := Runner{Manager: manager}
	errCh := make(chan error, 1)
	go func() {
		errCh <- runner.Start(ctx, cfg)
	}()

	<-manager.webStarted
	waitForCalls(t, manager, []string{"socks", "web"})
	cancel()

	select {
	case err := <-errCh:
		if err != context.Canceled {
			t.Fatalf("Start error = %v, want %v", err, context.Canceled)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for runner to stop")
	}

	if got := manager.shutdownCount(); got != 1 {
		t.Fatalf("shutdown count = %d, want 1", got)
	}
}

func TestRunnerRejectsInvalidTunnelConfigBeforeStartingManager(t *testing.T) {
	cfg := Config{
		TunnelServer: TunnelServerConfig{
			Enabled:       true,
			AllowInsecure: true,
		},
	}

	manager := &fakeManager{}
	runner := Runner{Manager: manager}
	err := runner.Start(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "tunnel token is required") {
		t.Fatalf("expected tunnel validation error, got %v", err)
	}
	if got := manager.snapshot(); len(got) != 0 {
		t.Fatalf("expected no manager calls on validation failure, got %v", got)
	}
	if got := manager.shutdownCount(); got != 0 {
		t.Fatalf("expected no shutdown on validation failure, got %d", got)
	}
}

func TestRunnerRejectsUnsupportedTunnelEngineBeforeStartingManager(t *testing.T) {
	cfg := Config{
		TunnelClient: TunnelClientConfig{
			Enabled: true,
			Engine:  "bogus",
			Server:  "127.0.0.1:7000",
			Token:   "secret",
			Client:  "client-a",
		},
	}

	manager := &fakeManager{}
	runner := Runner{Manager: manager}
	err := runner.Start(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "unsupported tunnel client engine") {
		t.Fatalf("expected tunnel engine validation error, got %v", err)
	}
	if got := manager.snapshot(); len(got) != 0 {
		t.Fatalf("expected no manager calls on validation failure, got %v", got)
	}
	if got := manager.shutdownCount(); got != 0 {
		t.Fatalf("expected no shutdown on validation failure, got %d", got)
	}
}

func TestRunnerShutsDownAfterPartialStartupFailure(t *testing.T) {
	cfg := Config{
		Socks: ProxyConfig{Enabled: true, Port: 1080},
		HTTP:  ProxyConfig{Enabled: true, Port: 8081},
		Web:   WebConfig{Enabled: true, Port: 8080},
	}

	manager := &fakeManager{
		httpErr:     context.Canceled,
		webStarted:  make(chan struct{}),
		webRelease:  make(chan struct{}),
		shutdownCnt: 0,
	}
	runner := Runner{Manager: manager}

	err := runner.Start(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("expected startup failure, got %v", err)
	}

	if got := manager.snapshot(); len(got) == 0 || got[0] != "socks" || got[1] != "http" {
		t.Fatalf("unexpected manager calls on failure: %v", got)
	}
	if got := manager.shutdownCount(); got != 1 {
		t.Fatalf("expected shutdown after partial startup failure, got %d", got)
	}
}

func TestRunnerShutsDownWhenContextCancelsDuringPreWebStartup(t *testing.T) {
	cfg := Config{
		Socks: ProxyConfig{Enabled: true, Port: 1080},
		HTTP:  ProxyConfig{Enabled: true, Port: 8081},
		Web:   WebConfig{Enabled: true, Port: 8080},
	}

	ctx, cancel := context.WithCancel(context.Background())
	manager := &fakeManager{
		socksStarted: make(chan struct{}),
		socksRelease: make(chan struct{}),
	}
	runner := Runner{Manager: manager}
	errCh := make(chan error, 1)
	go func() {
		errCh <- runner.Start(ctx, cfg)
	}()

	<-manager.socksStarted
	cancel()
	close(manager.socksRelease)

	select {
	case err := <-errCh:
		if err != context.Canceled {
			t.Fatalf("Start error = %v, want %v", err, context.Canceled)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for runner to stop")
	}

	if got := manager.snapshot(); len(got) != 1 || got[0] != "socks" {
		t.Fatalf("unexpected manager calls on cancellation: %v", got)
	}
	if got := manager.shutdownCount(); got != 1 {
		t.Fatalf("expected shutdown after cancellation during startup, got %d", got)
	}
}

func waitForCalls(t *testing.T, manager *fakeManager, want []string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := manager.snapshot()
		if len(got) == len(want) {
			match := true
			for i := range want {
				if got[i] != want[i] {
					match = false
					break
				}
			}
			if match {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for calls %v, got %v", want, manager.snapshot())
}

func TestWebManagerAdapterForwardsAllowInsecure(t *testing.T) {
	starter := &fakeWebStarter{}
	adapter := NewWebManagerAdapter(starter)

	if err := adapter.StartTunnelServer(TunnelServerConfig{AllowInsecure: true}); err != nil {
		t.Fatalf("StartTunnelServer: %v", err)
	}
	if err := adapter.StartTunnelClient(TunnelClientConfig{AllowInsecure: true}); err != nil {
		t.Fatalf("StartTunnelClient: %v", err)
	}

	if !starter.serverAllowInsecure {
		t.Fatal("server allowInsecure was not forwarded")
	}
	if !starter.clientAllowInsecure {
		t.Fatal("client allowInsecure was not forwarded")
	}
}
