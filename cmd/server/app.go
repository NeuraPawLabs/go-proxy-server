package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/apeming/go-proxy-server/internal/auth"
	"github.com/apeming/go-proxy-server/internal/constants"
	applogger "github.com/apeming/go-proxy-server/internal/logger"
	"github.com/apeming/go-proxy-server/internal/proxy"
	runtimecfg "github.com/apeming/go-proxy-server/internal/runtime"
	"github.com/apeming/go-proxy-server/internal/service"
	"github.com/apeming/go-proxy-server/internal/tunnel"
	"github.com/apeming/go-proxy-server/internal/web"
)

type commandHandler func(*App, []string) error

type commandSpec struct {
	name  string
	usage string
	run   commandHandler
}

type parseResult struct {
	handledHelp bool
	err         error
}

type App struct {
	db     *gorm.DB
	stdout io.Writer
	stderr io.Writer
}

var commandSpecs = []commandSpec{
	{name: "adduser", usage: "adduser -username <username> -password <password>", run: (*App).handleAddUserCommand},
	{name: "deluser", usage: "deluser -username <username>", run: (*App).handleDeleteUserCommand},
	{name: "listuser", usage: "listuser", run: (*App).handleListUserCommand},
	{name: "addip", usage: "addip -ip <ip_to_add>", run: (*App).handleAddIPCommand},
	{name: "delip", usage: "delip -ip <ip_to_delete>", run: (*App).handleDeleteIPCommand},
	{name: "listip", usage: "listip", run: (*App).handleListIPCommand},
	{name: "run", usage: "run [-config <path>] [-web-port <port>] [-socks-port <port>] [-socks-bind-listen] [-http-port <port>] [-http-bind-listen]", run: (*App).handleRunCommand},
	{name: "socks", usage: "socks -port <port_number> [-bind-listen]", run: (*App).handleSocksCommand},
	{name: "http", usage: "http -port <port_number> [-bind-listen]", run: (*App).handleHTTPCommand},
	{name: "both", usage: "both -socks-port <port_number> -http-port <port_number> [-bind-listen]", run: (*App).handleBothCommand},
	{name: "web", usage: "web [-port <port_number>]  (default: random port)", run: (*App).handleWebCommand},
	{name: "tunnel-server", usage: "tunnel-server -engine <classic|quic> -listen <host:port> -token <token> (-cert <cert.pem> -key <key.pem> | -allow-insecure) [-public-bind <host>] [-auto-port-start <port> -auto-port-end <port>]", run: (*App).handleTunnelServerCommand},
	{name: "tunnel-client", usage: "tunnel-client -engine <classic|quic> -server <host:port> -token <token> -client <client_name> (-ca <ca.pem> | -insecure-skip-verify | -allow-insecure) [-server-name <name>]", run: (*App).handleTunnelClientCommand},
	{name: "tunnel-list-clients", usage: "tunnel-list-clients", run: (*App).handleTunnelListClientsCommand},
	{name: "tunnel-list-routes", usage: "tunnel-list-routes", run: (*App).handleTunnelListRoutesCommand},
	{name: "tunnel-save-route", usage: "tunnel-save-route -client <client_name> -name <route_name> -protocol <tcp|udp> -target <host:port> [-public-port <port_number>] [-enabled] [-ip-whitelist <ip_or_cidr,...>] [-udp-idle-timeout <sec>] [-udp-max-payload <bytes>]", run: (*App).handleTunnelSaveRouteCommand},
	{name: "tunnel-del-route", usage: "tunnel-del-route -client <client_name> -name <route_name>", run: (*App).handleTunnelDeleteRouteCommand},
	{name: "service", usage: "service install|uninstall|start|stop|status", run: (*App).handleServiceCommand},
}

var runConfigRuntimeFn = func(ctx context.Context, db *gorm.DB, cfg runtimecfg.Config) error {
	proxy.SetBindPolicy(proxy.BindPolicy{ExitBindings: toProxyExitBindings(cfg.ExitBindings)})
	manager := web.NewManager(db, cfg.Web.Port)
	if err := manager.SyncAuthState(); err != nil {
		return err
	}
	runner := runtimecfg.Runner{Manager: runtimecfg.NewWebManagerAdapter(manager)}
	return runner.Start(ctx, cfg)
}

