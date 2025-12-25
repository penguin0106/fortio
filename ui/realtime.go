package ui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"fortio.org/log"
)

// PrometheusMetric represents a parsed Prometheus metric
type PrometheusMetric struct {
	Name  string
	Value float64
}

// ParsePrometheusMetrics parses Prometheus text format metrics
func ParsePrometheusMetrics(data string) []PrometheusMetric {
	var metrics []PrometheusMetric
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Parse metric line: metric_name{labels} value
		// or simple: metric_name value
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			name := parts[0]
			// Remove labels if present
			if idx := strings.Index(name, "{"); idx > 0 {
				name = name[:idx]
			}
			if val, err := strconv.ParseFloat(parts[len(parts)-1], 64); err == nil {
				metrics = append(metrics, PrometheusMetric{Name: name, Value: val})
			}
		}
	}
	return metrics
}

// FetchConsumerMetrics fetches metrics from a Prometheus endpoint
func FetchConsumerMetrics(url string) ([]PrometheusMetric, error) {
	// Ensure URL has /metrics
	if !strings.HasSuffix(url, "/metrics") && !strings.Contains(url, "/metrics") {
		if !strings.HasSuffix(url, "/") {
			url += "/"
		}
		url += "metrics"
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}

	return ParsePrometheusMetrics(sb.String()), nil
}

// TimeSeriesPoint represents a single data point in time series
type TimeSeriesPoint struct {
	Time  float64 `json:"t"` // Seconds since start
	Value float64 `json:"v"`
}

// MetricTimeSeries holds time series data for a named metric
type MetricTimeSeries struct {
	Name        string            `json:"name"`
	Label       string            `json:"label,omitempty"`       // Human-readable label
	Unit        string            `json:"unit,omitempty"`        // e.g., "ms", "bytes", "count"
	Color       string            `json:"color,omitempty"`       // Chart color
	ServiceName string            `json:"serviceName,omitempty"` // Service name for multi-consumer support
	Points      []TimeSeriesPoint `json:"points"`
}

// ConsumerServiceConfig holds consumer service configuration
type ConsumerServiceConfig struct {
	Name string `json:"name"` // User-defined service name
	URL  string `json:"url"`  // Metrics endpoint URL
}

// ConsumerServiceInfo holds info about a consumer service and its metrics
type ConsumerServiceInfo struct {
	Name    string             `json:"name"`    // User-defined service name
	URL     string             `json:"url"`     // Metrics endpoint URL
	Metrics []MetricTimeSeries `json:"metrics"` // Metrics for this service
}

// LiveProgress holds real-time test progress data
type LiveProgress struct {
	RunID           int64     `json:"runId"`
	Status          string    `json:"status"` // "running", "completed", "error"
	StartTime       time.Time `json:"startTime"`
	ElapsedSeconds  float64   `json:"elapsedSeconds"`
	ExpectedSeconds float64   `json:"expectedSeconds"`
	ProgressPercent float64   `json:"progressPercent"`

	// Live stats
	RequestsTotal   int64   `json:"requestsTotal"`
	RequestsSuccess int64   `json:"requestsSuccess"`
	RequestsError   int64   `json:"requestsError"`
	CurrentQPS      float64 `json:"currentQps"`
	TargetQPS       float64 `json:"targetQps"`

	// Latency stats (in ms)
	LatencyMin float64 `json:"latencyMin"`
	LatencyAvg float64 `json:"latencyAvg"`
	LatencyMax float64 `json:"latencyMax"`
	LatencyP50 float64 `json:"latencyP50"`
	LatencyP90 float64 `json:"latencyP90"`
	LatencyP99 float64 `json:"latencyP99"`

	// Kafka specific
	KafkaMessagesSent int64  `json:"kafkaMessagesSent,omitempty"`
	KafkaBytesSent    int64  `json:"kafkaBytesSent,omitempty"`
	KafkaTopic        string `json:"kafkaTopic,omitempty"`

	// Kafka metrics time series (all metrics in one chart)
	KafkaMetrics []MetricTimeSeries `json:"kafkaMetrics,omitempty"`

	// Consumer metrics time series (each metric gets its own chart)
	ConsumerMetrics []MetricTimeSeries `json:"consumerMetrics,omitempty"`

	// Multiple consumer services support
	ConsumerServices []ConsumerServiceInfo `json:"consumerServices,omitempty"`

	// Legacy chart data (for backwards compatibility)
	ChartQPS     []TimeSeriesPoint `json:"chartQps,omitempty"`
	ChartLatency []TimeSeriesPoint `json:"chartLatency,omitempty"`

	// Error info
	LastError string `json:"lastError,omitempty"`
}

