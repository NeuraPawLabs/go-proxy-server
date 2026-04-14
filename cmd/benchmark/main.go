package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/proxy"
)

// TestConfig holds the configuration for the benchmark test
type TestConfig struct {
	ProxyHost     string
	ProxyPort     int
	ProxyType     string // "http" or "socks5"
	Username      string
	Password      string
	TargetURL     string
	Concurrency   int
	TotalRequests int
	Duration      time.Duration
	Timeout       time.Duration
}

// TestResult holds the results of a single request
type TestResult struct {
	Success      bool
	Duration     time.Duration
	Error        error
	StatusCode   int
	BytesRead    int64
	ExitIP       string
	ConnectTime  time.Duration
}

// BenchmarkStats holds aggregated statistics
type BenchmarkStats struct {
	TotalRequests     int64
	SuccessRequests   int64
	FailedRequests    int64
	TotalDuration     time.Duration
	MinDuration       time.Duration
	MaxDuration       time.Duration
	AvgDuration       time.Duration
	TotalBytes        int64
	ExitIPs           sync.Map // map[string]int64 - count per exit IP
	StatusCodes       sync.Map // map[int]int64 - count per status code
	Errors            sync.Map // map[string]int64 - count per error type
	RequestsPerSecond float64
	BytesPerSecond    float64
}

func main() {
	config := parseFlags()

	fmt.Println("=== Proxy Server Performance Test ===")
	fmt.Printf("Proxy: %s://%s:%d\n", config.ProxyType, config.ProxyHost, config.ProxyPort)
	fmt.Printf("Target: %s\n", config.TargetURL)
	fmt.Printf("Concurrency: %d\n", config.Concurrency)
	if config.Duration > 0 {
		fmt.Printf("Duration: %s\n", config.Duration)
	} else {
		fmt.Printf("Total Requests: %d\n", config.TotalRequests)
	}
	fmt.Printf("Timeout: %s\n", config.Timeout)
	fmt.Println()

	// Run the benchmark
	stats := runBenchmark(config)

	// Print results
	printResults(stats)
}

func parseFlags() *TestConfig {
	config := &TestConfig{}

	flag.StringVar(&config.ProxyHost, "host", "localhost", "Proxy server host")
	flag.IntVar(&config.ProxyPort, "port", 1080, "Proxy server port")
	flag.StringVar(&config.ProxyType, "type", "socks5", "Proxy type (http or socks5)")
	flag.StringVar(&config.Username, "username", "", "Proxy username")
	flag.StringVar(&config.Password, "password", "", "Proxy password")
	flag.StringVar(&config.TargetURL, "target", "http://httpbin.org/ip", "Target URL to test")
	flag.IntVar(&config.Concurrency, "c", 10, "Number of concurrent connections")
	flag.IntVar(&config.TotalRequests, "n", 100, "Total number of requests (0 for duration-based test)")
	flag.DurationVar(&config.Duration, "d", 0, "Test duration (e.g., 30s, 1m). If set, -n is ignored")
	flag.DurationVar(&config.Timeout, "timeout", 30*time.Second, "Request timeout")

	flag.Parse()

	return config
}

func runBenchmark(config *TestConfig) *BenchmarkStats {
	stats := &BenchmarkStats{
		MinDuration: time.Hour, // Initialize with a large value
	}

	var wg sync.WaitGroup
	resultChan := make(chan *TestResult, config.Concurrency*2)

	// Context for controlling test duration
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start time
	startTime := time.Now()

	// Start result collector
	collectorDone := make(chan struct{})
	go func() {
		collectResults(resultChan, stats)
		close(collectorDone)
	}()

	// Determine test mode: duration-based or count-based
	if config.Duration > 0 {
		// Duration-based test
		go func() {
			time.Sleep(config.Duration)
			cancel()
		}()

		// Start workers
		for i := 0; i < config.Concurrency; i++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				runWorkerDuration(ctx, config, resultChan, workerID)
			}(i)
		}
	} else {
		// Count-based test
		requestCounter := int64(0)

		// Start workers
		for i := 0; i < config.Concurrency; i++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				runWorkerCount(ctx, config, resultChan, &requestCounter, workerID)
			}(i)
		}
	}

	// Wait for all workers to finish
	wg.Wait()
	close(resultChan)

	// Wait for result collector to finish
	<-collectorDone

	// Calculate final statistics
	stats.TotalDuration = time.Since(startTime)
	if stats.TotalRequests > 0 {
		stats.AvgDuration = time.Duration(int64(stats.TotalDuration) / stats.TotalRequests)
		stats.RequestsPerSecond = float64(stats.TotalRequests) / stats.TotalDuration.Seconds()
		stats.BytesPerSecond = float64(stats.TotalBytes) / stats.TotalDuration.Seconds()
	}

	return stats
}

func runWorkerDuration(ctx context.Context, config *TestConfig, resultChan chan<- *TestResult, workerID int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			result := makeRequest(config)
			resultChan <- result
		}
	}
}

func runWorkerCount(ctx context.Context, config *TestConfig, resultChan chan<- *TestResult, counter *int64, workerID int) {
	for {
		// Check if we've reached the total request count
		current := atomic.AddInt64(counter, 1)
		if current > int64(config.TotalRequests) {
			return
		}

		select {
		case <-ctx.Done():
			return
		default:
			result := makeRequest(config)
			resultChan <- result
		}
	}
}