func toProxyExitBindings(bindings []runtimecfg.ExitBinding) []proxy.ExitBinding {
	out := make([]proxy.ExitBinding, 0, len(bindings))
	for _, binding := range bindings {
		out = append(out, proxy.ExitBinding{
			Name:            binding.Name,
			IngressLocalIP:  binding.IngressLocalIP,
			OutboundLocalIP: binding.OutboundLocalIP,
		})
	}
	return out
}

var installServiceFn = func(spec service.ServiceSpec) error {
	return service.NewManager().Install(spec)
}

var uninstallServiceFn = func(name string) error {
	return service.NewManager().Uninstall(name)
}

var startServiceFn = func(name string) error {
	return service.NewManager().Start(name)
}

var stopServiceFn = func(name string) error {
	return service.NewManager().Stop(name)
}

var serviceStatusFn = func(name string) (service.Status, error) {
	return service.NewManager().Status(name)
}

var resolveServiceInstallConfigPathFn = resolveServiceInstallConfigPath
var lookupUserFn = user.Lookup

var commandRegistry = func() map[string]commandSpec {
	registry := make(map[string]commandSpec, len(commandSpecs))
	for _, spec := range commandSpecs {
		registry[spec.name] = spec
	}
	return registry
}()

func NewApp(db *gorm.DB, stdout, stderr io.Writer) *App {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	return &App{
		db:     db,
		stdout: stdout,
		stderr: stderr,
	}
}

func (a *App) Run(args []string) error {
	if handleGlobalCLIArgs(args, a.stdout) {
		return nil
	}
	if shouldWriteUsageForNoArgs(args) {
		writeUsage(a.stdout)
		return nil
	}
	if len(args) == 0 {
		return a.startDefaultMode()
	}

	spec, ok := commandRegistry[args[0]]
	if !ok {
		writeUsage(a.stderr)
		return fmt.Errorf("unknown command: %s", args[0])
	}

	return spec.run(a, args[1:])
}

func (a *App) startDefaultMode() error {
	applogger.Debug("Starting in default mode (no arguments)")
	applogger.Debug("Platform: %s", currentGOOS())

	if currentGOOS() == "windows" {
		applogger.Debug("Windows detected - attempting to start system tray application")
		if err := startTrayModeFn(a.db, 0); err == nil {
			return nil
		}

		applogger.Debug("Falling back to web server mode")
		fmt.Fprintln(a.stdout, "系统托盘启动失败，切换到Web服务器模式...")
		fmt.Fprintln(a.stdout, "System tray failed to start, falling back to web server mode...")
		return startWebModeFn(a.db, 0)
	}

	applogger.Debug("Non-Windows platform - starting web server directly")
	return startWebModeFn(a.db, 0)
}

func shouldWriteUsageForNoArgs(args []string) bool {
	return len(args) == 0 && currentGOOS() != "windows"
}

func newCommandFlagSet(name, usage string, stdout io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {
		writeCommandUsage(stdout, usage, fs)
	}
	return fs
}

func parseCommandFlags(fs *flag.FlagSet, args []string) parseResult {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.Usage()
			return parseResult{handledHelp: true}
		}
		return parseResult{err: err}
	}
	return parseResult{}
}

func writeCommandUsage(w io.Writer, usage string, fs *flag.FlagSet) {
	if w == nil {
		w = io.Discard
	}
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintf(w, "  go-proxy-server %s\n", usage)

	if fs == nil {
		return
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "  -h, --help")

	hasFlags := false
	fs.VisitAll(func(f *flag.Flag) {
		hasFlags = true
	})
	if !hasFlags {
		return
	}

	previousOutput := fs.Output()
	fs.SetOutput(w)
	fs.PrintDefaults()
	fs.SetOutput(previousOutput)
}

func findCommandSpec(name string) (commandSpec, bool) {
	spec, ok := commandRegistry[name]
	return spec, ok
}

