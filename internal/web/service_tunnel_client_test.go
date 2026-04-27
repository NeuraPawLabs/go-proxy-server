package web

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/apeming/go-proxy-server/internal/config"
)

func TestSaveManagedTunnelClientConfigForRuntimeAllowsInsecureClassicClient(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)

	cfg := managedTunnelClientConfig{
		Engine:        "classic",
		ServerAddr:    "127.0.0.1:7443",
		ClientName:    "edge-node",
		Token:         "secret-token",
		AllowInsecure: true,
	}

	if err := wm.saveManagedTunnelClientConfigForRuntime(cfg); err != nil {
		t.Fatalf("saveManagedTunnelClientConfigForRuntime: %v", err)
	}

	raw, err := config.GetSystemConfig(db, config.KeyTunnelClientConfig)
	if err != nil {
		t.Fatalf("GetSystemConfig: %v", err)
	}
	if raw == "" {
		t.Fatal("expected persisted tunnel client config")
	}

	stored, err := wm.loadManagedTunnelClientConfig()
	if err != nil {
		t.Fatalf("loadManagedTunnelClientConfig: %v", err)
	}
	if !stored.AllowInsecure {
		t.Fatalf("stored config lost allowInsecure flag: %+v", stored)
	}
	if stored.ServerAddr != cfg.ServerAddr || stored.ClientName != cfg.ClientName || stored.Token != cfg.Token {
		t.Fatalf("stored config mismatch: got %+v want %+v", stored, cfg)
	}
}

func TestStartTunnelClientRuntimeUsesCAFileForSecureClassicClient(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)

	caPath := writeTestCAFile(t, t.TempDir())
	err := wm.StartTunnelClientRuntime(
		"classic",
		"127.0.0.1:7443",
		"secret-token",
		"edge-node",
		caPath,
		"tunnel.example.com",
		false,
		false,
	)
	if err != nil {
		t.Fatalf("StartTunnelClientRuntime: %v", err)
	}

	stored, err := wm.loadManagedTunnelClientConfig()
	if err != nil {
		t.Fatalf("loadManagedTunnelClientConfig: %v", err)
	}
	if stored.CAFile != caPath {
		t.Fatalf("stored CAFile = %q, want %q", stored.CAFile, caPath)
	}
	if stored.ServerName != "tunnel.example.com" {
		t.Fatalf("stored ServerName = %q, want %q", stored.ServerName, "tunnel.example.com")
	}

	status, err := wm.getManagedTunnelClientStatus()
	if err != nil {
		t.Fatalf("getManagedTunnelClientStatus: %v", err)
	}
	if status.EffectiveCAFile != caPath {
		t.Fatalf("EffectiveCAFile = %q, want %q", status.EffectiveCAFile, caPath)
	}
}

func TestStartTunnelClientRuntimeUsesManagedServerCAWhenCAOmitted(t *testing.T) {
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

	sourceCA := writeTestCAFile(t, t.TempDir())
	_, _, managedCAPath, err := managedTunnelServerTLSPaths()
	if err != nil {
		t.Fatalf("managedTunnelServerTLSPaths: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(managedCAPath), 0o700); err != nil {
		t.Fatalf("mkdir managed ca dir: %v", err)
	}
	in, err := os.Open(sourceCA)
	if err != nil {
		t.Fatalf("open source ca: %v", err)
	}
	defer in.Close()
	out, err := os.Create(managedCAPath)
	if err != nil {
		t.Fatalf("create managed ca: %v", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatalf("copy managed ca: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close managed ca: %v", err)
	}

	if err := wm.StartTunnelClientRuntime("classic", "127.0.0.1:7443", "secret-token", "edge-node", "", "", false, false); err != nil {
		t.Fatalf("StartTunnelClientRuntime: %v", err)
	}

	stored, err := wm.loadManagedTunnelClientConfig()
	if err != nil {
		t.Fatalf("loadManagedTunnelClientConfig: %v", err)
	}
	if !stored.UseManagedServerCA {
		t.Fatalf("stored UseManagedServerCA = false, want true")
	}

	status, err := wm.getManagedTunnelClientStatus()
	if err != nil {
		t.Fatalf("getManagedTunnelClientStatus: %v", err)
	}
	if status.EffectiveCAFile != managedCAPath {
		t.Fatalf("EffectiveCAFile = %q, want %q", status.EffectiveCAFile, managedCAPath)
	}
}

func writeTestCAFile(t *testing.T, dir string) string {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	path := filepath.Join(dir, "ca.pem")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create ca file: %v", err)
	}
	defer f.Close()
	if err := pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatalf("write ca file: %v", err)
	}
	return path
}
