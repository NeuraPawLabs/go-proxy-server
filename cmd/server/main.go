package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/apeming/go-proxy-server/internal/activity"
	"github.com/apeming/go-proxy-server/internal/config"
	"github.com/apeming/go-proxy-server/internal/constants"
	applogger "github.com/apeming/go-proxy-server/internal/logger"
	"github.com/apeming/go-proxy-server/internal/metrics"
	"github.com/apeming/go-proxy-server/internal/models"
	"github.com/apeming/go-proxy-server/internal/proxy"
	"github.com/apeming/go-proxy-server/internal/singleinstance"
	"github.com/apeming/go-proxy-server/internal/tray"
	"github.com/apeming/go-proxy-server/internal/web"
)

type Bootstrap struct {
	App             *App
	releaseInstance func()
}

var (
	ensureSingleInstanceFn = ensureSingleInstance
	loadDotEnvFn           = loadDotEnv
	loadConfigFn           = config.Load
	initLoggerFn           = applogger.Init
	setupCleanupHandlerFn  = setupCleanupHandler
	initializeDatabaseFn   = initializeDatabase
	currentGOOS            = func() string { return runtime.GOOS }
	startTrayModeFn        = startTrayMode
	startWebModeFn         = startWebMode
	singleInstanceCheckFn  = singleinstance.Check
	currentArgsFn          = func() []string { return os.Args }
	waitForExitAckFn       = func() {
		_, _ = fmt.Fscanln(os.Stdin)
	}
)

func setupCleanupHandler() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		applogger.Info("Received shutdown signal, cleaning up...")
		proxy.CloseAllTransports()
		applogger.Info("All transport connections closed")
		applogger.Close()
		os.Exit(0)
	}()
}

func onAuthReloadError(err error) {
	applogger.Error("Failed to reload auth state: %v", err)
}

func isListenerClosed(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "use of closed network connection") ||
		strings.Contains(errStr, "listener closed")
}

func runProxyServer(proxyType string, port int, bindListen bool) error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to start %s listener: %w", proxyType, err)
	}
	defer listener.Close()

	applogger.Info("%s proxy server started on port %d", proxyType, port)

	consecutiveErrors := 0
	for {
		conn, err := listener.Accept()
		if err != nil {
			if isListenerClosed(err) {
				applogger.Info("%s proxy server stopped", proxyType)
				return nil
			}

			applogger.Error("%s accept failed: %v", proxyType, err)
			consecutiveErrors++
			if consecutiveErrors >= constants.MaxConsecutiveAcceptErrors {
				return fmt.Errorf("%s proxy: too many consecutive accept errors", proxyType)
			}
			time.Sleep(constants.AcceptErrorBackoff)
			continue
		}

		consecutiveErrors = 0

		switch proxyType {
		case "SOCKS5":
			go proxy.HandleSocks5Connection(conn, bindListen)
		case "HTTP":
			go proxy.HandleHTTPConnection(conn, bindListen)
		default:
			conn.Close()
			return fmt.Errorf("unsupported proxy type: %s", proxyType)
		}
	}
}

func ensureSingleInstance(stdout, stderr io.Writer) (func(), bool) {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}

	if currentGOOS() != "windows" || len(currentArgsFn()) != 1 {
		return nil, true
	}

	isOnly, err := singleInstanceCheckFn("Global\\GoProxyServerInstance")
	if err != nil {
		applogger.Error("Failed to check single instance: %v", err)
		fmt.Fprintf(stderr, "警告: 无法检查是否已有实例运行: %v\n", err)
		return nil, true
	}
	if !isOnly {
		applogger.Info("Another instance is already running, exiting")
		fmt.Fprintln(stdout, "======================================")
		fmt.Fprintln(stdout, "检测到程序已在运行!")
		fmt.Fprintln(stdout, "Another instance is already running!")
		fmt.Fprintln(stdout, "======================================")
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "请检查系统托盘（任务栏右下角）是否已有图标。")
		fmt.Fprintln(stdout, "Please check the system tray (bottom-right of taskbar) for the application icon.")
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "按任意键退出... Press any key to exit...")
		waitForExitAckFn()
		return nil, false
	}

	applogger.Info("Single instance check passed")
	return singleinstance.Release, true
}

