@echo off
chcp 65001 >nul
echo ===========================================
echo Go Proxy Server - 调试模式
echo ===========================================
echo.
echo 程序启动中，请查看下方输出...
echo 日志文件位置: %APPDATA%\go-proxy-server\app.log
echo.
echo 按 Ctrl+C 可以停止程序
echo ===========================================
echo.

go-proxy-server-debug.exe

echo.
echo 程序已退出
pause
