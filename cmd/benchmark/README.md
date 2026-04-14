# 代理服务器性能测试工具

这是一个用于测试远程代理服务器连接性能的工具，支持HTTP和SOCKS5代理，特别适合测试多出口IP场景。

## 功能特性

- 支持HTTP和SOCKS5代理协议
- 并发连接测试
- 基于请求数量或持续时间的测试模式
- 详细的性能指标统计
- 出口IP分布统计（适合多出口IP场景）
- 响应时间分析（最小/最大/平均）
- 吞吐量统计
- 错误统计和分类

## 编译

```bash
cd cmd/benchmark
go build -o benchmark main.go
```

或者从项目根目录：

```bash
go build -o bin/benchmark ./cmd/benchmark
```

## 使用方法

### 基本用法

```bash
# 测试SOCKS5代理（默认）
./benchmark -host 192.168.1.100 -port 1080 -c 50 -n 1000

# 测试HTTP代理
./benchmark -host 192.168.1.100 -port 8080 -type http -c 50 -n 1000

# 使用认证
./benchmark -host 192.168.1.100 -port 1080 -username user1 -password pass123 -c 50 -n 1000
```

### 参数说明

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `-host` | 代理服务器地址 | localhost |
| `-port` | 代理服务器端口 | 1080 |
| `-type` | 代理类型 (http/socks5) | socks5 |
| `-username` | 代理用户名 | 空 |
| `-password` | 代理密码 | 空 |
| `-target` | 测试目标URL | http://httpbin.org/ip |
| `-c` | 并发连接数 | 10 |
| `-n` | 总请求数 | 100 |
| `-d` | 测试持续时间 (如: 30s, 1m, 5m) | 0 (使用-n) |
| `-timeout` | 单个请求超时时间 | 30s |

## 测试场景示例

### 1. 测试最大并发连接数

测试代理服务器能支持多少并发连接：

```bash
# 逐步增加并发数，观察成功率
./benchmark -host 192.168.1.100 -port 1080 -c 100 -n 1000
./benchmark -host 192.168.1.100 -port 1080 -c 200 -n 1000
./benchmark -host 192.168.1.100 -port 1080 -c 500 -n 1000
./benchmark -host 192.168.1.100 -port 1080 -c 1000 -n 5000
```

### 2. 测试持续负载能力

测试代理服务器在持续负载下的稳定性：

```bash
# 持续测试5分钟，50个并发
./benchmark -host 192.168.1.100 -port 1080 -c 50 -d 5m

# 持续测试30分钟，100个并发
./benchmark -host 192.168.1.100 -port 1080 -c 100 -d 30m
```

### 3. 测试多出口IP分布

测试多出口IP的负载均衡效果：

```bash
# 大量请求测试IP分布
./benchmark -host 192.168.1.100 -port 1080 -c 100 -n 10000 -target http://httpbin.org/ip

# 或使用其他IP检测服务
./benchmark -host 192.168.1.100 -port 1080 -c 100 -n 10000 -target http://api.ipify.org
./benchmark -host 192.168.1.100 -port 1080 -c 100 -n 10000 -target https://ifconfig.me/ip
```

### 4. 压力测试

测试代理服务器的极限性能：

```bash
# 高并发短时间测试
./benchmark -host 192.168.1.100 -port 1080 -c 500 -d 1m

# 超高并发测试
./benchmark -host 192.168.1.100 -port 1080 -c 1000 -d 30s
```

### 5. HTTP vs SOCKS5 性能对比

```bash
# 测试SOCKS5性能
./benchmark -host 192.168.1.100 -port 1080 -type socks5 -c 100 -n 5000

# 测试HTTP性能
./benchmark -host 192.168.1.100 -port 8080 -type http -c 100 -n 5000
```

### 6. 不同目标网站测试

```bash
# 测试访问国内网站
./benchmark -host 192.168.1.100 -port 1080 -c 50 -n 1000 -target http://www.baidu.com

# 测试访问国外网站
./benchmark -host 192.168.1.100 -port 1080 -c 50 -n 1000 -target http://www.google.com

# 测试HTTPS
./benchmark -host 192.168.1.100 -port 1080 -c 50 -n 1000 -target https://httpbin.org/get
```

## 输出结果说明

测试完成后会输出以下统计信息：

### 基本统计
- **Total Requests**: 总请求数
- **Successful**: 成功请求数和百分比
- **Failed**: 失败请求数和百分比
- **Total Duration**: 总测试时间
- **Requests/sec**: 每秒请求数（QPS）
- **Total Data**: 总传输数据量
- **Throughput**: 吞吐量（字节/秒）

### 响应时间
- **Min**: 最小响应时间
- **Max**: 最大响应时间
- **Avg**: 平均响应时间

### 出口IP分布
显示每个出口IP的使用次数和百分比，用于验证多出口IP的负载均衡效果。

### 状态码分布
显示HTTP状态码的分布情况。

### 错误统计
如果有失败的请求，会显示错误类型和数量。

## 性能调优建议

### 1. 系统层面
```bash
# 增加系统文件描述符限制
ulimit -n 65535

# 调整TCP参数
sysctl -w net.ipv4.ip_local_port_range="1024 65535"
sysctl -w net.ipv4.tcp_tw_reuse=1
```

### 2. 测试机配置
- 确保测试机有足够的网络带宽
- 测试机和代理服务器之间网络延迟要低
- 测试机CPU和内存资源充足

### 3. 测试策略
- 从小并发开始，逐步增加
- 观察代理服务器的CPU、内存、网络使用情况
- 记录不同并发数下的成功率和响应时间
- 找到最佳的并发数配置

## 常见问题

### Q: 测试时出现大量连接超时
A: 可能原因：
1. 并发数设置过高，超过代理服务器限制
2. 网络延迟过大，需要增加 `-timeout` 参数
3. 代理服务器配置的连接限制过低

### Q: 出口IP分布不均匀
A: 可能原因：
1. 代理服务器的负载均衡算法问题
2. 某些出口IP可能不可用
3. 测试请求数量不够多，建议增加 `-n` 参数

### Q: 性能测试结果不稳定
A: 建议：
1. 多次运行测试取平均值
2. 使用 `-d` 参数进行持续时间测试
3. 确保测试环境稳定（网络、服务器负载等）

## 示例输出

```
=== Proxy Server Performance Test ===
Proxy: socks5://192.168.1.100:1080
Target: http://httpbin.org/ip
Concurrency: 100
Total Requests: 5000
Timeout: 30s

=== Test Results ===
Total Requests:    5000
Successful:        4987 (99.74%)
Failed:            13 (0.26%)
Total Duration:    45.23s
Requests/sec:      110.52
Total Data:        245.67 KB
Throughput:        5.43 KB/s

=== Response Time ===
Min:               123ms
Max:               2.45s
Avg:               456ms

=== Exit IPs ===
{"origin":"1.2.3.4"}                    : 1245 (24.90%)
{"origin":"1.2.3.5"}                    : 1256 (25.12%)
{"origin":"1.2.3.6"}                    : 1234 (24.68%)
{"origin":"1.2.3.7"}                    : 1252 (25.04%)

=== Status Codes ===
200: 4987 (99.74%)
502:   13 (0.26%)
```

## 注意事项

1. 测试前请确保有权限对目标代理服务器进行压力测试
2. 建议在非生产环境或低峰期进行测试
3. 高并发测试可能会对代理服务器造成较大压力，请谨慎设置并发数
4. 测试时注意监控代理服务器的资源使用情况
5. 某些目标网站可能有访问频率限制，建议使用专门的测试服务（如httpbin.org）
