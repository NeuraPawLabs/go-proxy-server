package web

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/apeming/go-proxy-server/internal/config"
)

const (
	managedTunnelClientTLSSubdir = "tunnel-client"
	managedTunnelClientCAFile    = "ca.pem"

	tunnelClientCertificateSourceNone     = "none"
	tunnelClientCertificateSourceUploaded = "uploaded"
)

type managedTunnelClientCertificateState struct {
	Ready     bool       `json:"ready"`
	Source    string     `json:"source"`
	CAName    string     `json:"caName"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
	Message   string     `json:"message,omitempty"`
}

type managedTunnelClientTLSAssetsMeta struct {
	Source    string    `json:"source"`
	CAName    string    `json:"caName"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (wm *Manager) loadManagedTunnelClientTLSAssetsMeta() (managedTunnelClientTLSAssetsMeta, error) {
	raw, err := config.GetSystemConfig(wm.db, config.KeyTunnelClientTLSAssets)
	if err != nil {
		return managedTunnelClientTLSAssetsMeta{}, err
	}
	if raw == "" {
		return managedTunnelClientTLSAssetsMeta{}, nil
	}

	var meta managedTunnelClientTLSAssetsMeta
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return managedTunnelClientTLSAssetsMeta{}, fmt.Errorf("decode tunnel client TLS assets metadata: %w", err)
	}
	return meta, nil
}

func (wm *Manager) saveManagedTunnelClientTLSAssetsMeta(meta managedTunnelClientTLSAssetsMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("encode tunnel client TLS assets metadata: %w", err)
	}
	return config.SetSystemConfig(wm.db, config.KeyTunnelClientTLSAssets, string(data))
}

func managedTunnelClientTLSDir() (string, error) {
	dataDir, err := getTunnelDataDir()
	if err != nil {
		return "", fmt.Errorf("resolve data directory: %w", err)
	}
	path := filepath.Join(dataDir, managedTunnelClientTLSSubdir)
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", fmt.Errorf("create tunnel client TLS directory: %w", err)
	}
	return path, nil
}

func managedTunnelClientTLSCAPath() (string, error) {
	dir, err := managedTunnelClientTLSDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, managedTunnelClientCAFile), nil
}

func (wm *Manager) getManagedTunnelClientCertificateState() (managedTunnelClientCertificateState, error) {
	meta, err := wm.loadManagedTunnelClientTLSAssetsMeta()
	if err != nil {
		return managedTunnelClientCertificateState{}, err
	}
	caPath, err := managedTunnelClientTLSCAPath()
	if err != nil {
		return managedTunnelClientCertificateState{}, err
	}
	caExists := fileExists(caPath)
	if caExists || meta.Source != "" {
		state := managedTunnelClientCertificateState{
			Ready:  caExists,
			Source: nonEmpty(meta.Source, tunnelClientCertificateSourceUploaded),
			CAName: nonEmpty(meta.CAName, managedTunnelClientCAFile),
		}
		if !meta.UpdatedAt.IsZero() {
			updatedAt := meta.UpdatedAt
			state.UpdatedAt = &updatedAt
		}
		if !state.Ready {
			state.Message = "客户端 CA 材料不完整，请重新上传。"
		}
		return state, nil
	}

	return managedTunnelClientCertificateState{
		Ready:   false,
		Source:  tunnelClientCertificateSourceNone,
		Message: "尚未上传客户端 CA。",
	}, nil
}

func (wm *Manager) storeManagedTunnelClientCA(uploadName string, caPEM []byte) error {
	if _, err := parseCertificatePEM(caPEM); err != nil {
		return fmt.Errorf("validate tunnel client CA file: %w", err)
	}

	caPath, err := managedTunnelClientTLSCAPath()
	if err != nil {
		return err
	}
	if err := os.WriteFile(caPath, caPEM, 0o600); err != nil {
		return fmt.Errorf("write tunnel client CA file: %w", err)
	}

	meta := managedTunnelClientTLSAssetsMeta{
		Source:    tunnelClientCertificateSourceUploaded,
		CAName:    sanitizeUploadedFilename(uploadName, managedTunnelClientCAFile),
		UpdatedAt: time.Now(),
	}
	return wm.saveManagedTunnelClientTLSAssetsMeta(meta)
}

func (wm *Manager) resolveManagedTunnelClientUploadedCA() (string, error) {
	state, err := wm.getManagedTunnelClientCertificateState()
	if err != nil {
		return "", err
	}
	if !state.Ready {
		return "", fmt.Errorf("managed tunnel client requires uploaded CA file")
	}
	caPath, err := managedTunnelClientTLSCAPath()
	if err != nil {
		return "", err
	}
	return caPath, nil
}
