package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadTunnelServerTLSConfigRequiresCertAndKey(t *testing.T) {
	if _, err := loadTunnelServerTLSConfig("", "", false); err == nil {
		t.Fatal("expected error when TLS files are missing")
	}
}

func TestLoadTunnelClientTLSConfigRequiresVerificationMode(t *testing.T) {
	if _, err := loadTunnelClientTLSConfig("127.0.0.1:7000", "", "", false, false); err == nil {
		t.Fatal("expected error when no verification mode is configured")
	}
}

func TestLoadTunnelClientTLSConfigLoadsCAFile(t *testing.T) {
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caPath, generateTestCertPEM(t), 0600); err != nil {
		t.Fatalf("write ca file: %v", err)
	}

	cfg, err := loadTunnelClientTLSConfig("127.0.0.1:7000", caPath, "", false, false)
	if err != nil {
		t.Fatalf("load client tls config: %v", err)
	}
	if cfg.RootCAs == nil {
		t.Fatal("expected RootCAs to be populated")
	}
	if cfg.ServerName != "127.0.0.1" {
		t.Fatalf("unexpected server name: %q", cfg.ServerName)
	}
}

func generateTestCertPEM(t *testing.T) []byte {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate private key: %v", err)
	}

	serialNumber, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatalf("generate serial number: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
