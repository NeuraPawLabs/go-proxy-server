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
	"sync"
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
	closeAllTransportsFn   = proxy.CloseAllTransports
	exitProcessFn          = os.Exit
	waitForExitAckFn       = func() {
		_, _ = fmt.Fscanln(os.Stdin)
	}
	shutdownSignalHandlerMu sync.RWMutex
	shutdownSignalHandlerFn = shutdownService
)

func setupCleanupHandler() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		invokeShutdownSignalHandler()
	}()
}

func setShutdownSignalHandler(handler func()) func() {
	if handler == nil {
		handler = shutdownService
	}

	shutdownSignalHandlerMu.Lock()
	previous := shutdownSignalHandlerFn
	shutdownSignalHandlerFn = handler
	shutdownSignalHandlerMu.Unlock()

	return func() {
		shutdownSignalHandlerMu.Lock()
		shutdownSignalHandlerFn = previous
		shutdownSignalHandlerMu.Unlock()
	}
}

func invokeShutdownSignalHandler() {
	shutdownSignalHandlerMu.RLock()
	handler := shutdownSignalHandlerFn
	shutdownSignalHandlerMu.RUnlock()
	if handler == nil {
		handler = shutdownService
	}
	handler()
}

func shutdownService() {
	applogger.Info("Shutting down...")
	closeAllTransportsFn()
	applogger.Close()
	exitProcessFn(0)
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
	proxy.LogBindListenStartupDiagnostics(proxyType, port, bindListen)

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
	applogger.Debug("Config loaded - DB: %s", dbPath)

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	applogger.Debug("Database opened successfully")

	err = migrateDatabase(db)
	if err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}
	applogger.Debug("Database migration completed")

	metrics.InitCollector(db, 10*time.Second)
	applogger.Debug("Metrics collector initialized")

	if err := config.InitTimeout(db); err != nil {
		return nil, fmt.Errorf("failed to initialize timeout configuration: %w", err)
	}
	applogger.Debug("Timeout configuration initialized")
	config.StartTimeoutReloader(db)

	if err := config.InitLimiterConfig(db); err != nil {
		return nil, fmt.Errorf("failed to initialize connection limiter configuration: %w", err)
	}
	applogger.Debug("Connection limiter configuration initialized")

	if err := config.InitSecurityConfig(db); err != nil {
		return nil, fmt.Errorf("failed to initialize security configuration: %w", err)
	}
	applogger.Debug("Security configuration initialized")

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}
	sqlDB.SetMaxIdleConns(constants.DBMaxIdleConns)
	sqlDB.SetMaxOpenConns(constants.DBMaxOpenConns)
	sqlDB.SetConnMaxLifetime(constants.DBConnMaxLifetime)
	applogger.Debug("Database connection pool configured")

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
	if shouldWriteUsageForNoArgs(currentArgsFn()[1:]) {
		writeUsage(stdout)
		return nil, true, nil
	}
	if handleGlobalCLIArgs(currentArgsFn()[1:], stdout) {
		return nil, true, nil
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
	applogger.Debug("Go Proxy Server starting...")
	applogger.Debug("Command line arguments: %v", currentArgsFn())
	applogger.Debug("Number of arguments: %d", len(currentArgsFn()))

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

func reportBootstrapFailure(stderr io.Writer, err error) {
	if err == nil {
		return
	}
	if stderr == nil {
		stderr = io.Discard
	}
	fmt.Fprintf(stderr, "Error: %v\n", err)
	applogger.Error("Bootstrap failed: %v", err)
}

func main() {
	applogger.InitStdout()
	flag.Usage = printUsage
	applogger.SetLevel(bootstrapLogLevel(currentArgsFn()[1:]))

	bootstrap, handled, err := NewBootstrap(os.Stdout, os.Stderr)
	if handled {
		return
	}
	if err != nil {
		reportBootstrapFailure(os.Stderr, err)
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
