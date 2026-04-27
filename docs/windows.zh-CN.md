# Windows 使用与构建

这是 Windows 平台的精简专题文档，覆盖日常使用、调试日志和构建要点。

## 日常使用

推荐直接运行 Windows 版本：

```powershell
bin\go-proxy-server.exe
```

说明：
- 默认进入系统托盘模式
- Web 管理界面仅监听本机 `localhost`
- 默认会自动选择一个可用随机端口
- 可从托盘菜单打开管理界面

## 日志与数据目录

默认数据目录：

```powershell
%APPDATA%\go-proxy-server\
```

常见文件：
- `data.db`：SQLite 数据库
- `app.log`：Windows 托盘 / GUI 模式日志

补充说明：
- 实际管理地址以启动日志或托盘提示为准

## 常见问题

### 看不到托盘图标

检查任务栏右下角隐藏图标区域。

### 想固定管理端口

```powershell
go-proxy-server.exe web -port 8888
```

### 程序闪退

优先查看应用数据目录中的 `app.log`：

```powershell
Get-Content "$env:APPDATA\go-proxy-server\app.log" -Tail 200
```

## 构建

推荐使用 Makefile：

```bash
make build-windows
```

说明：
- `build-windows` 生成托盘 / GUI 版本
- `build-windows-gui` 仅作为 `build-windows` 的别名保留

如果需要完整资源信息，构建过程会使用仓库中的资源脚本和清单文件。

## 误报与签名

Go 编译的网络程序在 Windows 上可能被安全软件误报。常见缓解方式：
- 为可执行文件添加版本信息
- 使用代码签名
- 向安全软件厂商提交误报申诉
- 提供 SHA256 校验值给用户验证