func writeServiceUsage(w io.Writer) {
	if w == nil {
		w = io.Discard
	}
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  go-proxy-server service <install|uninstall|start|stop|status>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Subcommands:")
	fmt.Fprintln(w, "  go-proxy-server service install [-config <path>] [--name <service-name>]")
	fmt.Fprintln(w, "  go-proxy-server service uninstall [--name <service-name>]")
	fmt.Fprintln(w, "  go-proxy-server service start [--name <service-name>]")
	fmt.Fprintln(w, "  go-proxy-server service stop [--name <service-name>]")
	fmt.Fprintln(w, "  go-proxy-server service status [--name <service-name>]")
}

func (a *App) syncProxyRuntimeState() error {
	if err := auth.SyncState(a.db); err != nil {
		return fmt.Errorf("failed to load proxy auth state: %w", err)
	}
	auth.StartStateReloader(context.Background(), a.db, constants.ConfigReloadInterval, onAuthReloadError)
	return nil
}

func (a *App) handleAddIPCommand(args []string) error {
	fs := newCommandFlagSet("addip", "addip -ip <ip_to_add>", a.stdout)
	ip := fs.String("ip", "", "Add an IP address to the whitelist")
	result := parseCommandFlags(fs, args)
	if result.handledHelp || result.err != nil {
		return result.err
	}
	if *ip == "" {
		return fmt.Errorf("usage: proxy-server addip -ip [ip]")
	}
	if err := auth.AddIPToWhitelist(a.db, *ip); err != nil {
		return err
	}
	fmt.Fprintln(a.stdout, "Whiteip added successfully!")
	return nil
}

func (a *App) handleDeleteIPCommand(args []string) error {
	fs := newCommandFlagSet("delip", "delip -ip <ip_to_delete>", a.stdout)
	ip := fs.String("ip", "", "Delete an IP address from the whitelist")
	result := parseCommandFlags(fs, args)
	if result.handledHelp || result.err != nil {
		return result.err
	}
	if *ip == "" {
		return fmt.Errorf("usage: proxy-server delip -ip [ip]")
	}
	if err := auth.DeleteIPFromWhitelist(a.db, *ip); err != nil {
		return err
	}
	fmt.Fprintln(a.stdout, "Whiteip deleted successfully!")
	return nil
}

func (a *App) handleListIPCommand(args []string) error {
	fs := newCommandFlagSet("listip", "listip", a.stdout)
	result := parseCommandFlags(fs, args)
	if result.handledHelp || result.err != nil {
		return result.err
	}
	if err := auth.SyncState(a.db); err != nil {
		return err
	}
	return auth.WriteWhitelist(a.stdout)
}

func (a *App) handleAddUserCommand(args []string) error {
	fs := newCommandFlagSet("adduser", "adduser -username <username> -password <password>", a.stdout)
	username := fs.String("username", "", "Username to add")
	password := fs.String("password", "", "Password to add")
	connectIP := fs.String("ip", "", "Connect ip")
	result := parseCommandFlags(fs, args)
	if result.handledHelp || result.err != nil {
		return result.err
	}
	if *username == "" || *password == "" {
		return fmt.Errorf("usage: proxy-server adduser -username [username] -password [password]")
	}
	if err := auth.AddUser(a.db, *connectIP, *username, *password); err != nil {
		return err
	}
	fmt.Fprintln(a.stdout, "User added successfully!")
	return nil
}

func (a *App) handleDeleteUserCommand(args []string) error {
	fs := newCommandFlagSet("deluser", "deluser -username <username>", a.stdout)
	username := fs.String("username", "", "Username to delete")
	result := parseCommandFlags(fs, args)
	if result.handledHelp || result.err != nil {
		return result.err
	}
	if *username == "" {
		return fmt.Errorf("usage: proxy-server deluser -username [username]")
	}
	if err := auth.DeleteUser(a.db, *username); err != nil {
		return err
	}
	fmt.Fprintln(a.stdout, "User deleted successfully!")
	return nil
}

func (a *App) handleListUserCommand(args []string) error {
	fs := newCommandFlagSet("listuser", "listuser", a.stdout)
	result := parseCommandFlags(fs, args)
	if result.handledHelp || result.err != nil {
		return result.err
	}
	return auth.ListUsersToWriter(a.db, a.stdout)
}

