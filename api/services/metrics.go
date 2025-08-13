package services

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// MetricsCollector handles Prometheus metrics collection
type MetricsCollector struct {
	// HTTP metrics
	httpRequestsTotal   *prometheus.CounterVec
	httpRequestDuration *prometheus.HistogramVec
	httpRequestSize     *prometheus.HistogramVec
	httpResponseSize    *prometheus.HistogramVec

	// Audio processing metrics
	audioFilesUploaded      prometheus.Counter
	audioProcessingDuration *prometheus.HistogramVec
	audioProcessingErrors   *prometheus.CounterVec
	audioFileSize           prometheus.Histogram
	audioDuration           prometheus.Histogram

	// Transcription metrics
	transcriptionJobs     *prometheus.CounterVec
	transcriptionDuration *prometheus.HistogramVec
	transcriptionSegments prometheus.Histogram
	transcriptionErrors   *prometheus.CounterVec
	transcriptionAccuracy prometheus.Histogram

	// Cache metrics
	cacheHits    *prometheus.CounterVec
	cacheMisses  *prometheus.CounterVec
	cacheSize    *prometheus.GaugeVec
	cacheLatency *prometheus.HistogramVec

	// Queue metrics
	queueJobs           *prometheus.GaugeVec
	queueProcessingTime *prometheus.HistogramVec
	queueWorkers        *prometheus.GaugeVec
	queueThroughput     *prometheus.CounterVec

	// WebSocket metrics
	wsConnections        prometheus.Gauge
	wsMessages           *prometheus.CounterVec
	wsConnectionDuration prometheus.Histogram
	wsStreamingSessions  prometheus.Gauge

	// System metrics
	systemInfo      *prometheus.GaugeVec
	dbConnections   prometheus.Gauge
	dbQueries       *prometheus.CounterVec
	dbQueryDuration *prometheus.HistogramVec
	memoryUsage     prometheus.Gauge
	cpuUsage        prometheus.Gauge

	// AI Service metrics
	aiServiceHealth *prometheus.GaugeVec
	aiModelLoad     *prometheus.GaugeVec
	aiInferenceTime *prometheus.HistogramVec
	aiMemoryUsage   *prometheus.GaugeVec

	// Custom business metrics
	activeUsers         prometheus.Gauge
	totalProcessedAudio prometheus.Counter
	avgSessionDuration  prometheus.Histogram
	userSatisfaction    prometheus.Histogram

	mu sync.RWMutex
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		// HTTP metrics
		httpRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "pocwhisp_http_requests_total",
				Help: "Total number of HTTP requests",
			},
			[]string{"method", "endpoint", "status_code"},
		),
		httpRequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "pocwhisp_http_request_duration_seconds",
				Help:    "Duration of HTTP requests in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "endpoint"},
		),
		httpRequestSize: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "pocwhisp_http_request_size_bytes",
				Help:    "Size of HTTP requests in bytes",
				Buckets: prometheus.ExponentialBuckets(1024, 2, 10), // 1KB to 1MB
			},
			[]string{"method", "endpoint"},
		),
		httpResponseSize: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "pocwhisp_http_response_size_bytes",
				Help:    "Size of HTTP responses in bytes",
				Buckets: prometheus.ExponentialBuckets(1024, 2, 10),
			},
			[]string{"method", "endpoint"},
		),

		// Audio processing metrics
		audioFilesUploaded: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "pocwhisp_audio_files_uploaded_total",
				Help: "Total number of audio files uploaded",
			},
		),
		audioProcessingDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "pocwhisp_audio_processing_duration_seconds",
				Help:    "Duration of audio processing in seconds",
				Buckets: []float64{0.1, 0.5, 1.0, 2.0, 5.0, 10.0, 30.0, 60.0, 120.0},
			},
			[]string{"processing_type", "file_format"},
		),
		audioProcessingErrors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "pocwhisp_audio_processing_errors_total",
				Help: "Total number of audio processing errors",
			},
			[]string{"error_type", "processing_stage"},
		),
		audioFileSize: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "pocwhisp_audio_file_size_bytes",
				Help:    "Size of uploaded audio files in bytes",
				Buckets: prometheus.ExponentialBuckets(1024*1024, 2, 10), // 1MB to 1GB
			},
		),
		audioDuration: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "pocwhisp_audio_duration_seconds",
				Help:    "Duration of uploaded audio files in seconds",
				Buckets: []float64{5, 10, 30, 60, 120, 300, 600, 1800, 3600},
			},
		),

		// Transcription metrics
		transcriptionJobs: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "pocwhisp_transcription_jobs_total",
				Help: "Total number of transcription jobs",
			},
			[]string{"status", "model"},
		),
		transcriptionDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "pocwhisp_transcription_duration_seconds",
				Help:    "Duration of transcription processing",
				Buckets: []float64{0.5, 1.0, 2.0, 5.0, 10.0, 30.0, 60.0},
			},
			[]string{"model", "language"},
		),
		transcriptionSegments: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "pocwhisp_transcription_segments_count",
				Help:    "Number of segments in transcription results",
				Buckets: []float64{1, 5, 10, 20, 50, 100, 200, 500},
			},
		),
		transcriptionErrors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "pocwhisp_transcription_errors_total",
				Help: "Total number of transcription errors",
			},
			[]string{"error_type", "model"},
		),
		transcriptionAccuracy: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "pocwhisp_transcription_accuracy_score",
				Help:    "Transcription accuracy score (0-1)",
				Buckets: []float64{0.5, 0.6, 0.7, 0.8, 0.85, 0.9, 0.95, 0.98, 0.99},
			},
		),

		// Cache metrics
		cacheHits: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "pocwhisp_cache_hits_total",
				Help: "Total number of cache hits",
			},
			[]string{"cache_level", "cache_type"},
		),
		cacheMisses: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "pocwhisp_cache_misses_total",
				Help: "Total number of cache misses",
			},
			[]string{"cache_level", "cache_type"},
		),
		cacheSize: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "pocwhisp_cache_size_items",
				Help: "Number of items in cache",
			},
			[]string{"cache_level", "cache_type"},
		),
		cacheLatency: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "pocwhisp_cache_operation_duration_seconds",
				Help:    "Duration of cache operations",
				Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0},
			},
			[]string{"operation", "cache_level"},
		),

		// Queue metrics
		queueJobs: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "pocwhisp_queue_jobs_count",
				Help: "Number of jobs in queue",
			},
			[]string{"status", "job_type", "priority"},
		),
		queueProcessingTime: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "pocwhisp_queue_job_processing_duration_seconds",
				Help:    "Duration of job processing",
				Buckets: []float64{0.1, 0.5, 1.0, 5.0, 10.0, 30.0, 60.0, 300.0},
			},
			[]string{"job_type", "worker_id"},
		),
		queueWorkers: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "pocwhisp_queue_workers_count",
				Help: "Number of active queue workers",
			},
			[]string{"queue", "status"},
		),
		queueThroughput: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "pocwhisp_queue_throughput_total",
				Help: "Total number of jobs processed",
			},
			[]string{"job_type", "status"},
		),

		// WebSocket metrics
		wsConnections: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "pocwhisp_websocket_connections_active",
				Help: "Number of active WebSocket connections",
			},
		),
		wsMessages: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "pocwhisp_websocket_messages_total",
				Help: "Total number of WebSocket messages",
			},
			[]string{"direction", "message_type"},
		),
		wsConnectionDuration: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "pocwhisp_websocket_connection_duration_seconds",
				Help:    "Duration of WebSocket connections",
				Buckets: []float64{1, 5, 10, 30, 60, 300, 600, 1800, 3600},
			},
		),
		wsStreamingSessions: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "pocwhisp_websocket_streaming_sessions_active",
				Help: "Number of active streaming sessions",
			},
		),

		// System metrics
		systemInfo: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "pocwhisp_system_info",
				Help: "System information",
			},
			[]string{"version", "go_version", "build_date"},
		),
		dbConnections: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "pocwhisp_db_connections_active",
				Help: "Number of active database connections",
			},
		),
		dbQueries: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "pocwhisp_db_queries_total",
				Help: "Total number of database queries",
			},
			[]string{"operation", "table"},
		),
		dbQueryDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "pocwhisp_db_query_duration_seconds",
				Help:    "Duration of database queries",
				Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 5.0},
			},
			[]string{"operation", "table"},
		),
		memoryUsage: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "pocwhisp_memory_usage_bytes",
				Help: "Memory usage in bytes",
			},
		),
		cpuUsage: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "pocwhisp_cpu_usage_percent",
				Help: "CPU usage percentage",
			},
		),

		// AI Service metrics
		aiServiceHealth: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "pocwhisp_ai_service_health",
				Help: "AI service health status (1=healthy, 0=unhealthy)",
			},
			[]string{"service", "model"},
		),
		aiModelLoad: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "pocwhisp_ai_model_loaded",
				Help: "AI model load status (1=loaded, 0=not loaded)",
			},
			[]string{"model", "device"},
		),
		aiInferenceTime: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "pocwhisp_ai_inference_duration_seconds",
				Help:    "Duration of AI model inference",
				Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1.0, 2.0, 5.0, 10.0},
			},
			[]string{"model", "device"},
		),
		aiMemoryUsage: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "pocwhisp_ai_memory_usage_bytes",
				Help: "AI service memory usage in bytes",
			},
			[]string{"model", "device"},
		),

		// Custom business metrics
		activeUsers: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "pocwhisp_active_users_count",
				Help: "Number of active users",
			},
		),
		totalProcessedAudio: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "pocwhisp_processed_audio_duration_seconds_total",
				Help: "Total duration of processed audio in seconds",
			},
		),
		avgSessionDuration: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "pocwhisp_session_duration_seconds",
				Help:    "Duration of user sessions",
				Buckets: []float64{60, 300, 600, 1800, 3600, 7200, 14400},
			},
		),
		userSatisfaction: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "pocwhisp_user_satisfaction_score",
				Help:    "User satisfaction score (1-5)",
				Buckets: []float64{1, 2, 3, 4, 5},
			},
		),
	}
}

