package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStartTunnelClientRuntimeAllowsInsecureSkipVerifyWithoutManagedCA(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)

	if err := wm.StartTunnelClientRuntime("classic", "127.0.0.1:7443", "secret-token", "edge-node", "", "", true, false); err != nil {
		t.Fatalf("StartTunnelClientRuntime: %v", err)
	}

	stored, err := wm.loadManagedTunnelClientConfig()
	if err != nil {
		t.Fatalf("loadManagedTunnelClientConfig: %v", err)
	}
	if stored.UseManagedServerCA {
		t.Fatalf("stored UseManagedServerCA = true, want false for insecure_skip_verify")
	}
	if !stored.InsecureSkipVerify {
		t.Fatalf("stored InsecureSkipVerify = false, want true")
	}
}

func TestStartTunnelServerRuntimeUsesManagedCertificatesWhenPathsOmitted(t *testing.T) {
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

	generateReq := httptest.NewRequest(http.MethodPost, "/api/tunnel/server/certificates/generate", bytes.NewBufferString(`{"commonName":"runtime.example.com","hosts":["runtime.example.com","127.0.0.1"],"validDays":30}`))
	generateRec := httptest.NewRecorder()
	wm.handleTunnelServerGenerateCertificates(generateRec, generateReq)
	if generateRec.Code != http.StatusOK {
		t.Fatalf("unexpected generate status: got %d want %d body=%s", generateRec.Code, http.StatusOK, generateRec.Body.String())
	}

	if err := wm.StartTunnelServerRuntime("classic", "127.0.0.1:0", "127.0.0.1", "secret-token", "", "", false, 0, 0); err != nil {
		t.Fatalf("StartTunnelServerRuntime: %v", err)
	}
	defer func() {
		if err := wm.stopManagedTunnelServer("classic"); err != nil {
			t.Fatalf("stopManagedTunnelServer: %v", err)
		}
	}()

	status, err := wm.getManagedTunnelServerStatus()
	if err != nil {
		t.Fatalf("getManagedTunnelServerStatus: %v", err)
	}
	if !status.Classic.Running {
		t.Fatalf("expected classic tunnel server to be running, status=%+v", status.Classic)
	}
}

func TestStartTunnelServerRuntimeAllowsInsecureModeWithManagedCertificatesPresent(t *testing.T) {
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

	generateReq := httptest.NewRequest(http.MethodPost, "/api/tunnel/server/certificates/generate", bytes.NewBufferString(`{"commonName":"insecure.example.com","hosts":["insecure.example.com","127.0.0.1"],"validDays":30}`))
	generateRec := httptest.NewRecorder()
	wm.handleTunnelServerGenerateCertificates(generateRec, generateReq)
	if generateRec.Code != http.StatusOK {
		t.Fatalf("unexpected generate status: got %d want %d body=%s", generateRec.Code, http.StatusOK, generateRec.Body.String())
	}

	if err := wm.StartTunnelServerRuntime("classic", "127.0.0.1:0", "127.0.0.1", "secret-token", "", "", true, 0, 0); err != nil {
		t.Fatalf("StartTunnelServerRuntime: %v", err)
	}
	defer func() {
		if err := wm.stopManagedTunnelServer("classic"); err != nil {
			t.Fatalf("stopManagedTunnelServer: %v", err)
		}
	}()
}
