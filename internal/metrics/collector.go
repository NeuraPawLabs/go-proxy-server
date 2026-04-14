package metrics

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/apeming/go-proxy-server/internal/models"
	"gorm.io/gorm"
)

// Collector collects and aggregates proxy metrics
type Collector struct {
	mu                   sync.RWMutex
	db                   *gorm.DB
	startTime            time.Time
	activeConnections    int32
	maxActiveConnections int32
	totalConnections     int64
	bytesReceived        int64
	bytesSent            int64
	errorCount           int64

	// For speed calculation
	lastSnapshot      time.Time
	lastBytesReceived int64
	lastBytesSent     int64
	uploadSpeed       float64
	downloadSpeed     float64
	maxUploadSpeed    float64
	maxDownloadSpeed  float64

	// Background aggregation
	stopChan         chan struct{}
	snapshotInterval time.Duration
}

var (
	globalCollector *Collector
	once            sync.Once
)

// InitCollector initializes the global metrics collector
func InitCollector(db *gorm.DB, snapshotInterval time.Duration) *Collector {
	once.Do(func() {
		globalCollector = &Collector{
			db:               db,
			startTime:        time.Now(),
			lastSnapshot:     time.Now(),
			stopChan:         make(chan struct{}),
			snapshotInterval: snapshotInterval,
		}

		// Start background aggregation
		go globalCollector.backgroundAggregation()
	})
	return globalCollector
}

// GetCollector returns the global collector instance
func GetCollector() *Collector {
	return globalCollector
}

// RecordConnection increments connection counters
func (c *Collector) RecordConnection() {
	newActive := atomic.AddInt32(&c.activeConnections, 1)
	atomic.AddInt64(&c.totalConnections, 1)

	// Update max active connections if current is higher
	for {
		currentMax := atomic.LoadInt32(&c.maxActiveConnections)
		if newActive <= currentMax {
			break
		}
		if atomic.CompareAndSwapInt32(&c.maxActiveConnections, currentMax, newActive) {
			break
		}
	}
}

// RecordDisconnection decrements active connection counter
func (c *Collector) RecordDisconnection() {
	atomic.AddInt32(&c.activeConnections, -1)
}

// RecordBytesReceived adds to bytes received counter
func (c *Collector) RecordBytesReceived(bytes int64) {
	atomic.AddInt64(&c.bytesReceived, bytes)
}

// RecordBytesSent adds to bytes sent counter
func (c *Collector) RecordBytesSent(bytes int64) {
	atomic.AddInt64(&c.bytesSent, bytes)
}

// RecordError increments error counter
func (c *Collector) RecordError() {
	atomic.AddInt64(&c.errorCount, 1)
}

// GetSnapshot returns current metrics snapshot
func (c *Collector) GetSnapshot() *MetricsSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return &MetricsSnapshot{
		Timestamp:            time.Now().Unix(),
		ActiveConnections:    int(atomic.LoadInt32(&c.activeConnections)),
		MaxActiveConnections: int(atomic.LoadInt32(&c.maxActiveConnections)),
		TotalConnections:     atomic.LoadInt64(&c.totalConnections),
		BytesReceived:        atomic.LoadInt64(&c.bytesReceived),
		BytesSent:            atomic.LoadInt64(&c.bytesSent),
		UploadSpeed:          c.uploadSpeed,
		DownloadSpeed:        c.downloadSpeed,
		MaxUploadSpeed:       c.maxUploadSpeed,
		MaxDownloadSpeed:     c.maxDownloadSpeed,
		ErrorCount:           atomic.LoadInt64(&c.errorCount),
		Uptime:               int64(time.Since(c.startTime).Seconds()),
	}
}

// MetricsSnapshot represents a point-in-time metrics snapshot
type MetricsSnapshot struct {
	Timestamp            int64   `json:"timestamp"`
	ActiveConnections    int     `json:"activeConnections"`
	MaxActiveConnections int     `json:"maxActiveConnections"`
	TotalConnections     int64   `json:"totalConnections"`
	BytesReceived        int64   `json:"bytesReceived"`
	BytesSent            int64   `json:"bytesSent"`
	UploadSpeed          float64 `json:"uploadSpeed"`
	DownloadSpeed        float64 `json:"downloadSpeed"`
	MaxUploadSpeed       float64 `json:"maxUploadSpeed"`
	MaxDownloadSpeed     float64 `json:"maxDownloadSpeed"`
	ErrorCount           int64   `json:"errorCount"`
	Uptime               int64   `json:"uptime"`
}

// backgroundAggregation periodically calculates speeds and saves snapshots
func (c *Collector) backgroundAggregation() {
	ticker := time.NewTicker(c.snapshotInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.calculateSpeeds()
			c.saveSnapshot()
		case <-c.stopChan:
			return
		}
	}
}

// calculateSpeeds calculates upload and download speeds
func (c *Collector) calculateSpeeds() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(c.lastSnapshot).Seconds()

	if elapsed > 0 {
		currentBytesReceived := atomic.LoadInt64(&c.bytesReceived)
		currentBytesSent := atomic.LoadInt64(&c.bytesSent)

		c.downloadSpeed = float64(currentBytesReceived-c.lastBytesReceived) / elapsed
		c.uploadSpeed = float64(currentBytesSent-c.lastBytesSent) / elapsed

		// Update max speeds
		if c.uploadSpeed > c.maxUploadSpeed {
			c.maxUploadSpeed = c.uploadSpeed
		}
		if c.downloadSpeed > c.maxDownloadSpeed {
			c.maxDownloadSpeed = c.downloadSpeed
		}

		c.lastBytesReceived = currentBytesReceived
		c.lastBytesSent = currentBytesSent
		c.lastSnapshot = now
	}
}