// HTTP Metrics
func (m *MetricsCollector) RecordHTTPRequest(method, endpoint, statusCode string, duration time.Duration, requestSize, responseSize int) {
	m.httpRequestsTotal.WithLabelValues(method, endpoint, statusCode).Inc()
	m.httpRequestDuration.WithLabelValues(method, endpoint).Observe(duration.Seconds())
	m.httpRequestSize.WithLabelValues(method, endpoint).Observe(float64(requestSize))
	m.httpResponseSize.WithLabelValues(method, endpoint).Observe(float64(responseSize))
}

// Audio Processing Metrics
func (m *MetricsCollector) RecordAudioUpload(fileSize, duration float64) {
	m.audioFilesUploaded.Inc()
	m.audioFileSize.Observe(fileSize)
	m.audioDuration.Observe(duration)
}

func (m *MetricsCollector) RecordAudioProcessing(processingType, fileFormat string, duration time.Duration) {
	m.audioProcessingDuration.WithLabelValues(processingType, fileFormat).Observe(duration.Seconds())
}

func (m *MetricsCollector) RecordAudioProcessingError(errorType, stage string) {
	m.audioProcessingErrors.WithLabelValues(errorType, stage).Inc()
}

// Transcription Metrics
func (m *MetricsCollector) RecordTranscriptionJob(status, model string) {
	m.transcriptionJobs.WithLabelValues(status, model).Inc()
}

