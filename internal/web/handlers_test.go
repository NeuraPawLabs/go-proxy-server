package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/apeming/go-proxy-server/internal/activity"
	"github.com/apeming/go-proxy-server/internal/auth"
	"github.com/apeming/go-proxy-server/internal/config"
	"github.com/apeming/go-proxy-server/internal/models"
	"github.com/apeming/go-proxy-server/internal/proxy"
	"github.com/apeming/go-proxy-server/internal/tunnel"
)

func newTestManager(db *gorm.DB) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		db:             db,
		adminAuth:      newAdminAuthManager(),
		socksServer:    newProxyServer("socks5"),
		httpServer:     newProxyServer("http"),
		tunnelServers:  make(map[string]managedTunnelServerRuntime),
		tunnelCancels:  make(map[string]context.CancelFunc),
		tunnelErrors:   make(map[string]string),
		shutdownCtx:    ctx,
		shutdownCancel: cancel,
	}
}

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{},
		&models.Whitelist{},
		&models.SystemConfig{},
		&models.ProxyConfig{},
		&models.AuditLog{},
		&models.EventLog{},
		&models.TunnelClient{},
		&models.TunnelRoute{},
	); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	if err := models.EnsureTunnelConstraints(db); err != nil {
		t.Fatalf("apply tunnel constraints: %v", err)
	}
	return db
}

func useTestActivityRecorder(t *testing.T, db *gorm.DB) {
	t.Helper()

	activity.SetRecorder(activity.NewDBRecorder(db, 64))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := activity.Close(ctx); err != nil {
			t.Fatalf("close activity recorder: %v", err)
		}
	})
}

func waitForModelCount(t *testing.T, db *gorm.DB, model any, want int64) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var got int64
		if err := db.Model(model).Count(&got).Error; err != nil {
			t.Fatalf("count %T: %v", model, err)
		}
		if got >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	var got int64
	if err := db.Model(model).Count(&got).Error; err != nil {
		t.Fatalf("count %T after wait: %v", model, err)
	}
	t.Fatalf("timeout waiting for %T count: got %d want at least %d", model, got, want)
}

type fakeGeetestValidator struct {
	valid bool
	err   error
	id    string
	key   string
	reqs  []geetestValidateRequest
}

func (f *fakeGeetestValidator) validate(req geetestValidateRequest) (bool, error) {
	f.reqs = append(f.reqs, req)
	return f.valid, f.err
}