// progressStore holds all active test progress
var progressStore = struct {
	sync.RWMutex
	runs map[int64]*LiveProgress
}{
	runs: make(map[int64]*LiveProgress),
}

// subscribers for SSE
var sseSubscribers = struct {
	sync.RWMutex
	clients map[int64][]chan *LiveProgress
}{
	clients: make(map[int64][]chan *LiveProgress),
}

// UpdateProgress updates the progress for a specific run
func UpdateProgress(runID int64, progress *LiveProgress) {
	progressStore.Lock()
	progressStore.runs[runID] = progress
	progressStore.Unlock()

	// Notify SSE subscribers
	notifySubscribers(runID, progress)
}

// GetProgress returns the current progress for a run
func GetProgress(runID int64) *LiveProgress {
	progressStore.RLock()
	defer progressStore.RUnlock()
	return progressStore.runs[runID]
}

// ClearProgress removes progress data for a completed run
func ClearProgress(runID int64) {
	progressStore.Lock()
	delete(progressStore.runs, runID)
	progressStore.Unlock()
}

// notifySubscribers sends progress to all SSE subscribers
func notifySubscribers(runID int64, progress *LiveProgress) {
	sseSubscribers.RLock()
	clients := sseSubscribers.clients[runID]
	sseSubscribers.RUnlock()

	for _, ch := range clients {
		select {
		case ch <- progress:
		default:
			// Channel full, skip this update
		}
	}
}

// addSubscriber adds a new SSE subscriber for a run
func addSubscriber(runID int64) chan *LiveProgress {
	ch := make(chan *LiveProgress, 10)
	sseSubscribers.Lock()
	sseSubscribers.clients[runID] = append(sseSubscribers.clients[runID], ch)
	sseSubscribers.Unlock()
	return ch
}

// removeSubscriber removes an SSE subscriber
func removeSubscriber(runID int64, ch chan *LiveProgress) {
	sseSubscribers.Lock()
	defer sseSubscribers.Unlock()

	clients := sseSubscribers.clients[runID]
	for i, c := range clients {
		if c == ch {
			sseSubscribers.clients[runID] = append(clients[:i], clients[i+1:]...)
			close(ch)
			break
		}
	}

	// Clean up if no more subscribers
	if len(sseSubscribers.clients[runID]) == 0 {
		delete(sseSubscribers.clients, runID)
	}
}