func initializeDatabase() (*gorm.DB, error) {
	dbPath, err := config.GetDbPath()
	if err != nil {
		return nil, fmt.Errorf("failed to get database path: %w", err)
	}
	applogger.Info("Config loaded - DB: %s", dbPath)

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	applogger.Info("Database opened successfully")

	err = db.AutoMigrate(
		&models.User{},
		&models.Whitelist{},
		&models.ProxyConfig{},
		&models.SystemConfig{},
		&models.TunnelClient{},
		&models.TunnelRoute{},
		&models.MetricsSnapshot{},
		&models.AlertConfig{},
		&models.AlertHistory{},
		&models.AuditLog{},
		&models.EventLog{},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}
	if err := models.EnsureTunnelConstraints(db); err != nil {
		return nil, fmt.Errorf("failed to apply tunnel constraints: %w", err)
	}
	applogger.Info("Database migration completed")

	metrics.InitCollector(db, 10*time.Second)
	applogger.Info("Metrics collector initialized")

	if err := config.InitTimeout(db); err != nil {
		return nil, fmt.Errorf("failed to initialize timeout configuration: %w", err)
	}
	applogger.Info("Timeout configuration initialized")
	config.StartTimeoutReloader(db)

	if err := config.InitLimiterConfig(db); err != nil {
		return nil, fmt.Errorf("failed to initialize connection limiter configuration: %w", err)
	}
	applogger.Info("Connection limiter configuration initialized")

	if err := config.InitSecurityConfig(db); err != nil {
		return nil, fmt.Errorf("failed to initialize security configuration: %w", err)
	}
	applogger.Info("Security configuration initialized")

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}
	sqlDB.SetMaxIdleConns(constants.DBMaxIdleConns)
	sqlDB.SetMaxOpenConns(constants.DBMaxOpenConns)
	sqlDB.SetConnMaxLifetime(constants.DBConnMaxLifetime)
	applogger.Info("Database connection pool configured")

	activity.SetRecorder(activity.NewDBRecorder(db, 2048))

	return db, nil
}

func NewBootstrap(stdout, stderr io.Writer) (*Bootstrap, bool, error) {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}

	releaseInstance, ok := ensureSingleInstanceFn(stdout, stderr)
	if !ok {
		return nil, true, nil
	}

	cleanupOnError := func() {
		if releaseInstance != nil {
			releaseInstance()
		}
		applogger.Close()
	}

	if err := loadDotEnvFn(); err != nil {
		cleanupOnError()
		return nil, false, fmt.Errorf("dotenv error: %w", err)
	}

	if err := loadConfigFn(); err != nil {
		cleanupOnError()
		return nil, false, fmt.Errorf("config error: %w", err)
	}

	if err := initLoggerFn(); err != nil {
		fmt.Fprintf(stderr, "Failed to initialize logger: %v\n", err)
	}

	setupCleanupHandlerFn()
	applogger.Info("Go Proxy Server starting...")
	applogger.Info("Command line arguments: %v", currentArgsFn())
	applogger.Info("Number of arguments: %d", len(currentArgsFn()))

	db, err := initializeDatabaseFn()
	if err != nil {
		cleanupOnError()
		return nil, false, fmt.Errorf("initialization failed: %w", err)
	}

	return &Bootstrap{
		App:             NewApp(db, stdout, stderr),
		releaseInstance: releaseInstance,
	}, false, nil
}

func (b *Bootstrap) Close() {
	if b == nil {
		return
	}
	_ = activity.Close(context.Background())
	if b.releaseInstance != nil {
		b.releaseInstance()
	}
	applogger.Close()
}

func startWebMode(db *gorm.DB, port int) error {
	webManager := web.NewManager(db, port)
	if err := webManager.SyncAuthState(); err != nil {
		return err
	}
	webManager.StartConfiguredProxies()
	webManager.StartConfiguredTunnelServer()
	webManager.StartConfiguredTunnelClient()
	return webManager.StartServer()
}

func startTrayMode(db *gorm.DB, port int) (err error) {
	defer func() {
		if r := recover(); r != nil {
			applogger.Error("System tray panic recovered in main: %v", r)
			err = fmt.Errorf("system tray panic: %v", r)
		}
	}()
	return tray.Start(db, port)
}

func main() {
	applogger.InitStdout()
	flag.Usage = printUsage

	bootstrap, handled, err := NewBootstrap(os.Stdout, os.Stderr)
	if handled {
		return
	}
	if err != nil {
		applogger.Error("Bootstrap failed: %v", err)
		return
	}
	defer bootstrap.Close()

	args := currentArgsFn()
	if err := bootstrap.App.Run(args[1:]); err != nil {
		applogger.Error("Command failed: %v", err)
		if !strings.HasPrefix(err.Error(), "unknown command:") {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
	}
}
