# Windows 代理服务器资源监控脚本
# 使用方法: .\monitor.ps1 -Port 12223 -Interval 5

param(
    [int]$Port = 12223,
    [int]$Interval = 5
)

Write-Host "=== 代理服务器资源监控 ===" -ForegroundColor Green
Write-Host "监控端口: $Port"
Write-Host "刷新间隔: $Interval 秒"
Write-Host "按 Ctrl+C 停止监控"
Write-Host ""

while ($true) {
    Clear-Host
    $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    Write-Host "=== 监控时间: $timestamp ===" -ForegroundColor Cyan
    Write-Host ""

    # 1. 进程资源使用
    Write-Host "【进程资源使用】" -ForegroundColor Yellow
    $process = Get-Process -Name "*go-proxy-server*" -ErrorAction SilentlyContinue
    if ($process) {
        $cpuPercent = [math]::Round($process.CPU, 2)
        $memoryMB = [math]::Round($process.WorkingSet64 / 1MB, 2)
        $handles = $process.Handles
        $threads = $process.Threads.Count

        Write-Host "  进程名称: $($process.ProcessName)"
        Write-Host "  CPU时间: $cpuPercent 秒"
        Write-Host "  内存使用: $memoryMB MB"
        Write-Host "  句柄数: $handles"
        Write-Host "  线程数: $threads"
    } else {
        Write-Host "  未找到 go-proxy-server 进程" -ForegroundColor Red
    }
    Write-Host ""

    # 2. 网络连接统计
    Write-Host "【网络连接统计】" -ForegroundColor Yellow
    $connections = netstat -an | Select-String ":$Port"
    $established = ($connections | Select-String "ESTABLISHED").Count
    $timeWait = ($connections | Select-String "TIME_WAIT").Count
    $closeWait = ($connections | Select-String "CLOSE_WAIT").Count
    $listening = ($connections | Select-String "LISTENING").Count
    $total = $connections.Count

    Write-Host "  总连接数: $total"
    Write-Host "  LISTENING: $listening"
    Write-Host "  ESTABLISHED: $established" -ForegroundColor $(if ($established -gt 1000) { "Red" } else { "Green" })
    Write-Host "  TIME_WAIT: $timeWait" -ForegroundColor $(if ($timeWait -gt 5000) { "Red" } elseif ($timeWait -gt 2000) { "Yellow" } else { "Green" })
    Write-Host "  CLOSE_WAIT: $closeWait" -ForegroundColor $(if ($closeWait -gt 100) { "Red" } else { "Green" })
    Write-Host ""

    # 3. 系统资源
    Write-Host "【系统资源】" -ForegroundColor Yellow
    $cpu = Get-Counter '\Processor(_Total)\% Processor Time' -ErrorAction SilentlyContinue
    $memory = Get-CimInstance Win32_OperatingSystem
    $memoryUsedPercent = [math]::Round((($memory.TotalVisibleMemorySize - $memory.FreePhysicalMemory) / $memory.TotalVisibleMemorySize) * 100, 2)

    if ($cpu) {
        Write-Host "  系统CPU使用率: $([math]::Round($cpu.CounterSamples[0].CookedValue, 2))%"
    }
    Write-Host "  系统内存使用率: $memoryUsedPercent%"
    Write-Host ""

    # 4. 端口耗尽检查
    Write-Host "【端口使用情况】" -ForegroundColor Yellow
    $allPorts = netstat -an | Select-String ":\d+" | Measure-Object
    $dynamicPortStart = 49152
    $dynamicPortEnd = 65535
    $dynamicPortsInUse = (netstat -an | Select-String ":($dynamicPortStart|[5-6]\d{4})" | Measure-Object).Count
    $dynamicPortsTotal = $dynamicPortEnd - $dynamicPortStart + 1
    $dynamicPortsUsedPercent = [math]::Round(($dynamicPortsInUse / $dynamicPortsTotal) * 100, 2)

    Write-Host "  动态端口使用: $dynamicPortsInUse / $dynamicPortsTotal ($dynamicPortsUsedPercent%)"
    if ($dynamicPortsUsedPercent -gt 80) {
        Write-Host "  警告: 动态端口使用率过高!" -ForegroundColor Red
    }
    Write-Host ""

    # 5. 告警提示
    Write-Host "【告警提示】" -ForegroundColor Yellow
    $warnings = @()

    if ($process -and $memoryMB -gt 1000) {
        $warnings += "  ⚠ 内存使用超过 1GB"
    }
    if ($established -gt 1000) {
        $warnings += "  ⚠ ESTABLISHED 连接数过多 ($established)"
    }
    if ($timeWait -gt 5000) {
        $warnings += "  ⚠ TIME_WAIT 连接数过多 ($timeWait)，可能导致端口耗尽"
    }
    if ($closeWait -gt 100) {
        $warnings += "  ⚠ CLOSE_WAIT 连接数过多 ($closeWait)，可能存在连接泄漏"
    }
    if ($handles -gt 10000) {
        $warnings += "  ⚠ 句柄数过多 ($handles)"
    }
    if ($dynamicPortsUsedPercent -gt 80) {
        $warnings += "  ⚠ 动态端口使用率过高 ($dynamicPortsUsedPercent%)"
    }

    if ($warnings.Count -eq 0) {
        Write-Host "  ✓ 暂无告警" -ForegroundColor Green
    } else {
        foreach ($warning in $warnings) {
            Write-Host $warning -ForegroundColor Red
        }
    }
    Write-Host ""

    Write-Host "下次刷新: $Interval 秒后..." -ForegroundColor Gray
    Start-Sleep -Seconds $Interval
}