// ProgressSSEHandler handles Server-Sent Events for real-time progress
func ProgressSSEHandler(w http.ResponseWriter, r *http.Request) {
	runIDStr := r.URL.Query().Get("runid")
	var runID int64
	_, err := fmt.Sscanf(runIDStr, "%d", &runID)
	if err != nil || runID == 0 {
		http.Error(w, "Invalid runid", http.StatusBadRequest)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Subscribe to updates
	ch := addSubscriber(runID)
	defer removeSubscriber(runID, ch)

	// Send current state if available
	if progress := GetProgress(runID); progress != nil {
		sendSSEEvent(w, flusher, progress)
	}

	log.Infof("SSE client connected for run %d", runID)

	// Keep connection open and send updates
	for {
		select {
		case progress, ok := <-ch:
			if !ok {
				return
			}
			sendSSEEvent(w, flusher, progress)

			// Close connection if test completed
			if progress.Status == "completed" || progress.Status == "error" {
				log.Infof("SSE closing for run %d (status: %s)", runID, progress.Status)
				return
			}

		case <-r.Context().Done():
			log.Infof("SSE client disconnected for run %d", runID)
			return
		}
	}
}

func sendSSEEvent(w http.ResponseWriter, flusher http.Flusher, progress *LiveProgress) {
	data, err := json.Marshal(progress)
	if err != nil {
		log.Errf("Failed to marshal progress: %v", err)
		return
	}

	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

// ProgressAPIHandler returns current progress as JSON (for polling fallback)
func ProgressAPIHandler(w http.ResponseWriter, r *http.Request) {
	runIDStr := r.URL.Query().Get("runid")
	var runID int64
	_, err := fmt.Sscanf(runIDStr, "%d", &runID)
	if err != nil || runID == 0 {
		http.Error(w, "Invalid runid", http.StatusBadRequest)
		return
	}

	progress := GetProgress(runID)
	if progress == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"not_found"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(progress)
}

// ProgressMonitorConfig holds configuration for progress monitoring
type ProgressMonitorConfig struct {
	RunID           int64
	TargetQPS       float64
	ExpectedSeconds float64
	RunType         string // "http", "grpc", "kafka", "tcp", "udp"
	KafkaTopic      string // For Kafka runs
}

// StartProgressMonitor starts a goroutine that monitors RunnerOptions and sends progress updates
// Returns a stop function that should be called when the test completes
func StartProgressMonitor(cfg *ProgressMonitorConfig, getStats func() (total, success, errors int64, avgMs, minMs, maxMs float64)) func(status string) {
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})

	// Initialize progress
	progress := &LiveProgress{
		RunID:           cfg.RunID,
		Status:          "running",
		StartTime:       time.Now(),
		ExpectedSeconds: cfg.ExpectedSeconds,
		TargetQPS:       cfg.TargetQPS,
		KafkaTopic:      cfg.KafkaTopic,
	}

	UpdateProgress(cfg.RunID, progress)

	go func() {
		defer close(doneCh)
		ticker := time.NewTicker(250 * time.Millisecond) // Update every 250ms
		defer ticker.Stop()

		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				// Get current stats
				total, success, errors, avgMs, minMs, maxMs := getStats()

				elapsed := time.Since(progress.StartTime).Seconds()
				var progressPercent float64
				if cfg.ExpectedSeconds > 0 {
					progressPercent = (elapsed / cfg.ExpectedSeconds) * 100
					if progressPercent > 100 {
						progressPercent = 100
					}
				}

				var currentQPS float64
				if elapsed > 0 {
					currentQPS = float64(total) / elapsed
				}

				// Update progress
				progress.ElapsedSeconds = elapsed
				progress.ProgressPercent = progressPercent
				progress.RequestsTotal = total
				progress.RequestsSuccess = success
				progress.RequestsError = errors
				progress.CurrentQPS = currentQPS
				progress.LatencyAvg = avgMs
				progress.LatencyMin = minMs
				progress.LatencyMax = maxMs

				UpdateProgress(cfg.RunID, progress)
			}
		}
	}()

	// Return stop function
	return func(status string) {
		close(stopCh)
		<-doneCh // Wait for goroutine to finish

		// Final update
		progress.Status = status
		progress.ElapsedSeconds = time.Since(progress.StartTime).Seconds()
		if cfg.ExpectedSeconds > 0 {
			progress.ProgressPercent = 100
		}

		// Get final stats
		total, success, errors, avgMs, minMs, maxMs := getStats()
		progress.RequestsTotal = total
		progress.RequestsSuccess = success
		progress.RequestsError = errors
		progress.LatencyAvg = avgMs
		progress.LatencyMin = minMs
		progress.LatencyMax = maxMs
		if progress.ElapsedSeconds > 0 {
			progress.CurrentQPS = float64(total) / progress.ElapsedSeconds
		}

		UpdateProgress(cfg.RunID, progress)

		// Clean up after a delay (let clients receive final status)
		go func() {
			time.Sleep(5 * time.Second)
			ClearProgress(cfg.RunID)
		}()
	}
}