func (a *App) handleRunCommand(args []string) error {
	fs := newCommandFlagSet("run", "run [-config <path>] [-web-port <port>] [-socks-port <port>] [-socks-bind-listen] [-http-port <port>] [-http-bind-listen]", a.stdout)
	configPath := fs.String("config", "", "Path to TOML runtime config")
	webPort := fs.Int("web-port", 0, "Override web port")
	socksPort := fs.Int("socks-port", 0, "Override SOCKS port")
	socksBind := fs.Bool("socks-bind-listen", false, "Override SOCKS bind-listen")
	httpPort := fs.Int("http-port", 0, "Override HTTP port")
	httpBind := fs.Bool("http-bind-listen", false, "Override HTTP bind-listen")
	result := parseCommandFlags(fs, args)
	if result.handledHelp || result.err != nil {
		return result.err
	}

	cfgPath := *configPath
	if strings.TrimSpace(cfgPath) == "" {
		var err error
		cfgPath, err = runtimecfg.DefaultConfigPath()
		if err != nil {
			return err
		}
	}

	cfg, err := runtimecfg.LoadConfig(cfgPath)
	if err != nil {
		return err
	}
	cfg = cfg.ApplyOverrides(runtimecfg.Overrides{
		WebPort:         optionalIntFlag(fs, "web-port", *webPort),
		SocksPort:       optionalIntFlag(fs, "socks-port", *socksPort),
		SocksBindListen: optionalBoolFlag(fs, "socks-bind-listen", *socksBind),
		HTTPPort:        optionalIntFlag(fs, "http-port", *httpPort),
		HTTPBindListen:  optionalBoolFlag(fs, "http-bind-listen", *httpBind),
	})
	if err := cfg.Validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	restoreShutdownHandler := setShutdownSignalHandler(cancel)
	defer func() {
		restoreShutdownHandler()
		cancel()
	}()

	err = runConfigRuntimeFn(ctx, a.db, cfg)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func (a *App) handleSocksCommand(args []string) error {
	fs := newCommandFlagSet("socks", "socks -port <port_number> [-bind-listen]", a.stdout)
	port := fs.Int("port", 1080, "The port number for the SOCKS5 proxy server")
	bindListen := fs.Bool("bind-listen", false, "use connect ip as output ip")
	result := parseCommandFlags(fs, args)
	if result.handledHelp || result.err != nil {
		return result.err
	}
	if err := a.syncProxyRuntimeState(); err != nil {
		return err
	}
	proxy.SetBindPolicy(proxy.BindPolicy{})
	return runProxyServer("SOCKS5", *port, *bindListen)
}

func (a *App) handleHTTPCommand(args []string) error {
	fs := newCommandFlagSet("http", "http -port <port_number> [-bind-listen]", a.stdout)
	port := fs.Int("port", 8080, "The port number for the HTTP proxy server")
	bindListen := fs.Bool("bind-listen", false, "use connect ip as output ip")
	result := parseCommandFlags(fs, args)
	if result.handledHelp || result.err != nil {
		return result.err
	}
	if err := a.syncProxyRuntimeState(); err != nil {
		return err
	}
	proxy.SetBindPolicy(proxy.BindPolicy{})
	return runProxyServer("HTTP", *port, *bindListen)
}

func (a *App) handleBothCommand(args []string) error {
	fs := newCommandFlagSet("both", "both -socks-port <port_number> -http-port <port_number> [-bind-listen]", a.stdout)
	socksPort := fs.Int("socks-port", 1080, "The port number for the SOCKS5 proxy server")
	httpPort := fs.Int("http-port", 8080, "The port number for the HTTP proxy server")
	bindListen := fs.Bool("bind-listen", false, "use connect ip as output ip")
	result := parseCommandFlags(fs, args)
	if result.handledHelp || result.err != nil {
		return result.err
	}
	if err := a.syncProxyRuntimeState(); err != nil {
		return err
	}
	proxy.SetBindPolicy(proxy.BindPolicy{})

	errChan := make(chan error, 2)
	go func() {
		if err := runProxyServer("SOCKS5", *socksPort, *bindListen); err != nil {
			errChan <- fmt.Errorf("SOCKS5: %w", err)
		}
	}()
	go func() {
		if err := runProxyServer("HTTP", *httpPort, *bindListen); err != nil {
			errChan <- fmt.Errorf("HTTP: %w", err)
		}
	}()

	return <-errChan
}

func (a *App) handleWebCommand(args []string) error {
	fs := newCommandFlagSet("web", "web [-port <port_number>]  (default: random port)", a.stdout)
	port := fs.Int("port", 0, "The port number for the web management interface (0 for random port)")
	result := parseCommandFlags(fs, args)
	if result.handledHelp || result.err != nil {
		return result.err
	}
	return startWebMode(a.db, *port)
}

func (a *App) handleTunnelServerCommand(args []string) error {
	fs := newCommandFlagSet("tunnel-server", "tunnel-server -engine <classic|quic> -listen <host:port> -token <token> (-cert <cert.pem> -key <key.pem> | -allow-insecure) [-public-bind <host>] [-auto-port-start <port> -auto-port-end <port>]", a.stdout)
	engine := fs.String("engine", tunnel.EngineClassic, "Tunnel engine: classic or quic")
	listenAddr := fs.String("listen", ":7000", "The control listener address for tunnel clients")
	publicBind := fs.String("public-bind", "0.0.0.0", "The bind address used for exposed public ports")
	autoPortStart := fs.Int("auto-port-start", 0, "Start of the auto-assigned public port range for managed routes")
	autoPortEnd := fs.Int("auto-port-end", 0, "End of the auto-assigned public port range for managed routes")
	token := fs.String("token", "", "Shared secret for tunnel clients")
	certFile := fs.String("cert", "", "TLS certificate path for the tunnel server")
	keyFile := fs.String("key", "", "TLS private key path for the tunnel server")
	allowInsecure := fs.Bool("allow-insecure", false, "Allow plaintext tunnel traffic (unsafe, testing only)")
	result := parseCommandFlags(fs, args)
	if result.handledHelp || result.err != nil {
		return result.err
	}
	if *engine != tunnel.EngineClassic && *engine != tunnel.EngineQUIC {
		return fmt.Errorf("unsupported tunnel engine: %s", *engine)
	}
	if *token == "" {
		return fmt.Errorf("usage: proxy-server tunnel-server -engine [classic|quic] -listen [host:port] -token [token] (-cert [cert.pem] -key [key.pem] | -allow-insecure) [-public-bind host] [-auto-port-start port -auto-port-end port]")
	}

	tlsConfig, err := loadTunnelServerTLSConfig(*certFile, *keyFile, *allowInsecure)
	if err != nil {
		return err
	}

	var waitFn func() error
	var actualAddr string
	switch *engine {
	case tunnel.EngineQUIC:
		server := tunnel.NewQUICManagedServer(a.db, *listenAddr, *publicBind, *token)
		server.TLSConfig = tlsConfig
		server.AutoPortRangeStart = *autoPortStart
		server.AutoPortRangeEnd = *autoPortEnd
		if err := server.Start(context.Background()); err != nil {
			return err
		}
		waitFn = server.Wait
		actualAddr = server.GetControlAddr()
	default:
		server := tunnel.NewManagedServer(a.db, *listenAddr, *publicBind, *token)
		server.TLSConfig = tlsConfig
		server.AllowInsecure = *allowInsecure
		server.AutoPortRangeStart = *autoPortStart
		server.AutoPortRangeEnd = *autoPortEnd
		if err := server.Start(context.Background()); err != nil {
			return err
		}
		waitFn = server.Wait
		actualAddr = server.GetControlAddr()
	}
	fmt.Fprintf(a.stdout, "Tunnel server (%s) listening on %s\n", *engine, actualAddr)
	return waitFn()
}

func (a *App) handleTunnelClientCommand(args []string) error {
	fs := newCommandFlagSet("tunnel-client", "tunnel-client -engine <classic|quic> -server <host:port> -token <token> -client <client_name> (-ca <ca.pem> | -insecure-skip-verify | -allow-insecure) [-server-name <name>]", a.stdout)
	engine := fs.String("engine", tunnel.EngineClassic, "Tunnel engine: classic or quic")
	serverAddr := fs.String("server", "", "The public tunnel server address")
	token := fs.String("token", "", "Shared secret for tunnel server")
	clientName := fs.String("client", "", "The tunnel client name")
	caFile := fs.String("ca", "", "CA certificate used to verify the tunnel server")
	serverName := fs.String("server-name", "", "Expected TLS server name")
	insecureSkipVerify := fs.Bool("insecure-skip-verify", false, "Skip TLS certificate verification (unsafe)")
	allowInsecure := fs.Bool("allow-insecure", false, "Allow plaintext tunnel traffic (unsafe, testing only)")
	result := parseCommandFlags(fs, args)
	if result.handledHelp || result.err != nil {
		return result.err
	}
	if *engine != tunnel.EngineClassic && *engine != tunnel.EngineQUIC {
		return fmt.Errorf("unsupported tunnel engine: %s", *engine)
	}
	if *serverAddr == "" || *token == "" || *clientName == "" {
		return fmt.Errorf("usage: proxy-server tunnel-client -engine [classic|quic] -server [host:port] -token [token] -client [client_name] (-ca [ca.pem] | -insecure-skip-verify | -allow-insecure) [-server-name name]")
	}

	tlsConfig, err := loadTunnelClientTLSConfig(*serverAddr, *caFile, *serverName, *insecureSkipVerify, *allowInsecure)
	if err != nil {
		return err
	}

	switch *engine {
	case tunnel.EngineQUIC:
		client := tunnel.NewQUICManagedClient(*serverAddr, *token, *clientName)
		client.TLSConfig = tlsConfig
		client.OnConnected = func(name string) {
			fmt.Fprintf(a.stdout, "QUIC tunnel client %s connected, waiting for route assignments...\n", name)
		}
		return client.Run(context.Background())
	default:
		client := tunnel.NewManagedClient(*serverAddr, *token, *clientName)
		client.TLSConfig = tlsConfig
		client.AllowInsecure = *allowInsecure
		client.OnConnected = func(name string) {
			fmt.Fprintf(a.stdout, "Tunnel client %s connected, waiting for route assignments...\n", name)
		}
		return client.Run(context.Background())
	}
}

func (a *App) handleTunnelListClientsCommand(args []string) error {
	fs := newCommandFlagSet("tunnel-list-clients", "tunnel-list-clients", a.stdout)
	result := parseCommandFlags(fs, args)
	if result.handledHelp || result.err != nil {
		return result.err
	}

	store := tunnel.NewManagedStore(a.db)
	clients, err := store.ListClients()
	if err != nil {
		return err
	}
	if len(clients) == 0 {
		fmt.Fprintln(a.stdout, "No tunnel clients found.")
		return nil
	}

	for _, client := range clients {
		lastSeen := "-"
		if client.LastSeenAt != nil {
			lastSeen = client.LastSeenAt.Format(time.RFC3339)
		}
		fmt.Fprintf(a.stdout, "%s\tengine=%s\tconnected=%t\tremote=%s\tlastSeen=%s\n", client.Name, client.Engine, client.Connected, client.RemoteAddr, lastSeen)
	}
	return nil
}

func (a *App) handleTunnelListRoutesCommand(args []string) error {
	fs := newCommandFlagSet("tunnel-list-routes", "tunnel-list-routes", a.stdout)
	result := parseCommandFlags(fs, args)
	if result.handledHelp || result.err != nil {
		return result.err
	}

	store := tunnel.NewManagedStore(a.db)
	routes, err := store.ListRoutes()
	if err != nil {
		return err
	}
	if len(routes) == 0 {
		fmt.Fprintln(a.stdout, "No tunnel routes found.")
		return nil
	}

	for _, route := range routes {
		whitelist := strings.Join(tunnel.ParseStoredIPWhitelist(route.IPWhitelist), ",")
		if whitelist == "" {
			whitelist = "-"
		}
		fmt.Fprintf(a.stdout, "%s/%s\tprotocol=%s\tenabled=%t\ttarget=%s\tpublic=%d\tactive=%d\twhitelist=%s\tlastError=%s\n",
			route.ClientName, route.Name, route.Protocol, route.Enabled, route.TargetAddr, route.PublicPort, route.ActivePublicPort, whitelist, route.LastError)
	}
	return nil
}

func (a *App) handleTunnelSaveRouteCommand(args []string) error {
	fs := newCommandFlagSet("tunnel-save-route", "tunnel-save-route -client <client_name> -name <route_name> -protocol <tcp|udp> -target <host:port> [-public-port <port_number>] [-enabled] [-ip-whitelist <ip_or_cidr,...>] [-udp-idle-timeout <sec>] [-udp-max-payload <bytes>]", a.stdout)
	clientName := fs.String("client", "", "Tunnel client name")
	routeName := fs.String("name", "", "Tunnel route name")
	protocol := fs.String("protocol", tunnel.ProtocolTCP, "Tunnel protocol: tcp or udp")
	targetAddr := fs.String("target", "", "Local target address on the client")
	publicPort := fs.Int("public-port", 0, "Public port to expose (0 for auto-assign)")
	ipWhitelist := fs.String("ip-whitelist", "", "Comma-separated public source IP/CIDR whitelist")
	udpIdleTimeout := fs.Int("udp-idle-timeout", tunnel.DefaultManagedUDPIdleTimeoutSec, "UDP idle session timeout in seconds")
	udpMaxPayload := fs.Int("udp-max-payload", tunnel.DefaultManagedUDPMaxPayload, "Maximum UDP payload size in bytes")
	enabled := fs.Bool("enabled", true, "Whether the route is enabled")
	result := parseCommandFlags(fs, args)
	if result.handledHelp || result.err != nil {
		return result.err
	}

	store := tunnel.NewManagedStore(a.db)
	if err := store.SaveRouteWithOptions(*clientName, *routeName, *targetAddr, *publicPort, *enabled, parseCSVFlag(*ipWhitelist), *protocol, *udpIdleTimeout, *udpMaxPayload); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Tunnel route %s/%s saved.\n", *clientName, *routeName)
	return nil
}

func (a *App) handleTunnelDeleteRouteCommand(args []string) error {
	fs := newCommandFlagSet("tunnel-del-route", "tunnel-del-route -client <client_name> -name <route_name>", a.stdout)
	clientName := fs.String("client", "", "Tunnel client name")
	routeName := fs.String("name", "", "Tunnel route name")
	result := parseCommandFlags(fs, args)
	if result.handledHelp || result.err != nil {
		return result.err
	}

	store := tunnel.NewManagedStore(a.db)
	if err := store.DeleteRoute(*clientName, *routeName); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Tunnel route %s/%s deleted.\n", *clientName, *routeName)
	return nil
}

func printUsage() {
	writeUsage(os.Stdout)
}

func writeUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  go-proxy-server [--help] [--version]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Global options:")
	fmt.Fprintln(w, "  help, -h, --help       Show this help message")
	fmt.Fprintln(w, "  version, -v, --version Show version information")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	for _, spec := range commandSpecs {
		fmt.Fprintf(w, "  %s\n", spec.usage)
	}
}