func (m *MetricsCollector) RecordTranscriptionDuration(model, language string, duration time.Duration) {
	m.transcriptionDuration.WithLabelValues(model, language).Observe(duration.Seconds())
}

func (m *MetricsCollector) RecordTranscriptionResult(segmentCount int, accuracy float64) {
	m.transcriptionSegments.Observe(float64(segmentCount))
	m.transcriptionAccuracy.Observe(accuracy)
}

func (m *MetricsCollector) RecordTranscriptionError(errorType, model string) {
	m.transcriptionErrors.WithLabelValues(errorType, model).Inc()
}

// Cache Metrics
func (m *MetricsCollector) RecordCacheHit(level, cacheType string) {
	m.cacheHits.WithLabelValues(level, cacheType).Inc()
}

func (m *MetricsCollector) RecordCacheMiss(level, cacheType string) {
	m.cacheMisses.WithLabelValues(level, cacheType).Inc()
}

func (m *MetricsCollector) UpdateCacheSize(level, cacheType string, size float64) {
	m.cacheSize.WithLabelValues(level, cacheType).Set(size)
}

func (m *MetricsCollector) RecordCacheOperation(operation, level string, duration time.Duration) {
	m.cacheLatency.WithLabelValues(operation, level).Observe(duration.Seconds())
}

