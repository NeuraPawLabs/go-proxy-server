@echo off
chcp 65001 >nul
set LOG_FILE=%APPDATA%\go-proxy-server\app.log

echo ===========================================
echo 打开日志文件
echo ===========================================
echo.
echo 日志文件位置:
echo %LOG_FILE%
echo.

if exist "%LOG_FILE%" (
    echo 正在打开日志文件...
    notepad "%LOG_FILE%"
) else (
    echo [错误] 日志文件不存在！
    echo.
    echo 可能的原因:
    echo 1. 程序尚未运行过
    echo 2. 程序启动失败
    echo.
    echo 请先运行程序: go-proxy-server-debug.exe
)

echo.
pause
