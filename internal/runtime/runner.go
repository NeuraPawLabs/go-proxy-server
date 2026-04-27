package runtime

import (
	"context"
	"errors"
)

// StartupItem identifies a service that should be started by the runtime.
type StartupItem struct {
	Name string
}

// Manager is the narrow runtime startup surface used by the one-process runner.
type Manager interface {
	StartWeb(port int) error
	StartSocks(port int, bindListen bool) error
	StartHTTP(port int, bindListen bool) error
	StartTunnelServer(cfg TunnelServerConfig) error
	StartTunnelClient(cfg TunnelClientConfig) error
	Shutdown() error
}

// Runner starts enabled services in a deterministic order.
type Runner struct {
	Manager Manager
}

// WebStarter is the adapter surface implemented by internal/web.Manager.
type WebStarter interface {
	StartWeb(port int) error
	StartSocks(port int, bindListen bool) error
	StartHTTP(port int, bindListen bool) error
	StartTunnelServerRuntime(engine, listen, publicBind, token, cert, key string, allowInsecure bool, autoPortStart, autoPortEnd int) error
	StartTunnelClientRuntime(engine, server, token, client, ca, serverName string, insecureSkipVerify, allowInsecure bool) error
	ShutdownApplication() error
}

// WebManagerAdapter bridges internal/runtime startup calls onto internal/web.
type WebManagerAdapter struct {
	Manager WebStarter
}

// NewWebManagerAdapter wraps a web manager with the runtime startup interface.
func NewWebManagerAdapter(manager WebStarter) WebManagerAdapter {
	return WebManagerAdapter{Manager: manager}
}

// BuildStartupPlan returns the enabled services in the order they should start.
func BuildStartupPlan(cfg Config) []StartupItem {
	plan := make([]StartupItem, 0, 5)
	if cfg.Socks.Enabled {
		plan = append(plan, StartupItem{Name: "socks"})
	}
	if cfg.HTTP.Enabled {
		plan = append(plan, StartupItem{Name: "http"})
	}
	if cfg.TunnelServer.Enabled {
		plan = append(plan, StartupItem{Name: "tunnel-server"})
	}
	if cfg.TunnelClient.Enabled {
		plan = append(plan, StartupItem{Name: "tunnel-client"})
	}
	if cfg.Web.Enabled {
		plan = append(plan, StartupItem{Name: "web"})
	}
	return plan
}

// Start launches the enabled services sequentially.
func (r Runner) Start(ctx context.Context, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if r.Manager == nil {
		return errors.New("runtime manager is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	cleanupOnFailure := func() {
		if err := r.Manager.Shutdown(); err != nil {
			// Ignore cleanup errors here; the startup failure is the primary error.
		}
	}
	startedAny := false
	for _, item := range BuildStartupPlan(cfg) {
		if err := ctx.Err(); err != nil {
			if startedAny {
				cleanupOnFailure()
			}
			return err
		}
		switch item.Name {
		case "socks":
			startedAny = true
			if err := r.Manager.StartSocks(cfg.Socks.Port, cfg.Socks.BindListen); err != nil {
				cleanupOnFailure()
				return err
			}
		case "http":
			startedAny = true
			if err := r.Manager.StartHTTP(cfg.HTTP.Port, cfg.HTTP.BindListen); err != nil {
				cleanupOnFailure()
				return err
			}
		case "tunnel-server":
			startedAny = true
			if err := r.Manager.StartTunnelServer(cfg.TunnelServer); err != nil {
				cleanupOnFailure()
				return err
			}
		case "tunnel-client":
			startedAny = true
			if err := r.Manager.StartTunnelClient(cfg.TunnelClient); err != nil {
				cleanupOnFailure()
				return err
			}
		}
	}

	if !cfg.Web.Enabled {
		<-ctx.Done()
		if err := r.Manager.Shutdown(); err != nil {
			return err
		}
		return ctx.Err()
	}

	webErrCh := make(chan error, 1)
	go func() {
		webErrCh <- r.Manager.StartWeb(cfg.Web.Port)
	}()

	select {
	case err := <-webErrCh:
		if err != nil && startedAny {
			cleanupOnFailure()
		}
		return err
	case <-ctx.Done():
		if err := r.Manager.Shutdown(); err != nil {
			return err
		}
		err := <-webErrCh
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return ctx.Err()
	}
}

// StartWeb starts the web manager through the existing internal/web implementation.
func (a WebManagerAdapter) StartWeb(port int) error {
	return a.Manager.StartWeb(port)
}

// StartSocks starts the SOCKS proxy through the existing internal/web implementation.
func (a WebManagerAdapter) StartSocks(port int, bindListen bool) error {
	return a.Manager.StartSocks(port, bindListen)
}

// StartHTTP starts the HTTP proxy through the existing internal/web implementation.
func (a WebManagerAdapter) StartHTTP(port int, bindListen bool) error {
	return a.Manager.StartHTTP(port, bindListen)
}

// StartTunnelServer starts the tunnel server through the existing internal/web implementation.
func (a WebManagerAdapter) StartTunnelServer(cfg TunnelServerConfig) error {
	return a.Manager.StartTunnelServerRuntime(
		cfg.Engine,
		cfg.Listen,
		cfg.PublicBind,
		cfg.Token,
		cfg.Cert,
		cfg.Key,
		cfg.AllowInsecure,
		cfg.AutoPortStart,
		cfg.AutoPortEnd,
	)
}

// StartTunnelClient starts the tunnel client through the existing internal/web implementation.
func (a WebManagerAdapter) StartTunnelClient(cfg TunnelClientConfig) error {
	return a.Manager.StartTunnelClientRuntime(
		cfg.Engine,
		cfg.Server,
		cfg.Token,
		cfg.Client,
		cfg.CA,
		cfg.ServerName,
		cfg.InsecureSkipVerify,
		cfg.AllowInsecure,
	)
}

// Shutdown stops all running services through the existing internal/web implementation.
func (a WebManagerAdapter) Shutdown() error {
	return a.Manager.ShutdownApplication()
}