// Queue Metrics
func (m *MetricsCollector) UpdateQueueJobs(status, jobType, priority string, count float64) {
	m.queueJobs.WithLabelValues(status, jobType, priority).Set(count)
}

func (m *MetricsCollector) RecordJobProcessing(jobType, workerID string, duration time.Duration) {
	m.queueProcessingTime.WithLabelValues(jobType, workerID).Observe(duration.Seconds())
}

func (m *MetricsCollector) UpdateQueueWorkers(queue, status string, count float64) {
	m.queueWorkers.WithLabelValues(queue, status).Set(count)
}

func (m *MetricsCollector) RecordJobThroughput(jobType, status string) {
	m.queueThroughput.WithLabelValues(jobType, status).Inc()
}

// WebSocket Metrics
func (m *MetricsCollector) UpdateWebSocketConnections(count float64) {
	m.wsConnections.Set(count)
}

func (m *MetricsCollector) RecordWebSocketMessage(direction, messageType string) {
	m.wsMessages.WithLabelValues(direction, messageType).Inc()
}

func (m *MetricsCollector) RecordWebSocketConnectionDuration(duration time.Duration) {
	m.wsConnectionDuration.Observe(duration.Seconds())
}

func (m *MetricsCollector) UpdateStreamingSessions(count float64) {
	m.wsStreamingSessions.Set(count)
}

// System Metrics
func (m *MetricsCollector) UpdateSystemInfo(version, goVersion, buildDate string) {
	m.systemInfo.WithLabelValues(version, goVersion, buildDate).Set(1)
}

func (m *MetricsCollector) UpdateDBConnections(count float64) {
	m.dbConnections.Set(count)
}

func (m *MetricsCollector) RecordDBQuery(operation, table string, duration time.Duration) {
	m.dbQueries.WithLabelValues(operation, table).Inc()
	m.dbQueryDuration.WithLabelValues(operation, table).Observe(duration.Seconds())
}

func (m *MetricsCollector) UpdateMemoryUsage(bytes float64) {
	m.memoryUsage.Set(bytes)
}

func (m *MetricsCollector) UpdateCPUUsage(percent float64) {
	m.cpuUsage.Set(percent)
}

// AI Service Metrics
func (m *MetricsCollector) UpdateAIServiceHealth(service, model string, healthy bool) {
	value := 0.0
	if healthy {
		value = 1.0
	}
	m.aiServiceHealth.WithLabelValues(service, model).Set(value)
}

func (m *MetricsCollector) UpdateAIModelLoad(model, device string, loaded bool) {
	value := 0.0
	if loaded {
		value = 1.0
	}
	m.aiModelLoad.WithLabelValues(model, device).Set(value)
}

func (m *MetricsCollector) RecordAIInference(model, device string, duration time.Duration) {
	m.aiInferenceTime.WithLabelValues(model, device).Observe(duration.Seconds())
}

func (m *MetricsCollector) UpdateAIMemoryUsage(model, device string, bytes float64) {
	m.aiMemoryUsage.WithLabelValues(model, device).Set(bytes)
}

// Business Metrics
func (m *MetricsCollector) UpdateActiveUsers(count float64) {
	m.activeUsers.Set(count)
}

func (m *MetricsCollector) RecordProcessedAudio(duration float64) {
	m.totalProcessedAudio.Add(duration)
}

func (m *MetricsCollector) RecordSessionDuration(duration time.Duration) {
	m.avgSessionDuration.Observe(duration.Seconds())
}

func (m *MetricsCollector) RecordUserSatisfaction(score float64) {
	m.userSatisfaction.Observe(score)
}

// Global metrics instance
var globalMetrics *MetricsCollector

// InitializeMetrics initializes the global metrics collector
func InitializeMetrics() {
	globalMetrics = NewMetricsCollector()
}

// GetMetrics returns the global metrics collector
func GetMetrics() *MetricsCollector {
	return globalMetrics
}
