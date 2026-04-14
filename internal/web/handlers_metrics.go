package web

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/apeming/go-proxy-server/internal/metrics"
	"github.com/apeming/go-proxy-server/internal/models"
)

func (wm *Manager) handleMetricsRealtime(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	collector := metrics.GetCollector()
	if collector == nil {
		http.Error(w, "Metrics collector not initialized", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, collector.GetSnapshot())
}

func (wm *Manager) handleMetricsHistory(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	collector := metrics.GetCollector()
	if collector == nil {
		http.Error(w, "Metrics collector not initialized", http.StatusInternalServerError)
		return
	}

	start, end, limit := parseMetricsQuery(r)
	downsample := r.URL.Query().Get("downsample")

	var (
		snapshots []models.MetricsSnapshot
		err       error
	)
	if downsample == "true" || downsample == "1" {
		snapshots, err = collector.GetDownsampledSnapshots(start, end, limit)
	} else {
		snapshots, err = collector.GetHistoricalSnapshots(start, end, limit)
	}
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to retrieve metrics: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, snapshots)
}

func parseMetricsQuery(r *http.Request) (int64, int64, int) {
	query := r.URL.Query()

	start := parseInt64QueryValue(query.Get("startTime"), time.Now().Add(-24*time.Hour).Unix())
	end := parseInt64QueryValue(query.Get("endTime"), time.Now().Unix())
	limit := parsePositiveIntQueryValue(query.Get("limit"), 100)

	return start, end, limit
}

func parseInt64QueryValue(raw string, fallback int64) int64 {
	if raw == "" {
		return fallback
	}

	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}

	return value
}

func parsePositiveIntQueryValue(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}

	return value
}