func parseCSVFlag(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}

func optionalInt(value int) *int {
	if value == 0 {
		return nil
	}
	v := value
	return &v
}

func optionalIntFlag(fs *flag.FlagSet, name string, value int) *int {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	if !set {
		return nil
	}
	v := value
	return &v
}

func optionalBoolFlag(fs *flag.FlagSet, name string, value bool) *bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	if !set {
		return nil
	}
	v := value
	return &v
}

func (a *App) handleServiceCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: go-proxy-server service <install|uninstall|start|stop|status>")
	}
	if args[0] == "-h" || args[0] == "--help" {
		writeServiceUsage(a.stdout)
		return nil
	}

	switch args[0] {
	case "install":
		fs := newCommandFlagSet("service install", "service install [-config <path>] [--name <service-name>]", a.stdout)
		configPath := fs.String("config", "", "Runtime config path")
		name := fs.String("name", service.DefaultServiceName, "Service name")
		result := parseCommandFlags(fs, args[1:])
		if result.handledHelp || result.err != nil {
			return result.err
		}
		execPath, err := os.Executable()
		if err != nil {
			return err
		}
		workDir, err := os.Getwd()
		if err != nil {
			return err
		}
		if err := service.ValidateName(*name); err != nil {
			return err
		}
		effectiveConfigPath := strings.TrimSpace(*configPath)
		if effectiveConfigPath == "" && currentGOOS() == "linux" {
			effectiveConfigPath, err = resolveServiceInstallConfigPathFn(currentGOOS())
			if err != nil {
				return err
			}
		}
		spec := service.BuildRunSpec(execPath, workDir, effectiveConfigPath)
		spec.Name = *name
		return installServiceFn(spec)
	case "uninstall":
		name, handledHelp, err := parseServiceName(args[1:], "service uninstall [--name <service-name>]", a.stdout)
		if err != nil {
			return err
		}
		if handledHelp {
			return nil
		}
		return uninstallServiceFn(name)
	case "start":
		name, handledHelp, err := parseServiceName(args[1:], "service start [--name <service-name>]", a.stdout)
		if err != nil {
			return err
		}
		if handledHelp {
			return nil
		}
		return startServiceFn(name)
	case "stop":
		name, handledHelp, err := parseServiceName(args[1:], "service stop [--name <service-name>]", a.stdout)
		if err != nil {
			return err
		}
		if handledHelp {
			return nil
		}
		return stopServiceFn(name)
	case "status":
		name, handledHelp, err := parseServiceName(args[1:], "service status [--name <service-name>]", a.stdout)
		if err != nil {
			return err
		}
		if handledHelp {
			return nil
		}
		status, err := serviceStatusFn(name)
		if err != nil {
			return err
		}
		if detail := compactStatusDetail(status.Detail); detail != "" {
			fmt.Fprintf(a.stdout, "%s\tstate=%s\tenabled=%t\trunning=%t\tdetail=%s\n", status.Name, status.State, status.Enabled, status.Running, detail)
			return nil
		}
		fmt.Fprintf(a.stdout, "%s\tstate=%s\tenabled=%t\trunning=%t\n", status.Name, status.State, status.Enabled, status.Running)
		return nil
	default:
		return fmt.Errorf("unknown service subcommand: %s", args[0])
	}
}

