//go:build windows
// +build windows

package tray

import (
	_ "embed"
	"fmt"
	"os/exec"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/getlantern/systray"
	"gorm.io/gorm"

	"github.com/apeming/go-proxy-server/internal/logger"
	"github.com/apeming/go-proxy-server/internal/web"
)

//go:embed icon.ico
var iconData []byte

var globalDB *gorm.DB
var globalWebManager *web.Manager
var actualWebPort int                     // Store actual port after binding
var trayReady = make(chan bool, 1)        // Signal when tray is ready
var shutdownComplete = make(chan bool, 1) // Signal when shutdown is complete

var (
	user32         = syscall.NewLazyDLL("user32.dll")
	messageBoxW    = user32.NewProc("MessageBoxW")
	MB_OK          = 0x00000000
	MB_ICONERROR   = 0x00000010
	MB_ICONWARNING = 0x00000030
	MB_ICONINFO    = 0x00000040
)

// showMessageBox displays a Windows message box
func showMessageBox(title, message string, icon int) {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	messagePtr, _ := syscall.UTF16PtrFromString(message)
	messageBoxW.Call(0, uintptr(unsafe.Pointer(messagePtr)), uintptr(unsafe.Pointer(titlePtr)), uintptr(MB_OK|icon))
}

// Start starts the system tray application with timeout detection
// Returns error if tray initialization fails or times out
func Start(db *gorm.DB, webPort int) error {
	globalDB = db
	logger.Info("Starting system tray application...")

	// Add panic recovery
	defer func() {
		if r := recover(); r != nil {
			logger.Error("System tray panic: %v", r)
			errMsg := fmt.Sprintf("系统托盘初始化失败: %v\n\n程序将以Web模式启动。", r)
			showMessageBox("Go Proxy Server - 启动错误", errMsg, MB_ICONWARNING)
		}
	}()

	// Start systray in goroutine to allow timeout detection
	go func() {
		// systray.Run() blocks until systray.Quit() is called
		systray.Run(onReady(webPort), onExit)
	}()

	// Wait for tray to be ready with timeout (increased to 30 seconds for first-time initialization)
	timeout := time.After(30 * time.Second)
	select {
	case <-trayReady:
		logger.Info("System tray initialized successfully")
		// Keep main goroutine alive until shutdown is complete
		<-shutdownComplete
		return nil
	case <-timeout:
		logger.Error("System tray initialization timeout after 30 seconds")
		return fmt.Errorf("tray initialization timeout")
	}
}

func onReady(webPort int) func() {
	return func() {
		logger.Info("Initializing system tray...")

		systray.SetIcon(getIcon())
		systray.SetTitle("Go Proxy Server")
		systray.SetTooltip("Go Proxy Server - 代理服务器管理")

		globalWebManager = web.NewManager(globalDB, webPort)
		if err := globalWebManager.SyncAuthState(); err != nil {
			logger.Error("Failed to load runtime auth state: %v", err)
		}

		globalWebManager.StartConfiguredProxies()
		globalWebManager.StartConfiguredTunnelServer()
		globalWebManager.StartConfiguredTunnelClient()

		go func() {
			if err := globalWebManager.StartServer(); err != nil {
				logger.Error("Web server failed: %v", err)
			}
		}()

		// Wait for server to bind to port (StartServer sets actualPort before blocking on http.Serve)
		time.Sleep(200 * time.Millisecond)
		actualWebPort = globalWebManager.GetActualPort()
		logger.Info("Web management interface started on http://localhost:%d", actualWebPort)

		// Update tooltip to show the actual port
		tooltipText := fmt.Sprintf("Go Proxy Server\n管理界面: http://localhost:%d", actualWebPort)
		systray.SetTooltip(tooltipText)
		logger.Info("Tooltip updated with port information")

		// Add menu items
		mOpen := systray.AddMenuItem("打开管理界面", "在浏览器中打开管理界面")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("退出", "退出程序")

		logger.Info("System tray application ready")

		// Signal that tray is ready
		select {
		case trayReady <- true:
		default:
		}

		// Handle menu clicks
		go func() {
			for {
				select {
				case <-mOpen.ClickedCh:
					// Use actual port
					port := actualWebPort
					if port == 0 {
						port = webPort
					}
					openBrowser(fmt.Sprintf("http://localhost:%d", port))
				case <-mQuit.ClickedCh:
					logger.Info("Quit requested by user")
					systray.Quit()
					return
				}
			}
		}()
	}
}

func onExit() {
	// Cleanup code here
	logger.Info("System tray application exiting...")

	// Stop all running proxy servers
	if globalWebManager != nil {
		logger.Info("Stopping all proxy servers...")
		globalWebManager.StopAllProxies()
	}

	// Shutdown web server gracefully
	if globalWebManager != nil {
		logger.Info("Shutting down web server...")
		globalWebManager.Shutdown()
	}

	// Close all HTTP transport connections
	logger.Info("Closing all transport connections...")
	// Note: This requires importing the proxy package
	// proxy.CloseAllTransports()

	logger.Info("Cleanup complete")

	// Signal that shutdown is complete
	select {
	case shutdownComplete <- true:
	default:
	}
}

// openBrowser opens the default browser with the given URL
func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default: // "linux", "freebsd", "openbsd", "netbsd"
		err = exec.Command("xdg-open", url).Start()
	}
	if err != nil {
		logger.Error("Failed to open browser: %v", err)
	}
}

// getIcon returns the embedded icon data (ICO format)
// Icon: Blue circular network/proxy theme with transparent background
// Embedded sizes: 256x256, 128x128, 96x96, 64x64, 48x48, 32x32, 16x16
func getIcon() []byte {
	return iconData
}