func makeRequest(config *TestConfig) *TestResult {
	result := &TestResult{}
	startTime := time.Now()

	// Create HTTP client with proxy
	client, err := createProxyClient(config)
	if err != nil {
		result.Error = err
		result.Duration = time.Since(startTime)
		return result
	}

	// Make request
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", config.TargetURL, nil)
	if err != nil {
		result.Error = err
		result.Duration = time.Since(startTime)
		return result
	}

	connectStart := time.Now()
	resp, err := client.Do(req)
	result.ConnectTime = time.Since(connectStart)

	if err != nil {
		result.Error = err
		result.Duration = time.Since(startTime)
		return result
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		result.Error = err
		result.Duration = time.Since(startTime)
		return result
	}

	result.BytesRead = int64(len(body))
	result.Duration = time.Since(startTime)

	// Try to extract exit IP from response (if target is httpbin.org/ip or similar)
	if config.TargetURL == "http://httpbin.org/ip" || config.TargetURL == "https://httpbin.org/ip" {
		result.ExitIP = string(body)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		result.Success = true
	}

	return result
}

func createProxyClient(config *TestConfig) (*http.Client, error) {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		MaxIdleConns:    100,
		IdleConnTimeout: 90 * time.Second,
	}

	if config.ProxyType == "http" {
		// HTTP proxy
		proxyURL := &url.URL{
			Scheme: "http",
			Host:   fmt.Sprintf("%s:%d", config.ProxyHost, config.ProxyPort),
		}
		if config.Username != "" {
			proxyURL.User = url.UserPassword(config.Username, config.Password)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	} else if config.ProxyType == "socks5" {
		// SOCKS5 proxy
		var auth *proxy.Auth
		if config.Username != "" {
			auth = &proxy.Auth{
				User:     config.Username,
				Password: config.Password,
			}
		}

		dialer, err := proxy.SOCKS5("tcp", fmt.Sprintf("%s:%d", config.ProxyHost, config.ProxyPort), auth, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("failed to create SOCKS5 dialer: %w", err)
		}

		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}
	} else {
		return nil, fmt.Errorf("unsupported proxy type: %s", config.ProxyType)
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   config.Timeout,
	}

	return client, nil
}

func collectResults(resultChan <-chan *TestResult, stats *BenchmarkStats) {
	for result := range resultChan {
		atomic.AddInt64(&stats.TotalRequests, 1)

		if result.Success {
			atomic.AddInt64(&stats.SuccessRequests, 1)
		} else {
			atomic.AddInt64(&stats.FailedRequests, 1)
		}

		atomic.AddInt64(&stats.TotalBytes, result.BytesRead)

		// Update min/max duration (needs locking for accurate results)
		if result.Duration < stats.MinDuration {
			stats.MinDuration = result.Duration
		}
		if result.Duration > stats.MaxDuration {
			stats.MaxDuration = result.Duration
		}

		// Track exit IPs
		if result.ExitIP != "" {
			val, _ := stats.ExitIPs.LoadOrStore(result.ExitIP, new(int64))
			atomic.AddInt64(val.(*int64), 1)
		}

		// Track status codes
		if result.StatusCode > 0 {
			val, _ := stats.StatusCodes.LoadOrStore(result.StatusCode, new(int64))
			atomic.AddInt64(val.(*int64), 1)
		}

		// Track errors
		if result.Error != nil {
			errMsg := result.Error.Error()
			val, _ := stats.Errors.LoadOrStore(errMsg, new(int64))
			atomic.AddInt64(val.(*int64), 1)
		}
	}
}

func printResults(stats *BenchmarkStats) {
	fmt.Println("=== Test Results ===")
	fmt.Printf("Total Requests:    %d\n", stats.TotalRequests)
	fmt.Printf("Successful:        %d (%.2f%%)\n", stats.SuccessRequests, float64(stats.SuccessRequests)/float64(stats.TotalRequests)*100)
	fmt.Printf("Failed:            %d (%.2f%%)\n", stats.FailedRequests, float64(stats.FailedRequests)/float64(stats.TotalRequests)*100)
	fmt.Printf("Total Duration:    %s\n", stats.TotalDuration)
	fmt.Printf("Requests/sec:      %.2f\n", stats.RequestsPerSecond)
	fmt.Printf("Total Data:        %s\n", formatBytes(stats.TotalBytes))
	fmt.Printf("Throughput:        %s/s\n", formatBytes(int64(stats.BytesPerSecond)))
	fmt.Println()

	fmt.Println("=== Response Time ===")
	fmt.Printf("Min:               %s\n", stats.MinDuration)
	fmt.Printf("Max:               %s\n", stats.MaxDuration)
	fmt.Printf("Avg:               %s\n", stats.AvgDuration)
	fmt.Println()

	// Print exit IPs
	fmt.Println("=== Exit IPs ===")
	stats.ExitIPs.Range(func(key, value interface{}) bool {
		ip := key.(string)
		count := atomic.LoadInt64(value.(*int64))
		percentage := float64(count) / float64(stats.TotalRequests) * 100
		fmt.Printf("%-40s: %6d (%.2f%%)\n", ip, count, percentage)
		return true
	})
	fmt.Println()

	// Print status codes
	fmt.Println("=== Status Codes ===")
	stats.StatusCodes.Range(func(key, value interface{}) bool {
		code := key.(int)
		count := atomic.LoadInt64(value.(*int64))
		percentage := float64(count) / float64(stats.TotalRequests) * 100
		fmt.Printf("%d: %6d (%.2f%%)\n", code, count, percentage)
		return true
	})
	fmt.Println()

	// Print errors if any
	if stats.FailedRequests > 0 {
		fmt.Println("=== Errors ===")
		stats.Errors.Range(func(key, value interface{}) bool {
			errMsg := key.(string)
			count := atomic.LoadInt64(value.(*int64))
			fmt.Printf("%s: %d\n", errMsg, count)
			return true
		})
		fmt.Println()
	}
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// Additional helper function for HTTP proxy with authentication
func createHTTPProxyAuth(username, password string) string {
	auth := username + ":" + password
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(auth))
}
