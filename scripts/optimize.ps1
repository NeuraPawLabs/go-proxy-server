# Windows 代理服务器配置优化脚本
# 使用方法: .\optimize.ps1

Write-Host "=== 代理服务器配置优化工具 ===" -ForegroundColor Green
Write-Host ""

# 检查是否以管理员权限运行
$isAdmin = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) {
    Write-Host "警告: 建议以管理员权限运行此脚本以应用所有优化" -ForegroundColor Yellow
    Write-Host ""
}

# 1. 检查数据库位置
Write-Host "【1. 检查数据库配置】" -ForegroundColor Cyan
$dbPath = "$env:APPDATA\go-proxy-server\data.db"
if (Test-Path $dbPath) {
    Write-Host "  ✓ 数据库路径: $dbPath" -ForegroundColor Green

    # 显示当前配置
    Write-Host ""
    Write-Host "  当前系统配置:" -ForegroundColor Yellow

    # 需要安装 sqlite3 命令行工具
    $sqliteExe = "sqlite3.exe"
    if (Get-Command $sqliteExe -ErrorAction SilentlyContinue) {
        & $sqliteExe $dbPath "SELECT key, value FROM system_configs ORDER BY key;"
    } else {
        Write-Host "  提示: 安装 sqlite3 命令行工具可查看详细配置" -ForegroundColor Gray
        Write-Host "  下载地址: https://www.sqlite.org/download.html" -ForegroundColor Gray
    }
} else {
    Write-Host "  ✗ 未找到数据库文件: $dbPath" -ForegroundColor Red
}
Write-Host ""

# 2. 推荐的配置值
Write-Host "【2. 推荐的配置优化】" -ForegroundColor Cyan
Write-Host ""
Write-Host "  通过 Web 管理界面 (http://localhost:9090) 调整以下配置:" -ForegroundColor Yellow
Write-Host ""
Write-Host "  连接限制配置:" -ForegroundColor White
Write-Host "    MaxConcurrentConnections: 10000        (全局最大并发连接数)"
Write-Host "    MaxConcurrentConnectionsPerIP: 500     (单IP最大并发连接数)"
Write-Host ""
Write-Host "  超时配置:" -ForegroundColor White
Write-Host "    ConnectTimeout: 30s                    (连接超时)"
Write-Host "    IdleReadTimeout: 300s                  (空闲读超时，5分钟)"
Write-Host "    IdleWriteTimeout: 300s                 (空闲写超时，5分钟)"
Write-Host "    MaxConnectionAge: 3600s                (最大连接存活时间，1小时)"
Write-Host "    CleanupTimeout: 5s                     (清理超时)"
Write-Host ""

# 3. Windows 系统优化
Write-Host "【3. Windows 系统优化】" -ForegroundColor Cyan
Write-Host ""

# 3.1 检查动态端口范围
Write-Host "  3.1 动态端口范围配置:" -ForegroundColor Yellow
$portRange = netsh int ipv4 show dynamicport tcp
Write-Host $portRange
Write-Host ""
Write-Host "  推荐优化命令 (需要管理员权限):" -ForegroundColor White
Write-Host "    netsh int ipv4 set dynamicport tcp start=10000 num=55535" -ForegroundColor Gray
Write-Host ""

# 3.2 检查 TCP 参数
Write-Host "  3.2 TCP 参数优化:" -ForegroundColor Yellow
Write-Host "  推荐优化命令 (需要管理员权限):" -ForegroundColor White
Write-Host "    # 启用 TCP 时间戳" -ForegroundColor Gray
Write-Host "    netsh int tcp set global timestamps=enabled" -ForegroundColor Gray
Write-Host ""
Write-Host "    # 调整 TIME_WAIT 超时时间 (通过注册表)" -ForegroundColor Gray
Write-Host "    reg add HKLM\SYSTEM\CurrentControlSet\Services\Tcpip\Parameters /v TcpTimedWaitDelay /t REG_DWORD /d 30 /f" -ForegroundColor Gray
Write-Host ""

# 3.3 防火墙检查
Write-Host "  3.3 防火墙配置:" -ForegroundColor Yellow
$firewallRule = Get-NetFirewallRule -DisplayName "*go-proxy-server*" -ErrorAction SilentlyContinue
if ($firewallRule) {
    Write-Host "  ✓ 已找到防火墙规则" -ForegroundColor Green
} else {
    Write-Host "  ✗ 未找到防火墙规则" -ForegroundColor Yellow
    Write-Host "  建议添加防火墙规则 (需要管理员权限):" -ForegroundColor White
    Write-Host "    New-NetFirewallRule -DisplayName 'Go Proxy Server' -Direction Inbound -Protocol TCP -LocalPort 12223 -Action Allow" -ForegroundColor Gray
}
Write-Host ""

# 4. 性能监控建议
Write-Host "【4. 性能监控建议】" -ForegroundColor Cyan
Write-Host ""
Write-Host "  使用监控脚本实时查看资源使用:" -ForegroundColor Yellow
Write-Host "    .\scripts\monitor.ps1 -Port 12223 -Interval 5" -ForegroundColor Gray
Write-Host ""
Write-Host "  使用 Windows 性能监视器 (perfmon):" -ForegroundColor Yellow
Write-Host "    perfmon" -ForegroundColor Gray
Write-Host "    添加计数器: Process(go-proxy-server*)\*" -ForegroundColor Gray
Write-Host ""

# 5. 应用优化（需要管理员权限）
Write-Host "【5. 应用系统优化】" -ForegroundColor Cyan
if ($isAdmin) {
    Write-Host ""
    $apply = Read-Host "是否应用系统优化? (y/n)"

    if ($apply -eq 'y' -or $apply -eq 'Y') {
        Write-Host ""
        Write-Host "  正在应用优化..." -ForegroundColor Yellow

        try {
            # 扩展动态端口范围
            Write-Host "  - 扩展动态端口范围..."
            netsh int ipv4 set dynamicport tcp start=10000 num=55535 | Out-Null
            Write-Host "    ✓ 动态端口范围已优化" -ForegroundColor Green

            # 启用 TCP 时间戳
            Write-Host "  - 启用 TCP 时间戳..."
            netsh int tcp set global timestamps=enabled | Out-Null
            Write-Host "    ✓ TCP 时间戳已启用" -ForegroundColor Green

            # 调整 TIME_WAIT 超时
            Write-Host "  - 调整 TIME_WAIT 超时时间..."
            reg add HKLM\SYSTEM\CurrentControlSet\Services\Tcpip\Parameters /v TcpTimedWaitDelay /t REG_DWORD /d 30 /f | Out-Null
            Write-Host "    ✓ TIME_WAIT 超时已调整为 30 秒" -ForegroundColor Green

            Write-Host ""
            Write-Host "  ✓ 系统优化已完成!" -ForegroundColor Green
            Write-Host "  注意: 某些优化可能需要重启系统才能生效" -ForegroundColor Yellow
        } catch {
            Write-Host "  ✗ 优化失败: $_" -ForegroundColor Red
        }
    }
} else {
    Write-Host ""
    Write-Host "  请以管理员权限重新运行此脚本以应用系统优化" -ForegroundColor Yellow
}

Write-Host ""
Write-Host "=== 优化完成 ===" -ForegroundColor Green
Write-Host ""
Write-Host "后续步骤:" -ForegroundColor Cyan
Write-Host "  1. 通过 Web 管理界面调整代理服务器配置"
Write-Host "  2. 重启代理服务器使配置生效"
Write-Host "  3. 使用 monitor.ps1 监控资源使用情况"
Write-Host "  4. 重新运行压测验证优化效果"
Write-Host ""
