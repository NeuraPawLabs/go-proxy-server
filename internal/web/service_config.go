package web

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/apeming/go-proxy-server/internal/autostart"
	"github.com/apeming/go-proxy-server/internal/config"
	"github.com/apeming/go-proxy-server/internal/proxy"
)

func (wm *Manager) buildConfigResponse() (configResponse, error) {
	timeout := config.GetTimeout()
	limiterConfig := config.GetLimiterConfig()
	autostartValue, err := config.GetSystemConfig(wm.db, config.KeyAutoStart)
	if err != nil {
		return configResponse{}, err
	}
	autostartSupported := runtime.GOOS == "windows"
	registryEnabled := false
	if autostartSupported {
		registryEnabled, _ = autostart.IsEnabled()
	}

	return configResponse{
		Timeout: configTimeoutResponse{
			Connect:   int(timeout.Connect.Seconds()),
			IdleRead:  int(timeout.IdleRead.Seconds()),
			IdleWrite: int(timeout.IdleWrite.Seconds()),
		},
		Limiter: configLimiterResponse{
			MaxConcurrentConnections:      limiterConfig.MaxConcurrentConnections,
			MaxConcurrentConnectionsPerIP: limiterConfig.MaxConcurrentConnectionsPerIP,
		},
		System: configSystemResponse{
			AutostartEnabled:   autostartValue == "true",
			RegistryEnabled:    registryEnabled,
			AutostartSupported: autostartSupported,
			Platform:           runtime.GOOS,
		},
		Security: configSecurityResponse{
			AllowPrivateIPAccess: config.GetAllowPrivateIPAccess(),
		},
	}, nil
}

func (wm *Manager) applyConfigUpdate(req configUpdateRequest) error {
	if req.Timeout != nil {
		if err := wm.updateTimeoutConfig(req.Timeout.Connect, req.Timeout.IdleRead, req.Timeout.IdleWrite); err != nil {
			return err
		}
	}
	if req.Limiter != nil {
		if err := wm.updateLimiterConfig(req.Limiter.MaxConcurrentConnections, req.Limiter.MaxConcurrentConnectionsPerIP); err != nil {
			return err
		}
	}
	if req.System != nil {
		if err := wm.updateSystemConfig(req.System.AutostartEnabled); err != nil {
			return err
		}
	}
	if req.Security != nil {
		if err := config.UpdateAllowPrivateIPAccess(wm.db, req.Security.AllowPrivateIPAccess); err != nil {
			return fmt.Errorf("failed to update security configuration: %v", err)
		}
	}
	return nil
}

func (wm *Manager) updateTimeoutConfig(connect, idleRead, idleWrite int) error {
	if connect <= 0 || connect > 300 {
		return fmt.Errorf("Connect timeout must be between 1 and 300 seconds")
	}
	if idleRead <= 0 || idleRead > 3600 {
		return fmt.Errorf("Idle read timeout must be between 1 and 3600 seconds")
	}
	if idleWrite <= 0 || idleWrite > 3600 {
		return fmt.Errorf("Idle write timeout must be between 1 and 3600 seconds")
	}

	currentTimeout := config.GetTimeout()
	newTimeout := config.TimeoutConfig{
		Connect:          time.Duration(connect) * time.Second,
		IdleRead:         time.Duration(idleRead) * time.Second,
		IdleWrite:        time.Duration(idleWrite) * time.Second,
		MaxConnectionAge: currentTimeout.MaxConnectionAge,
		CleanupTimeout:   currentTimeout.CleanupTimeout,
	}
	if err := config.UpdateTimeout(wm.db, newTimeout); err != nil {
		return fmt.Errorf("Failed to save timeout configuration: %v", err)
	}
	return nil
}

func (wm *Manager) updateLimiterConfig(maxConn, maxConnPerIP int32) error {
	if maxConn <= 0 || maxConn > 1000000 {
		return fmt.Errorf("Max concurrent connections must be between 1 and 1000000")
	}
	if maxConnPerIP <= 0 || maxConnPerIP > 100000 {
		return fmt.Errorf("Max concurrent connections per IP must be between 1 and 100000")
	}
	if err := config.UpdateLimiterConfig(wm.db, maxConn, maxConnPerIP); err != nil {
		return fmt.Errorf("Failed to update limiter configuration: %v", err)
	}
	proxy.RecreateLimiters()
	return nil
}

func (wm *Manager) updateSystemConfig(enabled bool) error {
	if enabled {
		if err := autostart.Enable(); err != nil {
			return fmt.Errorf("Failed to enable autostart: %v", err)
		}
	} else {
		if err := autostart.Disable(); err != nil {
			return fmt.Errorf("Failed to disable autostart: %v", err)
		}
	}

	value := "false"
	if enabled {
		value = "true"
	}
	if err := config.SetSystemConfig(wm.db, config.KeyAutoStart, value); err != nil {
		return err
	}
	return nil
}

func statusCodeForConfigError(err error) int {
	msg := err.Error()
	if hasPrefixAny(msg,
		"Connect timeout must be",
		"Idle read timeout must be",
		"Idle write timeout must be",
		"Max concurrent connections must be",
		"Max concurrent connections per IP must be",
	) {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func hasPrefixAny(s string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}
