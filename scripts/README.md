# Windows资源文件

本目录包含用于Windows可执行文件的资源配置。

## 文件说明

- `resource.rc` - Windows资源定义文件（用于windres）
- `manifest.xml` - Windows应用程序清单
- `build_resources.sh` - 自动构建脚本（支持多种工具）
- `build_resources_goversioninfo.sh` - 使用goversioninfo的构建脚本

## 快速开始

### 1. 安装工具（选择其一）

**推荐方法（纯Go）：**
```bash
go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
```

**替代方法1（纯Go）：**
```bash
go install github.com/tc-hib/go-winres@latest
```

**替代方法2（需要C编译器）：**
```bash
# Ubuntu/Debian
sudo apt-get install mingw-w64

# macOS
brew install mingw-w64
```

### 2. 构建Windows版本

安装工具后，直接使用Make命令：

```bash
# 自动编译资源并构建
make build-windows      # 控制台版本
make build-windows-gui  # 系统托盘版本
```

资源文件会自动编译并嵌入到exe中。

### 3. 验证

构建完成后，在Windows上右键点击exe → 属性 → 详细信息，应该能看到：
- 文件描述、产品名称、版本号等信息

## 手动构建资源（可选）

如果只想生成资源文件：

```bash
# 自动选择最佳方法
./scripts/build_resources.sh

# 或使用特定工具
./scripts/build_resources_goversioninfo.sh
```

## 自定义版本信息

编辑 `assets/versioninfo.json` 文件修改版本号和其他信息。

## 故障排除

### 问题：构建时提示找不到资源编译器
**解决**：安装上述任一工具后再运行

### 问题：网络超时无法安装go工具
**解决**：
1. 配置Go代理：`go env -w GOPROXY=https://goproxy.cn,direct`
2. 或使用mingw-w64（不需要网络）

### 问题：仍然被Windows Defender误报
**解决**：
1. 确认资源已正确嵌入（查看exe属性）
2. 考虑代码签名（最有效）
3. 向Microsoft提交误报申诉

详细说明请查看 `docs/WINDOWS_BUILD.md`