func parseServiceName(args []string, usage string, stdout io.Writer) (string, bool, error) {
	fs := newCommandFlagSet("service name", usage, stdout)
	name := fs.String("name", service.DefaultServiceName, "Service name")
	result := parseCommandFlags(fs, args)
	if result.handledHelp || result.err != nil {
		return "", result.handledHelp, result.err
	}
	if err := service.ValidateName(*name); err != nil {
		return "", false, err
	}
	return *name, false, nil
}

func compactStatusDetail(detail string) string {
	lines := strings.Split(detail, "\n")
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts = append(parts, line)
		if len(parts) == 3 {
			break
		}
	}
	if len(parts) == 0 {
		return ""
	}
	if len(parts) < len(nonEmptyStatusLines(lines)) {
		parts = append(parts, "...")
	}
	return strings.Join(parts, " | ")
}

func nonEmptyStatusLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

func resolveServiceInstallConfigPath(goos string) (string, error) {
	if goos != "linux" {
		return "", nil
	}

	if xdgConfigHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdgConfigHome != "" {
		return filepath.Join(xdgConfigHome, "go-proxy-server", "config.toml"), nil
	}

	sudoUser := strings.TrimSpace(os.Getenv("SUDO_USER"))
	if sudoUser == "" || sudoUser == "root" {
		return runtimecfg.DefaultConfigPath()
	}

	account, err := lookupUserFn(sudoUser)
	if err != nil {
		return "", fmt.Errorf("resolve sudo user %q: %w", sudoUser, err)
	}
	if strings.TrimSpace(account.HomeDir) == "" {
		return "", fmt.Errorf("resolve sudo user %q home directory: empty", sudoUser)
	}
	return filepath.Join(account.HomeDir, ".config", "go-proxy-server", "config.toml"), nil
}