// saveSnapshot saves current metrics to database
func (c *Collector) saveSnapshot() {
	if c.db == nil {
		return
	}

	snapshot := &models.MetricsSnapshot{
		Timestamp:            time.Now().Unix(),
		ActiveConnections:    int(atomic.LoadInt32(&c.activeConnections)),
		MaxActiveConnections: int(atomic.LoadInt32(&c.maxActiveConnections)),
		TotalConnections:     atomic.LoadInt64(&c.totalConnections),
		BytesReceived:        atomic.LoadInt64(&c.bytesReceived),
		BytesSent:            atomic.LoadInt64(&c.bytesSent),
		UploadSpeed:          c.uploadSpeed,
		DownloadSpeed:        c.downloadSpeed,
		MaxUploadSpeed:       c.maxUploadSpeed,
		MaxDownloadSpeed:     c.maxDownloadSpeed,
		ErrorCount:           atomic.LoadInt64(&c.errorCount),
	}

	c.db.Create(snapshot)
}

// GetHistoricalSnapshots retrieves historical metrics from database
func (c *Collector) GetHistoricalSnapshots(startTime, endTime int64, limit int) ([]models.MetricsSnapshot, error) {
	var snapshots []models.MetricsSnapshot

	query := c.db.Where("timestamp >= ? AND timestamp <= ?", startTime, endTime).
		Order("timestamp ASC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Find(&snapshots).Error
	return snapshots, err
}

// GetDownsampledSnapshots retrieves downsampled historical metrics from database
// It aggregates data points within each interval to reduce data volume
func (c *Collector) GetDownsampledSnapshots(startTime, endTime int64, targetPoints int) ([]models.MetricsSnapshot, error) {
	if targetPoints <= 0 {
		targetPoints = 60 // Default to 60 points
	}

	// Calculate interval size in seconds
	timeRange := endTime - startTime
	if timeRange <= 0 {
		return []models.MetricsSnapshot{}, nil
	}

	interval := timeRange / int64(targetPoints)
	if interval < 1 {
		interval = 1 // Minimum 1 second interval
	}

	var snapshots []models.MetricsSnapshot

	// Query all data points in the time range
	var allSnapshots []models.MetricsSnapshot
	err := c.db.Where("timestamp >= ? AND timestamp <= ?", startTime, endTime).
		Order("timestamp ASC").
		Find(&allSnapshots).Error
	if err != nil {
		return nil, err
	}

	if len(allSnapshots) == 0 {
		return snapshots, nil
	}

	// Downsample by averaging data points within each interval
	currentBucket := startTime
	var bucketData []models.MetricsSnapshot

	for _, snapshot := range allSnapshots {
		// Check if this snapshot belongs to the next bucket
		if snapshot.Timestamp >= currentBucket+interval {
			// Process current bucket if it has data
			if len(bucketData) > 0 {
				snapshots = append(snapshots, aggregateBucket(bucketData))
				bucketData = nil
			}
			// Move to the next bucket
			currentBucket = (snapshot.Timestamp / interval) * interval
		}
		bucketData = append(bucketData, snapshot)
	}

	// Process the last bucket
	if len(bucketData) > 0 {
		snapshots = append(snapshots, aggregateBucket(bucketData))
	}

	return snapshots, nil
}

// aggregateBucket aggregates multiple snapshots into one by averaging
func aggregateBucket(snapshots []models.MetricsSnapshot) models.MetricsSnapshot {
	if len(snapshots) == 0 {
		return models.MetricsSnapshot{}
	}

	if len(snapshots) == 1 {
		return snapshots[0]
	}

	// Use the middle timestamp as representative
	result := models.MetricsSnapshot{
		Timestamp: snapshots[len(snapshots)/2].Timestamp,
	}

	// Sum all values
	var sumActive, sumMax, sumTotal, sumBytesRecv, sumBytesSent, sumErrors int64
	var sumUpSpeed, sumDownSpeed float64

	for _, s := range snapshots {
		sumActive += int64(s.ActiveConnections)
		sumMax += int64(s.MaxActiveConnections)
		sumTotal += s.TotalConnections
		sumBytesRecv += s.BytesReceived
		sumBytesSent += s.BytesSent
		sumUpSpeed += s.UploadSpeed
		sumDownSpeed += s.DownloadSpeed
		sumErrors += s.ErrorCount
	}

	count := int64(len(snapshots))

	// Calculate averages
	result.ActiveConnections = int(sumActive / count)
	result.MaxActiveConnections = int(sumMax / count)
	result.TotalConnections = sumTotal / count
	result.BytesReceived = sumBytesRecv / count
	result.BytesSent = sumBytesSent / count
	result.UploadSpeed = sumUpSpeed / float64(count)
	result.DownloadSpeed = sumDownSpeed / float64(count)
	result.ErrorCount = sumErrors / count

	return result
}

// Stop stops the background aggregation
func (c *Collector) Stop() {
	close(c.stopChan)
}

// Reset resets all metrics counters
func (c *Collector) Reset() {
	atomic.StoreInt32(&c.activeConnections, 0)
	atomic.StoreInt32(&c.maxActiveConnections, 0)
	atomic.StoreInt64(&c.totalConnections, 0)
	atomic.StoreInt64(&c.bytesReceived, 0)
	atomic.StoreInt64(&c.bytesSent, 0)
	atomic.StoreInt64(&c.errorCount, 0)

	c.mu.Lock()
	c.startTime = time.Now()
	c.lastSnapshot = time.Now()
	c.lastBytesReceived = 0
	c.lastBytesSent = 0
	c.uploadSpeed = 0
	c.downloadSpeed = 0
	c.maxUploadSpeed = 0
	c.maxDownloadSpeed = 0
	c.mu.Unlock()
}
