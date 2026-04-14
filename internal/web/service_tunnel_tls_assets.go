package web

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/apeming/go-proxy-server/internal/config"
	"github.com/apeming/go-proxy-server/internal/tunnel"
)

const (
	managedTunnelTLSSubdir     = "tunnel-server"
	managedTunnelServerCertPEM = "server.crt"
	managedTunnelServerKeyPEM  = "server.key"
	managedTunnelClientCAPEM   = "ca.pem"

	tunnelCertificateSourceNone      = "none"
	tunnelCertificateSourceUploaded  = "uploaded"
	tunnelCertificateSourceGenerated = "generated"
	tunnelCertificateSourceLegacy    = "legacy-path"
)

var getTunnelDataDir = config.GetDataDir

type tunnelServerCertificateState struct {
	Ready               bool       `json:"ready"`
	Managed             bool       `json:"managed"`
	Source              string     `json:"source"`
	ServerCertName      string     `json:"serverCertName"`
	ServerKeyName       string     `json:"serverKeyName"`
	ClientCAName        string     `json:"clientCaName"`
	CanDownloadClientCA bool       `json:"canDownloadClientCa"`
	UpdatedAt           *time.Time `json:"updatedAt,omitempty"`
	Message             string     `json:"message,omitempty"`
}

type tunnelServerTLSAssetsMeta struct {
	Source         string    `json:"source"`
	ServerCertName string    `json:"serverCertName"`
	ServerKeyName  string    `json:"serverKeyName"`
	ClientCAName   string    `json:"clientCaName"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type tunnelServerCertificateUpload struct {
	ServerCertName string
	ServerCertPEM  []byte
	ServerKeyName  string
	ServerKeyPEM   []byte
	ClientCAName   string
	ClientCAPEM    []byte
}

type generatedTunnelServerCertificates struct {
	ServerCertPEM []byte
	ServerKeyPEM  []byte
	ClientCAPEM   []byte
}

func (wm *Manager) loadManagedTunnelServerTLSAssetsMeta() (tunnelServerTLSAssetsMeta, error) {
	raw, err := config.GetSystemConfig(wm.db, config.KeyTunnelServerTLSAssets)
	if err != nil {
		return tunnelServerTLSAssetsMeta{}, err
	}
	if raw == "" {
		return tunnelServerTLSAssetsMeta{}, nil
	}

	var meta tunnelServerTLSAssetsMeta
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return tunnelServerTLSAssetsMeta{}, fmt.Errorf("decode tunnel TLS assets metadata: %w", err)
	}
	return meta, nil
}

func (wm *Manager) saveManagedTunnelServerTLSAssetsMeta(meta tunnelServerTLSAssetsMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("encode tunnel TLS assets metadata: %w", err)
	}
	return config.SetSystemConfig(wm.db, config.KeyTunnelServerTLSAssets, string(data))
}

func managedTunnelServerTLSDir() (string, error) {
	dataDir, err := getTunnelDataDir()
	if err != nil {
		return "", fmt.Errorf("resolve data directory: %w", err)
	}
	path := filepath.Join(dataDir, managedTunnelTLSSubdir)
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", fmt.Errorf("create tunnel TLS directory: %w", err)
	}
	return path, nil
}

func managedTunnelServerTLSPaths() (string, string, string, error) {
	dir, err := managedTunnelServerTLSDir()
	if err != nil {
		return "", "", "", err
	}
	return filepath.Join(dir, managedTunnelServerCertPEM), filepath.Join(dir, managedTunnelServerKeyPEM), filepath.Join(dir, managedTunnelClientCAPEM), nil
}

func (wm *Manager) getTunnelServerCertificateState(cfg tunnelServerConfig) (tunnelServerCertificateState, error) {
	meta, err := wm.loadManagedTunnelServerTLSAssetsMeta()
	if err != nil {
		return tunnelServerCertificateState{}, err
	}

	certPath, keyPath, caPath, err := managedTunnelServerTLSPaths()
	if err != nil {
		return tunnelServerCertificateState{}, err
	}
	managedCertExists := fileExists(certPath)
	managedKeyExists := fileExists(keyPath)
	managedCAExists := fileExists(caPath)
	if managedCertExists || managedKeyExists || managedCAExists || meta.Source != "" {
		state := tunnelServerCertificateState{
			Ready:               managedCertExists && managedKeyExists,
			Managed:             true,
			Source:              nonEmpty(meta.Source, tunnelCertificateSourceUploaded),
			ServerCertName:      nonEmpty(meta.ServerCertName, managedTunnelServerCertPEM),
			ServerKeyName:       nonEmpty(meta.ServerKeyName, managedTunnelServerKeyPEM),
			ClientCAName:        nonEmpty(meta.ClientCAName, managedTunnelClientCAPEM),
			CanDownloadClientCA: managedCAExists,
		}
		if !meta.UpdatedAt.IsZero() {
			updatedAt := meta.UpdatedAt
			state.UpdatedAt = &updatedAt
		}
		if !state.Ready {
			state.Message = "TLS 材料不完整，请重新上传或重新生成证书。"
		}
		return state, nil
	}

	legacyCertExists := cfg.CertFile != "" && fileExists(cfg.CertFile)
	legacyKeyExists := cfg.KeyFile != "" && fileExists(cfg.KeyFile)
	if cfg.CertFile != "" || cfg.KeyFile != "" {
		state := tunnelServerCertificateState{
			Ready:               legacyCertExists && legacyKeyExists,
			Managed:             false,
			Source:              tunnelCertificateSourceLegacy,
			ServerCertName:      filepath.Base(cfg.CertFile),
			ServerKeyName:       filepath.Base(cfg.KeyFile),
			ClientCAName:        filepath.Base(cfg.CertFile),
			CanDownloadClientCA: legacyCertExists,
		}
		if state.Ready {
			state.Message = "已检测到旧版路径证书配置，建议重新上传或生成以纳入后台统一管理。"
		} else {
			state.Message = "旧版路径证书文件不可用，请重新上传或直接生成。"
		}
		return state, nil
	}

	return tunnelServerCertificateState{
		Ready:   false,
		Managed: false,
		Source:  tunnelCertificateSourceNone,
		Message: "尚未配置 TLS 材料，可上传现有证书，或由后台直接生成。",
	}, nil
}

func (wm *Manager) resolveTunnelServerTLSFiles(cfg tunnelServerConfig) (string, string, error) {
	state, err := wm.getTunnelServerCertificateState(cfg)
	if err != nil {
		return "", "", err
	}
	if state.Managed {
		if !state.Ready {
			return "", "", fmt.Errorf("tunnel TLS materials are incomplete, please upload or generate certificate files first")
		}
		certPath, keyPath, _, err := managedTunnelServerTLSPaths()
		if err != nil {
			return "", "", err
		}
		return certPath, keyPath, nil
	}
	if state.Source == tunnelCertificateSourceLegacy {
		if !state.Ready {
			return "", "", fmt.Errorf("legacy tunnel certificate files are not available, please upload or generate certificate files")
		}
		return cfg.CertFile, cfg.KeyFile, nil
	}
	return "", "", fmt.Errorf("tunnel server requires uploaded or generated certificate files")
}

func (wm *Manager) loadManagedTunnelServerTLSConfig(cfg tunnelServerConfig) (*tls.Config, error) {
	certPath, keyPath, err := wm.resolveTunnelServerTLSFiles(cfg)
	if err != nil {
		return nil, err
	}
	return tunnel.LoadServerTLSConfig(certPath, keyPath, false)
}

func (wm *Manager) storeManagedTunnelServerCertificates(upload tunnelServerCertificateUpload, source string) error {
	if err := validateTunnelServerCertificates(upload.ServerCertPEM, upload.ServerKeyPEM, upload.ClientCAPEM); err != nil {
		return err
	}

	certPath, keyPath, caPath, err := managedTunnelServerTLSPaths()
	if err != nil {
		return err
	}

	if err := os.WriteFile(certPath, upload.ServerCertPEM, 0o644); err != nil {
		return fmt.Errorf("write tunnel server certificate: %w", err)
	}
	if err := os.WriteFile(keyPath, upload.ServerKeyPEM, 0o600); err != nil {
		return fmt.Errorf("write tunnel server key: %w", err)
	}
	if err := os.WriteFile(caPath, upload.ClientCAPEM, 0o644); err != nil {
		return fmt.Errorf("write tunnel client CA file: %w", err)
	}

	meta := tunnelServerTLSAssetsMeta{
		Source:         source,
		ServerCertName: sanitizeUploadedFilename(upload.ServerCertName, managedTunnelServerCertPEM),
		ServerKeyName:  sanitizeUploadedFilename(upload.ServerKeyName, managedTunnelServerKeyPEM),
		ClientCAName:   sanitizeUploadedFilename(upload.ClientCAName, managedTunnelClientCAPEM),
		UpdatedAt:      time.Now(),
	}
	return wm.saveManagedTunnelServerTLSAssetsMeta(meta)
}

func validateTunnelServerCertificates(serverCertPEM, serverKeyPEM, clientCAPEM []byte) error {
	if len(serverCertPEM) == 0 || len(serverKeyPEM) == 0 {
		return fmt.Errorf("server certificate and private key are required")
	}
	if _, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM); err != nil {
		return fmt.Errorf("parse tunnel server certificate and key: %w", err)
	}
	if _, err := parseCertificatePEM(serverCertPEM); err != nil {
		return fmt.Errorf("parse tunnel server certificate: %w", err)
	}
	if len(clientCAPEM) == 0 {
		return fmt.Errorf("client CA certificate is required")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(clientCAPEM) {
		return fmt.Errorf("parse client CA certificate: no certificates found")
	}
	return nil
}

func parseCertificatePEM(pemData []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	remaining := pemData
	for {
		block, rest := pem.Decode(remaining)
		if block == nil {
			break
		}
		remaining = rest
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		certs = append(certs, cert)
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("no certificates found")
	}
	return certs, nil
}

func generateTunnelServerCertificates(commonName string, hosts []string, validDays int) (generatedTunnelServerCertificates, error) {
	hosts = normalizeTunnelCertificateHosts(hosts)
	if len(hosts) == 0 {
		return generatedTunnelServerCertificates{}, fmt.Errorf("at least one host or IP is required")
	}
	if validDays <= 0 {
		validDays = 365
	}
	if commonName == "" {
		commonName = hosts[0]
	}

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return generatedTunnelServerCertificates{}, fmt.Errorf("generate CA key: %w", err)
	}
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return generatedTunnelServerCertificates{}, fmt.Errorf("generate server key: %w", err)
	}

	now := time.Now().Add(-5 * time.Minute)
	caTemplate := &x509.Certificate{
		SerialNumber:          mustRandomSerial(),
		Subject:               pkix.Name{CommonName: commonName + " Tunnel CA", Organization: []string{"go-proxy-server"}},
		NotBefore:             now,
		NotAfter:              now.Add(time.Duration(validDays) * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return generatedTunnelServerCertificates{}, fmt.Errorf("create CA certificate: %w", err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return generatedTunnelServerCertificates{}, fmt.Errorf("parse generated CA certificate: %w", err)
	}

	serverTemplate := &x509.Certificate{
		SerialNumber: mustRandomSerial(),
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"go-proxy-server"},
		},
		NotBefore:             now,
		NotAfter:              now.Add(time.Duration(validDays) * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, host := range hosts {
		if ip := net.ParseIP(host); ip != nil {
			serverTemplate.IPAddresses = append(serverTemplate.IPAddresses, ip)
			continue
		}
		serverTemplate.DNSNames = append(serverTemplate.DNSNames, host)
	}

	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		return generatedTunnelServerCertificates{}, fmt.Errorf("create server certificate: %w", err)
	}

	serverKeyBytes, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		return generatedTunnelServerCertificates{}, fmt.Errorf("marshal server key: %w", err)
	}

	return generatedTunnelServerCertificates{
		ServerCertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}),
		ServerKeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: serverKeyBytes}),
		ClientCAPEM:   pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
	}, nil
}

func normalizeTunnelCertificateHosts(hosts []string) []string {
	seen := make(map[string]struct{}, len(hosts))
	result := make([]string, 0, len(hosts))
	for _, host := range hosts {
		for _, part := range strings.FieldsFunc(host, func(r rune) bool {
			return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
		}) {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			if _, ok := seen[trimmed]; ok {
				continue
			}
			seen[trimmed] = struct{}{}
			result = append(result, trimmed)
		}
	}
	return result
}

func mustRandomSerial() *big.Int {
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		panic(err)
	}
	return serialNumber
}

func sanitizeUploadedFilename(name, fallback string) string {
	name = strings.TrimSpace(filepath.Base(name))
	if name == "." || name == string(filepath.Separator) || name == "" {
		return fallback
	}
	return name
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func (wm *Manager) resolveTunnelServerDownload(kind string, cfg tunnelServerConfig) (string, string, error) {
	state, err := wm.getTunnelServerCertificateState(cfg)
	if err != nil {
		return "", "", err
	}

	switch state.Source {
	case tunnelCertificateSourceUploaded, tunnelCertificateSourceGenerated:
		_, _, caPath, err := managedTunnelServerTLSPaths()
		if err != nil {
			return "", "", err
		}
		if kind == "client-ca" {
			if !state.CanDownloadClientCA {
				return "", "", os.ErrNotExist
			}
			return caPath, state.ClientCAName, nil
		}
	case tunnelCertificateSourceLegacy:
		if kind == "client-ca" {
			if !state.CanDownloadClientCA {
				return "", "", os.ErrNotExist
			}
			return cfg.CertFile, nonEmpty(state.ClientCAName, managedTunnelClientCAPEM), nil
		}
	}

	return "", "", os.ErrNotExist
}

func cloneOrDefaultCAPEM(certPEM, caPEM []byte) []byte {
	if len(bytes.TrimSpace(caPEM)) == 0 {
		return append([]byte(nil), certPEM...)
	}
	return caPEM
}
