package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/apeming/go-proxy-server/internal/auth"
	"github.com/apeming/go-proxy-server/internal/constants"
	applogger "github.com/apeming/go-proxy-server/internal/logger"
	"github.com/apeming/go-proxy-server/internal/tunnel"
)

type commandHandler func(*App, []string) error

type commandSpec struct {
	name  string
	usage string
	run   commandHandler
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
}

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

func (a *App) syncProxyRuntimeState() error {
	if err := auth.SyncState(a.db); err != nil {
		return fmt.Errorf("failed to load proxy auth state: %w", err)
	}
	auth.StartStateReloader(context.Background(), a.db, constants.ConfigReloadInterval, onAuthReloadError)
	return nil
}

func (a *App) handleAddIPCommand(args []string) error {
	fs := flag.NewFlagSet("addip", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	ip := fs.String("ip", "", "Add an IP address to the whitelist")
	if err := fs.Parse(args); err != nil {
		return err
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
	fs := flag.NewFlagSet("delip", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	ip := fs.String("ip", "", "Delete an IP address from the whitelist")
	if err := fs.Parse(args); err != nil {
		return err
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
	fs := flag.NewFlagSet("listip", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := auth.SyncState(a.db); err != nil {
		return err
	}
	return auth.WriteWhitelist(a.stdout)
}

func (a *App) handleAddUserCommand(args []string) error {
	fs := flag.NewFlagSet("adduser", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	username := fs.String("username", "", "Username to add")
	password := fs.String("password", "", "Password to add")
	connectIP := fs.String("ip", "", "Connect ip")
	if err := fs.Parse(args); err != nil {
		return err
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
	fs := flag.NewFlagSet("deluser", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	username := fs.String("username", "", "Username to delete")
	if err := fs.Parse(args); err != nil {
		return err
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
	fs := flag.NewFlagSet("listuser", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return err
	}
	return auth.ListUsersToWriter(a.db, a.stdout)
}

func (a *App) handleSocksCommand(args []string) error {
	fs := flag.NewFlagSet("socks", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 1080, "The port number for the SOCKS5 proxy server")
	bindListen := fs.Bool("bind-listen", false, "use connect ip as output ip")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := a.syncProxyRuntimeState(); err != nil {
		return err
	}
	return runProxyServer("SOCKS5", *port, *bindListen)
}

func (a *App) handleHTTPCommand(args []string) error {
	fs := flag.NewFlagSet("http", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 8080, "The port number for the HTTP proxy server")
	bindListen := fs.Bool("bind-listen", false, "use connect ip as output ip")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := a.syncProxyRuntimeState(); err != nil {
		return err
	}
	return runProxyServer("HTTP", *port, *bindListen)
}

func (a *App) handleBothCommand(args []string) error {
	fs := flag.NewFlagSet("both", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	socksPort := fs.Int("socks-port", 1080, "The port number for the SOCKS5 proxy server")
	httpPort := fs.Int("http-port", 8080, "The port number for the HTTP proxy server")
	bindListen := fs.Bool("bind-listen", false, "use connect ip as output ip")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := a.syncProxyRuntimeState(); err != nil {
		return err
	}

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
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 0, "The port number for the web management interface (0 for random port)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return startWebMode(a.db, *port)
}

func (a *App) handleTunnelServerCommand(args []string) error {
	fs := flag.NewFlagSet("tunnel-server", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	engine := fs.String("engine", tunnel.EngineClassic, "Tunnel engine: classic or quic")
	listenAddr := fs.String("listen", ":7000", "The control listener address for tunnel clients")
	publicBind := fs.String("public-bind", "0.0.0.0", "The bind address used for exposed public ports")
	autoPortStart := fs.Int("auto-port-start", 0, "Start of the auto-assigned public port range for managed routes")
	autoPortEnd := fs.Int("auto-port-end", 0, "End of the auto-assigned public port range for managed routes")
	token := fs.String("token", "", "Shared secret for tunnel clients")
	certFile := fs.String("cert", "", "TLS certificate path for the tunnel server")
	keyFile := fs.String("key", "", "TLS private key path for the tunnel server")
	allowInsecure := fs.Bool("allow-insecure", false, "Allow plaintext tunnel traffic (unsafe, testing only)")
	if err := fs.Parse(args); err != nil {
		return err
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
	fs := flag.NewFlagSet("tunnel-client", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	engine := fs.String("engine", tunnel.EngineClassic, "Tunnel engine: classic or quic")
	serverAddr := fs.String("server", "", "The public tunnel server address")
	token := fs.String("token", "", "Shared secret for tunnel server")
	clientName := fs.String("client", "", "The tunnel client name")
	caFile := fs.String("ca", "", "CA certificate used to verify the tunnel server")
	serverName := fs.String("server-name", "", "Expected TLS server name")
	insecureSkipVerify := fs.Bool("insecure-skip-verify", false, "Skip TLS certificate verification (unsafe)")
	allowInsecure := fs.Bool("allow-insecure", false, "Allow plaintext tunnel traffic (unsafe, testing only)")
	if err := fs.Parse(args); err != nil {
		return err
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
	fs := flag.NewFlagSet("tunnel-list-clients", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return err
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
	fs := flag.NewFlagSet("tunnel-list-routes", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return err
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
	fs := flag.NewFlagSet("tunnel-save-route", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	clientName := fs.String("client", "", "Tunnel client name")
	routeName := fs.String("name", "", "Tunnel route name")
	protocol := fs.String("protocol", tunnel.ProtocolTCP, "Tunnel protocol: tcp or udp")
	targetAddr := fs.String("target", "", "Local target address on the client")
	publicPort := fs.Int("public-port", 0, "Public port to expose (0 for auto-assign)")
	ipWhitelist := fs.String("ip-whitelist", "", "Comma-separated public source IP/CIDR whitelist")
	udpIdleTimeout := fs.Int("udp-idle-timeout", tunnel.DefaultManagedUDPIdleTimeoutSec, "UDP idle session timeout in seconds")
	udpMaxPayload := fs.Int("udp-max-payload", tunnel.DefaultManagedUDPMaxPayload, "Maximum UDP payload size in bytes")
	enabled := fs.Bool("enabled", true, "Whether the route is enabled")
	if err := fs.Parse(args); err != nil {
		return err
	}

	store := tunnel.NewManagedStore(a.db)
	if err := store.SaveRouteWithOptions(*clientName, *routeName, *targetAddr, *publicPort, *enabled, parseCSVFlag(*ipWhitelist), *protocol, *udpIdleTimeout, *udpMaxPayload); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Tunnel route %s/%s saved.\n", *clientName, *routeName)
	return nil
}

func (a *App) handleTunnelDeleteRouteCommand(args []string) error {
	fs := flag.NewFlagSet("tunnel-del-route", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	clientName := fs.String("client", "", "Tunnel client name")
	routeName := fs.String("name", "", "Tunnel route name")
	if err := fs.Parse(args); err != nil {
		return err
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