func TestHandleUsersHidesPasswordHashes(t *testing.T) {
	db := newTestDB(t)
	wm := &Manager{db: db}

	user := models.User{
		IP:       "127.0.0.1",
		Username: "alice",
		Password: []byte("secret"),
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	rec := httptest.NewRecorder()
	wm.handleUsers(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	if strings.Contains(body, "Password") || strings.Contains(body, "c2VjcmV0") {
		t.Fatalf("response leaked password material: %s", body)
	}

	var payload []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("unexpected user count: got %d want 1", len(payload))
	}
	if _, ok := payload[0]["password"]; ok {
		t.Fatalf("response still contains password field: %v", payload[0])
	}
}

func TestHandleUsersReturnsSortedUsers(t *testing.T) {
	db := newTestDB(t)
	wm := &Manager{db: db}

	users := []models.User{
		{IP: "127.0.0.2", Username: "zoe", Password: []byte("secret")},
		{IP: "127.0.0.1", Username: "alice", Password: []byte("secret")},
	}
	for _, user := range users {
		if err := db.Create(&user).Error; err != nil {
			t.Fatalf("create user: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	rec := httptest.NewRecorder()
	wm.handleUsers(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d", rec.Code, http.StatusOK)
	}

	var payload []userResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload) != 2 {
		t.Fatalf("unexpected user count: got %d want 2", len(payload))
	}
	if payload[0].Username != "alice" || payload[1].Username != "zoe" {
		t.Fatalf("users not sorted by username: %+v", payload)
	}
}

func TestHandleConfigRecreatesLimiters(t *testing.T) {
	db := newTestDB(t)
	wm := &Manager{db: db}

	oldSocks := proxy.GetSOCKS5Limiter()
	oldHTTP := proxy.GetHTTPLimiter()

	body := bytes.NewBufferString(`{"limiter":{"maxConcurrentConnections":321,"maxConcurrentConnectionsPerIP":32}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/config", body)
	rec := httptest.NewRecorder()
	wm.handleConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	cfg := config.GetLimiterConfig()
	if cfg.MaxConcurrentConnections != 321 || cfg.MaxConcurrentConnectionsPerIP != 32 {
		t.Fatalf("unexpected limiter config: %+v", cfg)
	}
	if proxy.GetSOCKS5Limiter() == oldSocks {
		t.Fatal("SOCKS5 limiter was not recreated")
	}
	if proxy.GetHTTPLimiter() == oldHTTP {
		t.Fatal("HTTP limiter was not recreated")
	}
}

func TestHandleConfigPreservesExtendedTimeoutFields(t *testing.T) {
	db := newTestDB(t)
	wm := &Manager{db: db}

	initial := config.TimeoutConfig{
		Connect:          30 * time.Second,
		IdleRead:         300 * time.Second,
		IdleWrite:        120 * time.Second,
		MaxConnectionAge: 2 * time.Hour,
		CleanupTimeout:   9 * time.Second,
	}
	if err := config.UpdateTimeout(db, initial); err != nil {
		t.Fatalf("seed timeout: %v", err)
	}

	body := bytes.NewBufferString(`{"timeout":{"connect":15,"idleRead":25,"idleWrite":35}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/config", body)
	rec := httptest.NewRecorder()
	wm.handleConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	got := config.GetTimeout()
	if got.Connect != 15*time.Second || got.IdleRead != 25*time.Second || got.IdleWrite != 35*time.Second {
		t.Fatalf("unexpected short timeout fields: %+v", got)
	}
	if got.MaxConnectionAge != initial.MaxConnectionAge || got.CleanupTimeout != initial.CleanupTimeout {
		t.Fatalf("extended timeout fields changed unexpectedly: got %+v want max_age=%s cleanup=%s", got, initial.MaxConnectionAge, initial.CleanupTimeout)
	}

	if err := config.LoadTimeoutFromDB(db); err != nil {
		t.Fatalf("reload timeout from db: %v", err)
	}
	reloaded := config.GetTimeout()
	if reloaded != got {
		t.Fatalf("db timeout mismatch: got %+v want %+v", reloaded, got)
	}
}

func TestHandleConfigRejectsInvalidTimeout(t *testing.T) {
	db := newTestDB(t)
	wm := &Manager{db: db}

	body := bytes.NewBufferString(`{"timeout":{"connect":0,"idleRead":25,"idleWrite":35}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/config", body)
	rec := httptest.NewRecorder()
	wm.handleConfig(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: got %d want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Connect timeout must be between 1 and 300 seconds") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestHandleWhitelistReturnsSortedIPs(t *testing.T) {
	db := newTestDB(t)
	wm := &Manager{db: db}

	items := []models.Whitelist{
		{IP: "10.0.0.2"},
		{IP: "10.0.0.1"},
	}
	for _, item := range items {
		if err := db.Create(&item).Error; err != nil {
			t.Fatalf("create whitelist item: %v", err)
		}
	}
	if err := auth.LoadWhitelistFromDB(db); err != nil {
		t.Fatalf("load whitelist: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/whitelist", nil)
	rec := httptest.NewRecorder()
	wm.handleWhitelist(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d", rec.Code, http.StatusOK)
	}

	var payload []string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload) != 2 {
		t.Fatalf("unexpected whitelist count: got %d want 2", len(payload))
	}
	if payload[0] != "10.0.0.1" || payload[1] != "10.0.0.2" {
		t.Fatalf("whitelist not sorted: %+v", payload)
	}
}

func TestHandleTunnelRoutesCRUD(t *testing.T) {
	db := newTestDB(t)
	wm := &Manager{db: db}

	createBody := bytes.NewBufferString(`{"clientName":"node-b","name":"mysql","targetAddr":"127.0.0.1:3306","publicPort":33060,"ipWhitelist":["203.0.113.10","10.0.0.0/24"],"enabled":true}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/tunnel/routes", createBody)
	createRec := httptest.NewRecorder()
	wm.handleTunnelRoutes(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("unexpected create status: got %d want %d body=%s", createRec.Code, http.StatusOK, createRec.Body.String())
	}

	secondBody := bytes.NewBufferString(`{"clientName":"node-a","name":"redis","targetAddr":"127.0.0.1:6379","publicPort":16379,"enabled":false}`)
	secondReq := httptest.NewRequest(http.MethodPost, "/api/tunnel/routes", secondBody)
	secondRec := httptest.NewRecorder()
	wm.handleTunnelRoutes(secondRec, secondReq)
	if secondRec.Code != http.StatusOK {
		t.Fatalf("unexpected second create status: got %d want %d body=%s", secondRec.Code, http.StatusOK, secondRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/tunnel/routes", nil)
	listRec := httptest.NewRecorder()
	wm.handleTunnelRoutes(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("unexpected list status: got %d want %d body=%s", listRec.Code, http.StatusOK, listRec.Body.String())
	}

	var routes []tunnelRouteResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &routes); err != nil {
		t.Fatalf("decode routes: %v", err)
	}
	if len(routes) != 2 {
		t.Fatalf("unexpected route count: got %d want 2", len(routes))
	}
	if routes[0].ClientName != "node-a" || routes[0].Name != "redis" {
		t.Fatalf("routes not sorted by client/name: %+v", routes)
	}
	if len(routes[1].IPWhitelist) != 2 || routes[1].IPWhitelist[0] != "10.0.0.0/24" || routes[1].IPWhitelist[1] != "203.0.113.10" {
		t.Fatalf("unexpected route whitelist: %+v", routes[1].IPWhitelist)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/tunnel/routes", bytes.NewBufferString(`{"clientName":"node-a","name":"redis"}`))
	deleteRec := httptest.NewRecorder()
	wm.handleTunnelRoutes(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("unexpected delete status: got %d want %d body=%s", deleteRec.Code, http.StatusOK, deleteRec.Body.String())
	}
}

func TestHandleProxyStartRejectsInvalidType(t *testing.T) {
	db := newTestDB(t)
	wm := &Manager{db: db}

	body := bytes.NewBufferString(`{"type":"ftp","port":1080,"bindListen":false}`)
	req := httptest.NewRequest(http.MethodPost, "/api/proxy/start", body)
	rec := httptest.NewRecorder()
	wm.handleProxyStart(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: got %d want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Invalid proxy type: ftp") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestHandleTunnelServerStartStop(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)

	originalGetTunnelDataDir := getTunnelDataDir
	tempDir := t.TempDir()
	getTunnelDataDir = func() (string, error) {
		return tempDir, nil
	}
	defer func() {
		getTunnelDataDir = originalGetTunnelDataDir
	}()

	generateReq := httptest.NewRequest(http.MethodPost, "/api/tunnel/server/certificates/generate", bytes.NewBufferString(`{"commonName":"tunnel.example.com","hosts":["tunnel.example.com","127.0.0.1"],"validDays":30}`))
	generateRec := httptest.NewRecorder()
	wm.handleTunnelServerGenerateCertificates(generateRec, generateReq)
	if generateRec.Code != http.StatusOK {
		t.Fatalf("unexpected generate status: got %d want %d body=%s", generateRec.Code, http.StatusOK, generateRec.Body.String())
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/api/tunnel/server", nil)
	statusRec := httptest.NewRecorder()
	wm.handleTunnelServerStatus(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("unexpected initial tunnel server status: got %d want %d body=%s", statusRec.Code, http.StatusOK, statusRec.Body.String())
	}

	startReq := httptest.NewRequest(http.MethodPost, "/api/tunnel/server/start", bytes.NewBufferString(`{"listenAddr":"127.0.0.1:0","publicBind":"127.0.0.1","token":"secret-token","autoStart":true,"autoPortRangeStart":32000,"autoPortRangeEnd":32010}`))
	startRec := httptest.NewRecorder()
	wm.handleTunnelServerStart(startRec, startReq)
	if startRec.Code != http.StatusOK {
		t.Fatalf("unexpected tunnel server start status: got %d want %d body=%s", startRec.Code, http.StatusOK, startRec.Body.String())
	}

	var started tunnelServerStatusResponse
	if err := json.Unmarshal(startRec.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode start response: %v", err)
	}
	if !started.Classic.Running || started.Classic.ActualListenAddr == "" {
		t.Fatalf("unexpected started tunnel server status: %+v", started)
	}
	if !started.Classic.Config.AutoStart || started.Classic.Config.AutoPortRangeStart != 32000 || started.Classic.Config.AutoPortRangeEnd != 32010 {
		t.Fatalf("expected auto-start tunnel config in status: %+v", started.Classic.Config)
	}

	stopReq := httptest.NewRequest(http.MethodPost, "/api/tunnel/server/stop", nil)
	stopRec := httptest.NewRecorder()
	wm.handleTunnelServerStop(stopRec, stopReq)
	if stopRec.Code != http.StatusOK {
		t.Fatalf("unexpected tunnel server stop status: got %d want %d body=%s", stopRec.Code, http.StatusOK, stopRec.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		statusRec = httptest.NewRecorder()
		wm.handleTunnelServerStatus(statusRec, statusReq)
		if statusRec.Code != http.StatusOK {
			t.Fatalf("unexpected tunnel server status after stop: got %d want %d body=%s", statusRec.Code, http.StatusOK, statusRec.Body.String())
		}

		var stopped tunnelServerStatusResponse
		if err := json.Unmarshal(statusRec.Body.Bytes(), &stopped); err != nil {
			t.Fatalf("decode stop response: %v", err)
		}
		if !stopped.Classic.Running {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("timeout waiting for tunnel server to stop")
}

func TestHandleTunnelServerGenerateCertificatesAndDownload(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)

	originalGetTunnelDataDir := getTunnelDataDir
	tempDir := t.TempDir()
	getTunnelDataDir = func() (string, error) {
		return tempDir, nil
	}
	defer func() {
		getTunnelDataDir = originalGetTunnelDataDir
	}()

	generateReq := httptest.NewRequest(http.MethodPost, "/api/tunnel/server/certificates/generate", bytes.NewBufferString(`{"commonName":"tunnel.example.com","hosts":["tunnel.example.com","127.0.0.1"],"validDays":30}`))
	generateRec := httptest.NewRecorder()
	wm.handleTunnelServerGenerateCertificates(generateRec, generateReq)
	if generateRec.Code != http.StatusOK {
		t.Fatalf("unexpected generate status: got %d want %d body=%s", generateRec.Code, http.StatusOK, generateRec.Body.String())
	}

	var status tunnelServerStatusResponse
	if err := json.Unmarshal(generateRec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode generate response: %v", err)
	}
	if !status.Certificates.Ready || status.Certificates.Source != tunnelCertificateSourceGenerated {
		t.Fatalf("unexpected certificate state after generate: %+v", status.Certificates)
	}

	downloadReq := httptest.NewRequest(http.MethodGet, "/api/tunnel/server/files/client-ca", nil)
	downloadRec := httptest.NewRecorder()
	wm.handleTunnelServerDownloadClientCA(downloadRec, downloadReq)
	if downloadRec.Code != http.StatusOK {
		t.Fatalf("unexpected client CA download status: got %d want %d body=%s", downloadRec.Code, http.StatusOK, downloadRec.Body.String())
	}
	if !strings.Contains(downloadRec.Header().Get("Content-Disposition"), "ca.pem") {
		t.Fatalf("unexpected content disposition: %s", downloadRec.Header().Get("Content-Disposition"))
	}
	if !strings.Contains(downloadRec.Body.String(), "BEGIN CERTIFICATE") {
		t.Fatalf("unexpected download body: %s", downloadRec.Body.String())
	}

	startReq := httptest.NewRequest(http.MethodPost, "/api/tunnel/server/start", bytes.NewBufferString(`{"listenAddr":"127.0.0.1:0","publicBind":"127.0.0.1","token":"secret-token","autoStart":false,"autoPortRangeStart":32020,"autoPortRangeEnd":32030}`))
	startRec := httptest.NewRecorder()
	wm.handleTunnelServerStart(startRec, startReq)
	if startRec.Code != http.StatusOK {
		t.Fatalf("unexpected TLS tunnel server start status: got %d want %d body=%s", startRec.Code, http.StatusOK, startRec.Body.String())
	}
	if err := wm.stopManagedTunnelServer(""); err != nil {
		t.Fatalf("stop managed tunnel server: %v", err)
	}
}

func TestHandleTunnelServerUploadCertificates(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)

	originalGetTunnelDataDir := getTunnelDataDir
	tempDir := t.TempDir()
	getTunnelDataDir = func() (string, error) {
		return tempDir, nil
	}
	defer func() {
		getTunnelDataDir = originalGetTunnelDataDir
	}()

	generated, err := generateTunnelServerCertificates("upload.example.com", []string{"upload.example.com"}, 30)
	if err != nil {
		t.Fatalf("generate certificates: %v", err)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	addMultipartFile(t, writer, "serverCert", "upload.crt", generated.ServerCertPEM)
	addMultipartFile(t, writer, "serverKey", "upload.key", generated.ServerKeyPEM)
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/tunnel/server/certificates/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	wm.handleTunnelServerUploadCertificates(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected upload status: got %d want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var status tunnelServerStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if status.Certificates.Source != tunnelCertificateSourceUploaded || !status.Certificates.Ready {
		t.Fatalf("unexpected certificate state after upload: %+v", status.Certificates)
	}
	if !status.Certificates.CanDownloadClientCA {
		t.Fatalf("expected fallback client CA download to be available: %+v", status.Certificates)
	}

	certPath, keyPath, caPath, err := managedTunnelServerTLSPaths()
	if err != nil {
		t.Fatalf("resolve managed tls paths: %v", err)
	}
	for _, path := range []string{certPath, keyPath, caPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected managed certificate file %s: %v", path, err)
		}
	}
}

func TestHandleTunnelServerConfigSaveAndAutoStart(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)

	originalGetTunnelDataDir := getTunnelDataDir
	tempDir := t.TempDir()
	getTunnelDataDir = func() (string, error) {
		return tempDir, nil
	}
	defer func() {
		getTunnelDataDir = originalGetTunnelDataDir
	}()

	generateReq := httptest.NewRequest(http.MethodPost, "/api/tunnel/server/certificates/generate", bytes.NewBufferString(`{"commonName":"auto.example.com","hosts":["auto.example.com"],"validDays":30}`))
	generateRec := httptest.NewRecorder()
	wm.handleTunnelServerGenerateCertificates(generateRec, generateReq)
	if generateRec.Code != http.StatusOK {
		t.Fatalf("unexpected generate status: got %d want %d body=%s", generateRec.Code, http.StatusOK, generateRec.Body.String())
	}

	saveReq := httptest.NewRequest(http.MethodPost, "/api/tunnel/server/config", bytes.NewBufferString(`{"listenAddr":"127.0.0.1:0","publicBind":"127.0.0.1","clientEndpoint":"tunnel.example.com:7443","token":"auto-token","autoStart":true,"autoPortRangeStart":33000,"autoPortRangeEnd":33099}`))
	saveRec := httptest.NewRecorder()
	wm.handleTunnelServerConfig(saveRec, saveReq)
	if saveRec.Code != http.StatusOK {
		t.Fatalf("unexpected tunnel config save status: got %d want %d body=%s", saveRec.Code, http.StatusOK, saveRec.Body.String())
	}

	cfg, err := wm.loadManagedTunnelServerConfig(tunnel.EngineClassic)
	if err != nil {
		t.Fatalf("load saved tunnel config: %v", err)
	}
	if !cfg.AutoStart || cfg.Token != "auto-token" || cfg.ClientEndpoint != "tunnel.example.com:7443" || cfg.AutoPortRangeStart != 33000 || cfg.AutoPortRangeEnd != 33099 {
		t.Fatalf("unexpected saved tunnel config: %+v", cfg)
	}

	status, err := wm.getManagedTunnelServerStatus()
	if err != nil {
		t.Fatalf("get managed tunnel server status: %v", err)
	}
	if status.Classic.Config.AutoPortRangeStart != 33000 || status.Classic.Config.AutoPortRangeEnd != 33099 {
		t.Fatalf("unexpected tunnel server status config: %+v", status.Classic.Config)
	}

	wm.StartConfiguredTunnelServer()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status, err := wm.getManagedTunnelServerStatus()
		if err != nil {
			t.Fatalf("get managed tunnel server status: %v", err)
		}
		if status.Classic.Running && status.Classic.ActualListenAddr != "" {
			if err := wm.stopManagedTunnelServer(""); err != nil {
				t.Fatalf("stop auto-started tunnel server: %v", err)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("timeout waiting for auto-started tunnel server")
}

func TestHandleTunnelServerConfigRejectsInvalidAutoPortRange(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)

	req := httptest.NewRequest(http.MethodPost, "/api/tunnel/server/config", bytes.NewBufferString(`{"listenAddr":"127.0.0.1:0","publicBind":"127.0.0.1","token":"auto-token","autoPortRangeStart":33000}`))
	rec := httptest.NewRecorder()
	wm.handleTunnelServerConfig(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unexpected tunnel config save status: got %d want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "auto port range start and end must both be set") {
		t.Fatalf("unexpected response body: %s", rec.Body.String())
	}
}

func TestLoadManagedTunnelServerConfigsMigratesLegacyConfig(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)

	legacyRaw := `{"engine":"quic","listenAddr":"127.0.0.1:7443","publicBind":"127.0.0.1","clientEndpoint":"edge.example.com:7443","token":"legacy-token","autoStart":true,"autoPortRangeStart":32000,"autoPortRangeEnd":32099}`
	if err := config.SetSystemConfig(db, config.KeyTunnelServerConfig, legacyRaw); err != nil {
		t.Fatalf("save legacy config: %v", err)
	}

	cfgs, err := wm.loadManagedTunnelServerConfigs()
	if err != nil {
		t.Fatalf("load tunnel server configs: %v", err)
	}
	if cfgs.QUIC.Token != "legacy-token" || !cfgs.QUIC.AutoStart {
		t.Fatalf("expected legacy config to migrate into quic engine: %+v", cfgs.QUIC)
	}
	if cfgs.QUIC.ClientEndpoint != "edge.example.com:7443" || cfgs.QUIC.AutoPortRangeStart != 32000 || cfgs.QUIC.AutoPortRangeEnd != 32099 {
		t.Fatalf("unexpected migrated quic config: %+v", cfgs.QUIC)
	}
	if cfgs.Classic.Engine != tunnel.EngineClassic || cfgs.Classic.ListenAddr == "" {
		t.Fatalf("expected classic config defaults to remain intact: %+v", cfgs.Classic)
	}
}

func TestStopManagedTunnelServerRejectsUnknownEngine(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)

	err := wm.stopManagedTunnelServer("udp-only")
	if err == nil {
		t.Fatal("expected unknown engine to return error")
	}
	if !strings.Contains(err.Error(), "unknown tunnel engine") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleTunnelClientConfigSave(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)

	req := httptest.NewRequest(http.MethodPost, "/api/tunnel/client/config", bytes.NewBufferString(`{"serverAddr":"tunnel.example.com:7443","clientName":"edge-node","token":"secret-token","serverName":"tunnel.example.com","autoStart":true}`))
	rec := httptest.NewRecorder()
	wm.handleTunnelClientConfig(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected tunnel client config status: got %d want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	cfg, err := wm.loadManagedTunnelClientConfig()
	if err != nil {
		t.Fatalf("load managed tunnel client config: %v", err)
	}
	if cfg.ServerAddr != "tunnel.example.com:7443" || cfg.ClientName != "edge-node" || cfg.Token != "secret-token" || cfg.ServerName != "tunnel.example.com" || !cfg.AutoStart {
		t.Fatalf("unexpected managed tunnel client config: %+v", cfg)
	}
}

func TestHandleTunnelClientCAUpload(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)

	originalGetTunnelDataDir := getTunnelDataDir
	tempDir := t.TempDir()
	getTunnelDataDir = func() (string, error) {
		return tempDir, nil
	}
	defer func() {
		getTunnelDataDir = originalGetTunnelDataDir
	}()

	generated, err := generateTunnelServerCertificates("upload.example.com", []string{"upload.example.com"}, 30)
	if err != nil {
		t.Fatalf("generate certificates: %v", err)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	addMultipartFile(t, writer, "clientCa", "client-ca.pem", generated.ClientCAPEM)
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/tunnel/client/ca", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	wm.handleTunnelClientCAUpload(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected client ca upload status: got %d want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var status managedTunnelClientStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if !status.Certificates.Ready || status.Certificates.Source != tunnelClientCertificateSourceUploaded {
		t.Fatalf("unexpected client certificate state: %+v", status.Certificates)
	}
	if status.Certificates.CAName != "client-ca.pem" {
		t.Fatalf("unexpected client certificate name: %+v", status.Certificates)
	}

	caPath, err := managedTunnelClientTLSCAPath()
	if err != nil {
		t.Fatalf("resolve managed client ca path: %v", err)
	}
	if _, err := os.Stat(caPath); err != nil {
		t.Fatalf("expected uploaded client ca file %s: %v", caPath, err)
	}
}

func TestHandleTunnelClientConfigRejectsInvalidSettings(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)

	testCases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "invalid server addr",
			body: `{"serverAddr":"tunnel.example.com","clientName":"edge-node","token":"secret-token","autoStart":true}`,
			want: "tunnel server address must be in host:port format",
		},
		{
			name: "invalid client name",
			body: `{"serverAddr":"tunnel.example.com:7443","clientName":"edge node","token":"secret-token","autoStart":true}`,
			want: "tunnel client name may only contain letters, numbers, dot, dash, and underscore",
		},
		{
			name: "skip hostname verify unsupported",
			body: `{"serverAddr":"tunnel.example.com:7443","clientName":"edge-node","token":"secret-token","insecureSkipVerify":true,"autoStart":true}`,
			want: "managed tunnel client does not support skipping hostname verification",
		},
		{
			name: "plaintext mode unsupported",
			body: `{"serverAddr":"tunnel.example.com:7443","clientName":"edge-node","token":"secret-token","allowInsecure":true,"autoStart":true}`,
			want: "managed tunnel client does not support insecure plaintext mode",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/tunnel/client/config", bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()
			wm.handleTunnelClientConfig(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("unexpected tunnel client config status: got %d want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("unexpected error body: %s", rec.Body.String())
			}
		})
	}
}

func TestHandleTunnelClientStartRequiresUploadedCA(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)

	originalGetTunnelDataDir := getTunnelDataDir
	tempDir := t.TempDir()
	getTunnelDataDir = func() (string, error) {
		return tempDir, nil
	}
	defer func() {
		getTunnelDataDir = originalGetTunnelDataDir
	}()

	req := httptest.NewRequest(http.MethodPost, "/api/tunnel/client/start", bytes.NewBufferString(`{"serverAddr":"tunnel.example.com:7443","clientName":"edge-node","token":"secret-token","autoStart":true}`))
	rec := httptest.NewRecorder()
	wm.handleTunnelClientStart(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unexpected tunnel client start status: got %d want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "managed tunnel client requires uploaded CA file") {
		t.Fatalf("unexpected error body: %s", rec.Body.String())
	}
}

func TestHandleTunnelClientStartStopAndAutoStart(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)

	originalGetTunnelDataDir := getTunnelDataDir
	tempDir := t.TempDir()
	getTunnelDataDir = func() (string, error) {
		return tempDir, nil
	}
	defer func() {
		getTunnelDataDir = originalGetTunnelDataDir
	}()

	generateReq := httptest.NewRequest(http.MethodPost, "/api/tunnel/server/certificates/generate", bytes.NewBufferString(`{"commonName":"tunnel.example.com","hosts":["tunnel.example.com","127.0.0.1"],"validDays":30}`))
	generateRec := httptest.NewRecorder()
	wm.handleTunnelServerGenerateCertificates(generateRec, generateReq)
	if generateRec.Code != http.StatusOK {
		t.Fatalf("unexpected generate status: got %d want %d body=%s", generateRec.Code, http.StatusOK, generateRec.Body.String())
	}

	startServerReq := httptest.NewRequest(http.MethodPost, "/api/tunnel/server/start", bytes.NewBufferString(`{"listenAddr":"127.0.0.1:0","publicBind":"127.0.0.1","token":"secret-token"}`))
	startServerRec := httptest.NewRecorder()
	wm.handleTunnelServerStart(startServerRec, startServerReq)
	if startServerRec.Code != http.StatusOK {
		t.Fatalf("unexpected tunnel server start status: got %d want %d body=%s", startServerRec.Code, http.StatusOK, startServerRec.Body.String())
	}

	var serverStatus tunnelServerStatusResponse
	if err := json.Unmarshal(startServerRec.Body.Bytes(), &serverStatus); err != nil {
		t.Fatalf("decode server status: %v", err)
	}
	if serverStatus.Classic.ActualListenAddr == "" {
		t.Fatalf("expected actual listen address: %+v", serverStatus)
	}

	if err := wm.saveTunnelRoute("agent-1", "mysql", "3306", 0, true, nil); err != nil {
		t.Fatalf("seed managed tunnel route: %v", err)
	}

	startClientReq := httptest.NewRequest(http.MethodPost, "/api/tunnel/client/start", bytes.NewBufferString(fmt.Sprintf(`{"serverAddr":%q,"clientName":"agent-1","token":"secret-token","useManagedServerCa":true,"autoStart":true}`, serverStatus.Classic.ActualListenAddr)))
	startClientRec := httptest.NewRecorder()
	wm.handleTunnelClientStart(startClientRec, startClientReq)
	if startClientRec.Code != http.StatusOK {
		t.Fatalf("unexpected tunnel client start status: got %d want %d body=%s", startClientRec.Code, http.StatusOK, startClientRec.Body.String())
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		status, err := wm.getManagedTunnelClientStatus()
		if err != nil {
			t.Fatalf("get managed tunnel client status: %v", err)
		}
		if status.Running && status.Connected && len(status.Routes) == 1 && status.Routes[0].Name == "mysql" {
			goto verifyAutoStart
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timeout waiting for managed tunnel client to connect")

verifyAutoStart:
	if err := wm.stopManagedTunnelClient(); err != nil {
		t.Fatalf("stop managed tunnel client: %v", err)
	}

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status, err := wm.getManagedTunnelClientStatus()
		if err != nil {
			t.Fatalf("get managed tunnel client status after stop: %v", err)
		}
		if !status.Running {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	cfg, err := wm.loadManagedTunnelClientConfig()
	if err != nil {
		t.Fatalf("reload managed tunnel client config: %v", err)
	}
	if !cfg.AutoStart || !cfg.UseManagedServerCA {
		t.Fatalf("expected saved client config for auto-start: %+v", cfg)
	}

	wm.StartConfiguredTunnelClient()

	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		status, err := wm.getManagedTunnelClientStatus()
		if err != nil {
			t.Fatalf("get managed tunnel client status after auto-start: %v", err)
		}
		if status.Running && status.Connected {
			if err := wm.stopManagedTunnelClient(); err != nil {
				t.Fatalf("stop auto-started tunnel client: %v", err)
			}
			if err := wm.stopManagedTunnelServer(""); err != nil {
				t.Fatalf("stop managed tunnel server: %v", err)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("timeout waiting for auto-started managed tunnel client")
}

func TestHandleTunnelRoutesNormalizesPortOnlyTargetAddr(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)

	req := httptest.NewRequest(http.MethodPost, "/api/tunnel/routes", bytes.NewBufferString(`{"clientName":"node-a","name":"mysql","targetAddr":"3306","publicPort":33060,"enabled":true}`))
	rec := httptest.NewRecorder()
	wm.handleTunnelRoutes(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected route save status: got %d want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/tunnel/routes", nil)
	listRec := httptest.NewRecorder()
	wm.handleTunnelRoutes(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("unexpected route list status: got %d want %d body=%s", listRec.Code, http.StatusOK, listRec.Body.String())
	}

	var routes []tunnelRouteResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &routes); err != nil {
		t.Fatalf("decode route list response: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("unexpected route count: got %d want 1", len(routes))
	}
	if routes[0].TargetAddr != "127.0.0.1:3306" {
		t.Fatalf("unexpected normalized target address: got %q want %q", routes[0].TargetAddr, "127.0.0.1:3306")
	}
}

func TestHandleTunnelClientDelete(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)

	if err := db.Create(&models.TunnelClient{Name: "node-offline", Connected: false}).Error; err != nil {
		t.Fatalf("seed tunnel client: %v", err)
	}
	if err := db.Create(&models.TunnelRoute{ClientName: "node-offline", Name: "mysql", TargetAddr: "127.0.0.1:3306", PublicPort: 33060, Enabled: true}).Error; err != nil {
		t.Fatalf("seed tunnel route: %v", err)
	}
	if err := db.Create(&models.TunnelClient{Name: "node-online", Connected: true}).Error; err != nil {
		t.Fatalf("seed connected tunnel client: %v", err)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/tunnel/clients", bytes.NewBufferString(`{"clientName":"node-online"}`))
	deleteRec := httptest.NewRecorder()
	wm.handleTunnelClients(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusBadRequest {
		t.Fatalf("unexpected connected delete status: got %d want %d body=%s", deleteRec.Code, http.StatusBadRequest, deleteRec.Body.String())
	}

	deleteReq = httptest.NewRequest(http.MethodDelete, "/api/tunnel/clients", bytes.NewBufferString(`{"clientName":"node-offline"}`))
	deleteRec = httptest.NewRecorder()
	wm.handleTunnelClients(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("unexpected offline delete status: got %d want %d body=%s", deleteRec.Code, http.StatusOK, deleteRec.Body.String())
	}

	var clientCount int64
	if err := db.Model(&models.TunnelClient{}).Where("name = ?", "node-offline").Count(&clientCount).Error; err != nil {
		t.Fatalf("count deleted tunnel client: %v", err)
	}
	if clientCount != 0 {
		t.Fatalf("expected tunnel client to be deleted, got count=%d", clientCount)
	}

	var routeCount int64
	if err := db.Model(&models.TunnelRoute{}).Where("client_name = ?", "node-offline").Count(&routeCount).Error; err != nil {
		t.Fatalf("count deleted tunnel client routes: %v", err)
	}
	if routeCount != 0 {
		t.Fatalf("expected tunnel client routes to be deleted, got count=%d", routeCount)
	}
}

func TestHandleTunnelSessionsEmpty(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)

	req := httptest.NewRequest(http.MethodGet, "/api/tunnel/sessions?clientName=node-a", nil)
	rec := httptest.NewRecorder()
	wm.handleTunnelSessions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected tunnel sessions status: got %d want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode tunnel sessions response: %v", err)
	}
	if len(payload) != 0 {
		t.Fatalf("expected empty tunnel sessions response, got %+v", payload)
	}
}

func addMultipartFile(t *testing.T, writer *multipart.Writer, fieldName, fileName string, data []byte) {
	t.Helper()

	part, err := writer.CreateFormFile(fieldName, fileName)
	if err != nil {
		t.Fatalf("create multipart file %s: %v", fieldName, err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write multipart file %s: %v", fieldName, err)
	}
}

func TestHandleProxyConfigPreservesRuntimeEndpointWhileRunning(t *testing.T) {
	db := newTestDB(t)
	wm := &Manager{
		db:          db,
		socksServer: &ProxyServer{Type: "socks5"},
		httpServer: &ProxyServer{
			Type:       "http",
			Port:       8080,
			BindListen: true,
		},
	}
	wm.httpServer.Running.Store(true)

	body := bytes.NewBufferString(`{"type":"http","port":9090,"bindListen":false,"autoStart":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/proxy/config", body)
	rec := httptest.NewRecorder()
	wm.handleProxyConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if wm.httpServer.Port != 8080 || !wm.httpServer.BindListen {
		t.Fatalf("running proxy endpoint changed unexpectedly: %+v", wm.httpServer)
	}
	if !wm.httpServer.AutoStart {
		t.Fatal("expected autostart to be updated")
	}

	saved, err := config.LoadProxyConfig(db, "http")
	if err != nil {
		t.Fatalf("load saved config: %v", err)
	}
	if saved == nil {
		t.Fatal("expected saved proxy config")
	}
	if saved.Port != 8080 || !saved.BindListen || !saved.AutoStart {
		t.Fatalf("unexpected saved proxy config: %+v", saved)
	}
}

func TestHandleStatusReturnsProxySnapshots(t *testing.T) {
	db := newTestDB(t)
	wm := &Manager{
		db: db,
		socksServer: &ProxyServer{
			Type:       "socks5",
			Port:       1080,
			BindListen: false,
			AutoStart:  true,
		},
		httpServer: &ProxyServer{
			Type:       "http",
			Port:       8080,
			BindListen: true,
			AutoStart:  false,
		},
	}
	wm.socksServer.Running.Store(true)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	wm.handleStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d", rec.Code, http.StatusOK)
	}

	var payload statusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Socks5.Running || payload.Socks5.Port != 1080 || payload.Socks5.BindListen || !payload.Socks5.AutoStart {
		t.Fatalf("unexpected socks5 payload: %+v", payload.Socks5)
	}
	if payload.HTTP.Running || payload.HTTP.Port != 8080 || !payload.HTTP.BindListen || payload.HTTP.AutoStart {
		t.Fatalf("unexpected http payload: %+v", payload.HTTP)
	}
}

func TestParseMetricsQueryFallsBackOnInvalidValues(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/metrics/history?startTime=abc&endTime=xyz&limit=-5", nil)

	before := time.Now().Unix()
	start, end, limit := parseMetricsQuery(req)
	after := time.Now().Unix()

	if limit != 100 {
		t.Fatalf("unexpected limit: got %d want %d", limit, 100)
	}
	if start < before-24*60*60-1 || start > after-24*60*60+1 {
		t.Fatalf("unexpected default start: got %d before=%d after=%d", start, before, after)
	}
	if end < before || end > after {
		t.Fatalf("unexpected default end: got %d before=%d after=%d", end, before, after)
	}
}

func TestHandleMetricsHistoryRequiresGet(t *testing.T) {
	db := newTestDB(t)
	wm := &Manager{db: db}

	req := httptest.NewRequest(http.MethodPost, "/api/metrics/history", nil)
	rec := httptest.NewRecorder()
	wm.handleMetricsHistory(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("unexpected status: got %d want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleAuditLogsFiltersAndPagination(t *testing.T) {
	db := newTestDB(t)
	wm := &Manager{db: db}

	now := time.Date(2026, time.March, 30, 9, 0, 0, 0, time.UTC)
	rows := []models.AuditLog{
		{
			OccurredAt: now.Add(-2 * time.Hour),
			ActorType:  "admin",
			ActorID:    "web_admin",
			Action:     "proxy.start",
			TargetType: "proxy",
			TargetID:   "http:8080",
			Status:     "success",
			Message:    "HTTP proxy started",
			Details:    `{"port":8080}`,
		},
		{
			OccurredAt: now.Add(-1 * time.Hour),
			ActorType:  "admin",
			ActorID:    "web_admin",
			Action:     "proxy.stop",
			TargetType: "proxy",
			TargetID:   "socks5:1080",
			Status:     "failure",
			Message:    "SOCKS5 proxy stop failed",
		},
		{
			OccurredAt: now.Add(-30 * time.Minute),
			ActorType:  "admin",
			ActorID:    "ops_admin",
			Action:     "user.create",
			TargetType: "user",
			TargetID:   "alice",
			Status:     "success",
			Message:    "Created proxy user alice",
		},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed audit logs: %v", err)
	}

	req := httptest.NewRequest(
		http.MethodGet,
		fmt.Sprintf(
			"/api/logs/audit?page=1&limit=1&action=proxy.start&status=success&targetType=proxy&search=8080&from=%s&to=%s",
			now.Add(-3*time.Hour).Format(time.RFC3339),
			now.Add(-90*time.Minute).Format(time.RFC3339),
		),
		nil,
	)
	rec := httptest.NewRecorder()
	wm.handleAuditLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload paginatedLogsResponse[auditLogResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode audit logs response: %v", err)
	}
	if payload.Total != 1 || len(payload.Items) != 1 || payload.Page != 1 || payload.Limit != 1 {
		t.Fatalf("unexpected pagination payload: %+v", payload)
	}
	if payload.Items[0].Action != "proxy.start" || payload.Items[0].TargetID != "http:8080" {
		t.Fatalf("unexpected audit item: %+v", payload.Items[0])
	}
	if got := payload.Items[0].Details["port"]; got != float64(8080) {
		t.Fatalf("unexpected audit details: %+v", payload.Items[0].Details)
	}
}

func TestHandleEventLogsFiltersAndPagination(t *testing.T) {
	db := newTestDB(t)
	wm := &Manager{db: db}

	now := time.Date(2026, time.March, 30, 12, 0, 0, 0, time.UTC)
	rows := []models.EventLog{
		{
			OccurredAt: now.Add(-90 * time.Minute),
			Category:   "tunnel",
			EventType:  "managed_client_connected",
			Severity:   "info",
			Source:     "tunnel_server",
			Message:    "Managed tunnel client connected",
			Details:    `{"client_name":"node-a"}`,
		},
		{
			OccurredAt: now.Add(-45 * time.Minute),
			Category:   "security",
			EventType:  "ssrf_blocked",
			Severity:   "warn",
			Source:     "http_proxy",
			Message:    "Blocked SSRF attempt to metadata service",
		},
		{
			OccurredAt: now.Add(-15 * time.Minute),
			Category:   "auth",
			EventType:  "admin_login_failed",
			Severity:   "warn",
			Source:     "web_admin",
			Message:    "Admin login failed",
		},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed event logs: %v", err)
	}

	req := httptest.NewRequest(
		http.MethodGet,
		fmt.Sprintf(
			"/api/logs/events?page=1&limit=1&category=tunnel&severity=info&source=tunnel_server&eventType=managed_client_connected&search=connected&from=%s&to=%s",
			now.Add(-2*time.Hour).Format(time.RFC3339),
			now.Add(-1*time.Hour).Format(time.RFC3339),
		),
		nil,
	)
	rec := httptest.NewRecorder()
	wm.handleEventLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload paginatedLogsResponse[eventLogResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode event logs response: %v", err)
	}
	if payload.Total != 1 || len(payload.Items) != 1 || payload.Page != 1 || payload.Limit != 1 {
		t.Fatalf("unexpected pagination payload: %+v", payload)
	}
	if payload.Items[0].EventType != "managed_client_connected" || payload.Items[0].Source != "tunnel_server" {
		t.Fatalf("unexpected event item: %+v", payload.Items[0])
	}
	if got := payload.Items[0].Details["client_name"]; got != "node-a" {
		t.Fatalf("unexpected event details: %+v", payload.Items[0].Details)
	}
}

func TestAdminBootstrapSessionFlow(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)

	bootstrapToken, err := wm.ensureAdminBootstrapToken()
	if err != nil {
		t.Fatalf("ensure bootstrap token: %v", err)
	}

	sessionReq := httptest.NewRequest(http.MethodGet, "/api/admin/session", nil)
	sessionRec := httptest.NewRecorder()
	wm.handleAdminSession(sessionRec, sessionReq)

	if sessionRec.Code != http.StatusOK {
		t.Fatalf("unexpected initial session status: got %d want %d", sessionRec.Code, http.StatusOK)
	}

	var initial adminSessionResponse
	if err := json.Unmarshal(sessionRec.Body.Bytes(), &initial); err != nil {
		t.Fatalf("decode initial session response: %v", err)
	}
	if initial.Authenticated || !initial.BootstrapNeeded {
		t.Fatalf("unexpected initial session response: %+v", initial)
	}

	bootstrapReq := httptest.NewRequest(http.MethodPost, "/api/admin/bootstrap", bytes.NewBufferString(fmt.Sprintf(`{"password":"admin-pass-123","bootstrapToken":"%s"}`, bootstrapToken)))
	bootstrapRec := httptest.NewRecorder()
	wm.handleAdminBootstrap(bootstrapRec, bootstrapReq)

	if bootstrapRec.Code != http.StatusOK {
		t.Fatalf("unexpected bootstrap status: got %d want %d body=%s", bootstrapRec.Code, http.StatusOK, bootstrapRec.Body.String())
	}

	storedHash, err := config.GetSystemConfig(db, config.KeyWebAdminPassword)
	if err != nil {
		t.Fatalf("load stored admin password: %v", err)
	}
	if storedHash == "" {
		t.Fatal("expected admin password hash to be stored")
	}

	bootstrapCookie := bootstrapRec.Result().Cookies()
	if len(bootstrapCookie) == 0 || bootstrapCookie[0].Name != adminSessionCookieName {
		t.Fatalf("expected admin session cookie, got %+v", bootstrapCookie)
	}

	authReq := httptest.NewRequest(http.MethodGet, "/api/admin/session", nil)
	authReq.AddCookie(bootstrapCookie[0])
	authRec := httptest.NewRecorder()
	wm.handleAdminSession(authRec, authReq)

	if authRec.Code != http.StatusOK {
		t.Fatalf("unexpected authenticated session status: got %d want %d", authRec.Code, http.StatusOK)
	}

	var authenticated adminSessionResponse
	if err := json.Unmarshal(authRec.Body.Bytes(), &authenticated); err != nil {
		t.Fatalf("decode authenticated session response: %v", err)
	}
	if !authenticated.Authenticated || authenticated.BootstrapNeeded {
		t.Fatalf("unexpected authenticated session response: %+v", authenticated)
	}
}

func TestAdminBootstrapRejectsInvalidToken(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)

	if _, err := wm.ensureAdminBootstrapToken(); err != nil {
		t.Fatalf("ensure bootstrap token: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/bootstrap", bytes.NewBufferString(`{"password":"admin-pass-123","bootstrapToken":"wrong-token"}`))
	rec := httptest.NewRecorder()
	wm.handleAdminBootstrap(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("unexpected bootstrap status: got %d want %d body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}

	configured, err := wm.adminPasswordConfigured()
	if err != nil {
		t.Fatalf("check admin password configured: %v", err)
	}
	if configured {
		t.Fatal("expected admin password to remain unconfigured after invalid token")
	}
}

func TestAdminBootstrapRejectsSecondInitialization(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)

	bootstrapToken, err := wm.ensureAdminBootstrapToken()
	if err != nil {
		t.Fatalf("ensure bootstrap token: %v", err)
	}

	firstReq := httptest.NewRequest(http.MethodPost, "/api/admin/bootstrap", bytes.NewBufferString(fmt.Sprintf(`{"password":"admin-pass-123","bootstrapToken":"%s"}`, bootstrapToken)))
	firstRec := httptest.NewRecorder()
	wm.handleAdminBootstrap(firstRec, firstReq)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("unexpected first bootstrap status: got %d want %d body=%s", firstRec.Code, http.StatusOK, firstRec.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/api/admin/bootstrap", bytes.NewBufferString(fmt.Sprintf(`{"password":"admin-pass-456","bootstrapToken":"%s"}`, bootstrapToken)))
	secondRec := httptest.NewRecorder()
	wm.handleAdminBootstrap(secondRec, secondReq)
	if secondRec.Code != http.StatusConflict {
		t.Fatalf("unexpected second bootstrap status: got %d want %d body=%s", secondRec.Code, http.StatusConflict, secondRec.Body.String())
	}
}

func TestAdminLoginLogoutAndProtectedRoute(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)

	if err := wm.setAdminPassword("admin-pass-123"); err != nil {
		t.Fatalf("seed admin password: %v", err)
	}

	protected := wm.requireAdminAuth(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	unauthorizedReq := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	unauthorizedRec := httptest.NewRecorder()
	protected(unauthorizedRec, unauthorizedReq)

	if unauthorizedRec.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected protected route status without session: got %d want %d", unauthorizedRec.Code, http.StatusUnauthorized)
	}

	badLoginReq := httptest.NewRequest(http.MethodPost, "/api/admin/login", bytes.NewBufferString(`{"password":"wrong-pass"}`))
	badLoginRec := httptest.NewRecorder()
	wm.handleAdminLogin(badLoginRec, badLoginReq)

	if badLoginRec.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected bad login status: got %d want %d body=%s", badLoginRec.Code, http.StatusUnauthorized, badLoginRec.Body.String())
	}

	loginReq := httptest.NewRequest(http.MethodPost, "/api/admin/login", bytes.NewBufferString(`{"password":"admin-pass-123"}`))
	loginRec := httptest.NewRecorder()
	wm.handleAdminLogin(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("unexpected login status: got %d want %d body=%s", loginRec.Code, http.StatusOK, loginRec.Body.String())
	}

	loginCookie := loginRec.Result().Cookies()
	if len(loginCookie) == 0 || loginCookie[0].Name != adminSessionCookieName {
		t.Fatalf("expected login session cookie, got %+v", loginCookie)
	}

	authorizedReq := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	authorizedReq.AddCookie(loginCookie[0])
	authorizedRec := httptest.NewRecorder()
	protected(authorizedRec, authorizedReq)

	if authorizedRec.Code != http.StatusOK {
		t.Fatalf("unexpected protected route status with session: got %d want %d body=%s", authorizedRec.Code, http.StatusOK, authorizedRec.Body.String())
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/api/admin/logout", nil)
	logoutReq.AddCookie(loginCookie[0])
	logoutRec := httptest.NewRecorder()
	wm.handleAdminLogout(logoutRec, logoutReq)

	if logoutRec.Code != http.StatusOK {
		t.Fatalf("unexpected logout status: got %d want %d body=%s", logoutRec.Code, http.StatusOK, logoutRec.Body.String())
	}

	afterLogoutReq := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	afterLogoutReq.AddCookie(loginCookie[0])
	afterLogoutRec := httptest.NewRecorder()
	protected(afterLogoutRec, afterLogoutReq)

	if afterLogoutRec.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected protected route status after logout: got %d want %d", afterLogoutRec.Code, http.StatusUnauthorized)
	}
}

func TestAdminLoginWritesAuditAndEventLogs(t *testing.T) {
	db := newTestDB(t)
	useTestActivityRecorder(t, db)
	wm := newTestManager(db)

	if err := wm.setAdminPassword("admin-pass-123"); err != nil {
		t.Fatalf("seed admin password: %v", err)
	}

	badLoginReq := httptest.NewRequest(http.MethodPost, "/api/admin/login", bytes.NewBufferString(`{"password":"wrong-pass"}`))
	badLoginReq.RemoteAddr = "192.0.2.10:12345"
	badLoginRec := httptest.NewRecorder()
	wm.handleAdminLogin(badLoginRec, badLoginReq)
	if badLoginRec.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected bad login status: got %d want %d body=%s", badLoginRec.Code, http.StatusUnauthorized, badLoginRec.Body.String())
	}

	goodLoginReq := httptest.NewRequest(http.MethodPost, "/api/admin/login", bytes.NewBufferString(`{"password":"admin-pass-123"}`))
	goodLoginReq.RemoteAddr = "192.0.2.11:23456"
	goodLoginRec := httptest.NewRecorder()
	wm.handleAdminLogin(goodLoginRec, goodLoginReq)
	if goodLoginRec.Code != http.StatusOK {
		t.Fatalf("unexpected good login status: got %d want %d body=%s", goodLoginRec.Code, http.StatusOK, goodLoginRec.Body.String())
	}

	waitForModelCount(t, db, &models.AuditLog{}, 2)
	waitForModelCount(t, db, &models.EventLog{}, 2)

	var auditLogs []models.AuditLog
	if err := db.Order("occurred_at ASC").Find(&auditLogs).Error; err != nil {
		t.Fatalf("list audit logs: %v", err)
	}
	if len(auditLogs) != 2 {
		t.Fatalf("unexpected audit log count: got %d want 2", len(auditLogs))
	}
	if auditLogs[0].Action != "admin.login" || auditLogs[0].Status != "failure" {
		t.Fatalf("unexpected first audit log: %+v", auditLogs[0])
	}
	if auditLogs[1].Action != "admin.login" || auditLogs[1].Status != "success" {
		t.Fatalf("unexpected second audit log: %+v", auditLogs[1])
	}
	if auditLogs[1].SourceIP != "192.0.2.11" {
		t.Fatalf("unexpected audit source ip: %+v", auditLogs[1])
	}

	var eventLogs []models.EventLog
	if err := db.Order("occurred_at ASC").Find(&eventLogs).Error; err != nil {
		t.Fatalf("list event logs: %v", err)
	}
	if len(eventLogs) != 2 {
		t.Fatalf("unexpected event log count: got %d want 2", len(eventLogs))
	}
	if eventLogs[0].EventType != "admin_login_failed" || eventLogs[0].Severity != "warn" {
		t.Fatalf("unexpected first event log: %+v", eventLogs[0])
	}
	if eventLogs[1].EventType != "admin_login_succeeded" || eventLogs[1].Severity != "info" {
		t.Fatalf("unexpected second event log: %+v", eventLogs[1])
	}
}

func TestAdminSessionHidesPartialCaptchaConfig(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)

	if err := wm.setAdminPassword("admin-pass-123"); err != nil {
		t.Fatalf("seed admin password: %v", err)
	}
	t.Setenv("GEETEST_ID", "captcha-id")
	t.Setenv("GEETEST_KEY", "")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/session", nil)
	rec := httptest.NewRecorder()
	wm.handleAdminSession(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected session status: got %d want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload adminSessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode session response: %v", err)
	}
	if payload.GeetestID != "" {
		t.Fatalf("expected partial geetest config to stay hidden, got %+v", payload)
	}
	if payload.CaptchaError != "captcha configuration incomplete" {
		t.Fatalf("expected captcha misconfiguration error, got %+v", payload)
	}
}

func TestAdminLoginRejectsPartialCaptchaConfig(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)

	if err := wm.setAdminPassword("admin-pass-123"); err != nil {
		t.Fatalf("seed admin password: %v", err)
	}
	t.Setenv("GEETEST_ID", "captcha-id")
	t.Setenv("GEETEST_KEY", "")

	loginReq := httptest.NewRequest(http.MethodPost, "/api/admin/login", bytes.NewBufferString(`{"password":"admin-pass-123"}`))
	loginRec := httptest.NewRecorder()
	wm.handleAdminLogin(loginRec, loginReq)

	if loginRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unexpected login status: got %d want %d body=%s", loginRec.Code, http.StatusServiceUnavailable, loginRec.Body.String())
	}
	if !strings.Contains(loginRec.Body.String(), "captcha configuration incomplete") {
		t.Fatalf("unexpected login body: %s", loginRec.Body.String())
	}
}

func TestAdminLoginRequiresCaptchaWhenEnabled(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)

	if err := wm.setAdminPassword("admin-pass-123"); err != nil {
		t.Fatalf("seed admin password: %v", err)
	}
	t.Setenv("GEETEST_ID", "captcha-id")
	t.Setenv("GEETEST_KEY", "captcha-key")

	validator := &fakeGeetestValidator{valid: true}
	oldFactory := newGeetestValidator
	newGeetestValidator = func(id, key string) geetestValidator {
		validator.id = id
		validator.key = key
		return validator
	}
	t.Cleanup(func() {
		newGeetestValidator = oldFactory
	})

	missingCaptchaReq := httptest.NewRequest(http.MethodPost, "/api/admin/login", bytes.NewBufferString(`{"password":"admin-pass-123"}`))
	missingCaptchaRec := httptest.NewRecorder()
	wm.handleAdminLogin(missingCaptchaRec, missingCaptchaReq)

	if missingCaptchaRec.Code != http.StatusBadRequest {
		t.Fatalf("unexpected missing captcha status: got %d want %d body=%s", missingCaptchaRec.Code, http.StatusBadRequest, missingCaptchaRec.Body.String())
	}
	if len(validator.reqs) != 0 {
		t.Fatalf("validator should not run without captcha fields, got %d calls", len(validator.reqs))
	}

	validator.valid = false
	badCaptchaReq := httptest.NewRequest(http.MethodPost, "/api/admin/login", bytes.NewBufferString(`{"password":"admin-pass-123","lot_number":"lot-1","captcha_output":"out","pass_token":"token","gen_time":"123"}`))
	badCaptchaRec := httptest.NewRecorder()
	wm.handleAdminLogin(badCaptchaRec, badCaptchaReq)

	if badCaptchaRec.Code != http.StatusBadRequest {
		t.Fatalf("unexpected bad captcha status: got %d want %d body=%s", badCaptchaRec.Code, http.StatusBadRequest, badCaptchaRec.Body.String())
	}
	if len(validator.reqs) != 1 {
		t.Fatalf("expected validator call for bad captcha, got %d", len(validator.reqs))
	}

	validator.valid = true
	loginReq := httptest.NewRequest(http.MethodPost, "/api/admin/login", bytes.NewBufferString(`{"password":"admin-pass-123","lot_number":"lot-2","captcha_output":"out-2","pass_token":"token-2","gen_time":"456"}`))
	loginRec := httptest.NewRecorder()
	wm.handleAdminLogin(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("unexpected login status: got %d want %d body=%s", loginRec.Code, http.StatusOK, loginRec.Body.String())
	}
	if validator.id != "captcha-id" || validator.key != "captcha-key" {
		t.Fatalf("unexpected validator config: id=%q key=%q", validator.id, validator.key)
	}
	if len(validator.reqs) != 2 {
		t.Fatalf("expected validator to run twice, got %d calls", len(validator.reqs))
	}
	if validator.reqs[1].LotNumber != "lot-2" || validator.reqs[1].PassToken != "token-2" {
		t.Fatalf("unexpected validator request: %+v", validator.reqs[1])
	}
}
