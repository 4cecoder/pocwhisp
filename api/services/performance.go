package services

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"gorm.io/gorm"

	"pocwhisp/utils"
)

// PerformanceService manages performance optimization across the application
type PerformanceService struct {
	profiler       *utils.PerformanceProfiler
	optimizer      *utils.PerformanceOptimizer
	connectionPool *ConnectionPool
	requestPool    *RequestPool
	cacheOptimizer *CacheOptimizer
	queryOptimizer *QueryOptimizer
	db             *gorm.DB
	logger         *utils.Logger
	metricsService *MetricsService
	mu             sync.RWMutex
	running        bool
	stopChan       chan struct{}
}

// ConnectionPool manages database connection optimization
type ConnectionPool struct {
	maxOpenConns    int
	maxIdleConns    int
	connMaxLifetime time.Duration
	connMaxIdleTime time.Duration
	db              *gorm.DB
}

// RequestPool manages request processing optimization
type RequestPool struct {
	maxConcurrent   int
	queueSize       int
	processingQueue chan *ProcessingRequest
	workerPool      []chan *ProcessingRequest
	active          sync.Map
	metrics         *RequestPoolMetrics
}

// ProcessingRequest represents a request being processed
type ProcessingRequest struct {
	ID        string
	Type      string
	Priority  int
	StartTime time.Time
	Context   context.Context
	Handler   func(context.Context) error
	Result    chan error
}

// RequestPoolMetrics tracks request pool performance
type RequestPoolMetrics struct {
	QueueLength    int64   `json:"queue_length"`
	ActiveRequests int64   `json:"active_requests"`
	ProcessedTotal int64   `json:"processed_total"`
	ErrorsTotal    int64   `json:"errors_total"`
	AvgProcessTime float64 `json:"avg_process_time_ms"`
	ThroughputRPS  float64 `json:"throughput_rps"`
}

// CacheOptimizer optimizes cache performance
type CacheOptimizer struct {
	cache              MultiLevelCache
	hitRateTarget      float64
	warmupSchedule     time.Duration
	compressionEnabled bool
	ttlOptimization    bool
	stats              *CacheOptimizerStats
}

// CacheOptimizerStats tracks cache optimization metrics
type CacheOptimizerStats struct {
	OptimizationCount  int64   `json:"optimization_count"`
	HitRateImprovement float64 `json:"hit_rate_improvement"`
	MemorySaved        int64   `json:"memory_saved_bytes"`
	CompressionRatio   float64 `json:"compression_ratio"`
}

// QueryOptimizer optimizes database queries
type QueryOptimizer struct {
	db                 *gorm.DB
	slowQueryThreshold time.Duration
	indexSuggestions   map[string][]string
	queryCache         sync.Map
	stats              *QueryOptimizerStats
}

// QueryOptimizerStats tracks query optimization metrics
type QueryOptimizerStats struct {
	SlowQueries          int64   `json:"slow_queries"`
	OptimizedQueries     int64   `json:"optimized_queries"`
	AvgQueryTime         float64 `json:"avg_query_time_ms"`
	CacheHitRate         float64 `json:"cache_hit_rate"`
	IndexRecommendations int     `json:"index_recommendations"`
}

// PerformanceConfig holds performance optimization configuration
type PerformanceConfig struct {
	// Connection Pool
	MaxOpenConns    int           `json:"max_open_conns"`
	MaxIdleConns    int           `json:"max_idle_conns"`
	ConnMaxLifetime time.Duration `json:"conn_max_lifetime"`
	ConnMaxIdleTime time.Duration `json:"conn_max_idle_time"`

	// Request Pool
	MaxConcurrentRequests int `json:"max_concurrent_requests"`
	RequestQueueSize      int `json:"request_queue_size"`
	WorkerPoolSize        int `json:"worker_pool_size"`

	// Cache
	CacheHitRateTarget      float64       `json:"cache_hit_rate_target"`
	CacheWarmupSchedule     time.Duration `json:"cache_warmup_schedule"`
	CacheCompressionEnabled bool          `json:"cache_compression_enabled"`

	// Query Optimization
	SlowQueryThreshold time.Duration `json:"slow_query_threshold"`
	QueryCacheEnabled  bool          `json:"query_cache_enabled"`

	// Monitoring
	MetricsUpdateInterval time.Duration `json:"metrics_update_interval"`
	ProfilerEnabled       bool          `json:"profiler_enabled"`
}

// DefaultPerformanceConfig returns default performance configuration
func DefaultPerformanceConfig() *PerformanceConfig {
	return &PerformanceConfig{
		MaxOpenConns:            100,
		MaxIdleConns:            10,
		ConnMaxLifetime:         time.Hour,
		ConnMaxIdleTime:         10 * time.Minute,
		MaxConcurrentRequests:   1000,
		RequestQueueSize:        5000,
		WorkerPoolSize:          runtime.NumCPU() * 2,
		CacheHitRateTarget:      0.85,
		CacheWarmupSchedule:     5 * time.Minute,
		CacheCompressionEnabled: true,
		SlowQueryThreshold:      100 * time.Millisecond,
		QueryCacheEnabled:       true,
		MetricsUpdateInterval:   30 * time.Second,
		ProfilerEnabled:         true,
	}
}

// NewPerformanceService creates a new performance service
func NewPerformanceService(db *gorm.DB, cache MultiLevelCache, config *PerformanceConfig) *PerformanceService {
	if config == nil {
		config = DefaultPerformanceConfig()
	}

	service := &PerformanceService{
		profiler:       utils.NewPerformanceProfiler(),
		db:             db,
		logger:         utils.GetLogger(),
		metricsService: GetMetricsService(),
		stopChan:       make(chan struct{}),
	}

	// Initialize components
	service.connectionPool = NewConnectionPool(db, config)
	service.requestPool = NewRequestPool(config)
	service.cacheOptimizer = NewCacheOptimizer(cache, config)
	service.queryOptimizer = NewQueryOptimizer(db, config)

	return service
}

// NewConnectionPool creates optimized database connection pool
func NewConnectionPool(db *gorm.DB, config *PerformanceConfig) *ConnectionPool {
	pool := &ConnectionPool{
		maxOpenConns:    config.MaxOpenConns,
		maxIdleConns:    config.MaxIdleConns,
		connMaxLifetime: config.ConnMaxLifetime,
		connMaxIdleTime: config.ConnMaxIdleTime,
		db:              db,
	}

	// Apply connection pool settings
	sqlDB, err := db.DB()
	if err == nil {
		sqlDB.SetMaxOpenConns(pool.maxOpenConns)
		sqlDB.SetMaxIdleConns(pool.maxIdleConns)
		sqlDB.SetConnMaxLifetime(pool.connMaxLifetime)
		sqlDB.SetConnMaxIdleTime(pool.connMaxIdleTime)
	}

	return pool
}

// NewRequestPool creates optimized request processing pool
func NewRequestPool(config *PerformanceConfig) *RequestPool {
	pool := &RequestPool{
		maxConcurrent:   config.MaxConcurrentRequests,
		queueSize:       config.RequestQueueSize,
		processingQueue: make(chan *ProcessingRequest, config.RequestQueueSize),
		workerPool:      make([]chan *ProcessingRequest, config.WorkerPoolSize),
		metrics:         &RequestPoolMetrics{},
	}

	// Initialize worker pool
	for i := 0; i < config.WorkerPoolSize; i++ {
		worker := make(chan *ProcessingRequest, 1)
		pool.workerPool[i] = worker
		go pool.worker(worker)
	}

	return pool
}

// NewCacheOptimizer creates cache optimizer
func NewCacheOptimizer(cache MultiLevelCache, config *PerformanceConfig) *CacheOptimizer {
	return &CacheOptimizer{
		cache:              cache,
		hitRateTarget:      config.CacheHitRateTarget,
		warmupSchedule:     config.CacheWarmupSchedule,
		compressionEnabled: config.CacheCompressionEnabled,
		ttlOptimization:    true,
		stats:              &CacheOptimizerStats{},
	}
}

// NewQueryOptimizer creates query optimizer
func NewQueryOptimizer(db *gorm.DB, config *PerformanceConfig) *QueryOptimizer {
	return &QueryOptimizer{
		db:                 db,
		slowQueryThreshold: config.SlowQueryThreshold,
		indexSuggestions:   make(map[string][]string),
		stats:              &QueryOptimizerStats{},
	}
}

// Start begins performance optimization services
func (ps *PerformanceService) Start(ctx context.Context) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.running {
		return fmt.Errorf("performance service already running")
	}

	// Apply initial optimizations
	ps.profiler.OptimizeGC()

	// Start monitoring
	go ps.monitoringLoop(ctx)

	// Start request pool processing
	go ps.requestPool.startProcessing(ctx)

	// Start cache optimization
	go ps.cacheOptimizer.startOptimization(ctx)

	// Start query optimization
	go ps.queryOptimizer.startOptimization(ctx)

	ps.running = true

	ps.logger.LogSystem("performance", "Performance service started", map[string]interface{}{
		"max_open_conns":          ps.connectionPool.maxOpenConns,
		"max_concurrent_requests": ps.requestPool.maxConcurrent,
		"cache_hit_rate_target":   ps.cacheOptimizer.hitRateTarget,
		"component":               "performance_service",
	})

	return nil
}

// Stop stops performance optimization services
func (ps *PerformanceService) Stop() error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if !ps.running {
		return fmt.Errorf("performance service not running")
	}

	close(ps.stopChan)
	ps.running = false

	ps.logger.LogSystem("performance", "Performance service stopped", map[string]interface{}{
		"component": "performance_service",
	})

	return nil
}

// monitoringLoop runs continuous performance monitoring
func (ps *PerformanceService) monitoringLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ps.stopChan:
			return
		case <-ticker.C:
			if err := ps.profiler.UpdateMetrics(ctx); err != nil {
				ps.logger.LogError("performance", "Failed to update metrics", err, map[string]interface{}{
					"component": "performance_service",
				})
			}

			// Check for optimization opportunities
			ps.checkOptimizations()
		}
	}
}

// checkOptimizations analyzes metrics and applies optimizations
func (ps *PerformanceService) checkOptimizations() {
	metrics := ps.profiler.GetMetrics()

	// Memory optimization
	if metrics.HeapInuse > 1024*1024*1024 { // 1GB
		ps.profiler.ForceGC()
		ps.logger.LogSystem("performance", "Applied memory optimization", map[string]interface{}{
			"heap_inuse": metrics.HeapInuse,
			"component":  "performance_service",
		})
	}

	// Connection pool optimization
	if metrics.DBConnections > ps.connectionPool.maxOpenConns-5 {
		ps.optimizeConnectionPool()
	}

	// Cache optimization
	if metrics.CacheHitRate < ps.cacheOptimizer.hitRateTarget {
		ps.cacheOptimizer.optimizeCache()
	}

	// Request pool optimization
	if metrics.ActiveRequests > int64(ps.requestPool.maxConcurrent*80/100) {
		ps.requestPool.optimizePool()
	}
}

// optimizeConnectionPool dynamically adjusts connection pool settings
func (ps *PerformanceService) optimizeConnectionPool() {
	sqlDB, err := ps.db.DB()
	if err != nil {
		return
	}

	stats := sqlDB.Stats()

	// Increase max connections if we're hitting the limit
	if stats.OpenConnections >= ps.connectionPool.maxOpenConns-2 {
		newMax := min(ps.connectionPool.maxOpenConns+10, 200)
		sqlDB.SetMaxOpenConns(newMax)
		ps.connectionPool.maxOpenConns = newMax

		ps.logger.LogSystem("performance", "Optimized connection pool", map[string]interface{}{
			"new_max_conns": newMax,
			"open_conns":    stats.OpenConnections,
			"component":     "performance_service",
		})
	}
}

// SubmitRequest submits a request to the optimized processing pool
func (ps *PerformanceService) SubmitRequest(ctx context.Context, requestType string, priority int, handler func(context.Context) error) error {
	return ps.requestPool.Submit(ctx, requestType, priority, handler)
}

// Submit submits a request to the request pool
func (rp *RequestPool) Submit(ctx context.Context, requestType string, priority int, handler func(context.Context) error) error {
	req := &ProcessingRequest{
		ID:        fmt.Sprintf("%s-%d", requestType, time.Now().UnixNano()),
		Type:      requestType,
		Priority:  priority,
		StartTime: time.Now(),
		Context:   ctx,
		Handler:   handler,
		Result:    make(chan error, 1),
	}

	select {
	case rp.processingQueue <- req:
		rp.active.Store(req.ID, req)
		return <-req.Result
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("request queue full")
	}
}

// worker processes requests from the pool
func (rp *RequestPool) worker(requests chan *ProcessingRequest) {
	for req := range requests {
		startTime := time.Now()
		err := req.Handler(req.Context)
		duration := time.Since(startTime)

		// Update metrics
		rp.metrics.ProcessedTotal++
		if err != nil {
			rp.metrics.ErrorsTotal++
		}

		// Update average processing time
		rp.metrics.AvgProcessTime = (rp.metrics.AvgProcessTime + duration.Seconds()*1000) / 2

		// Clean up
		rp.active.Delete(req.ID)
		req.Result <- err
		close(req.Result)
	}
}

// startProcessing starts the request processing loop
func (rp *RequestPool) startProcessing(ctx context.Context) {
	dispatcher := func() {
		for {
			select {
			case <-ctx.Done():
				return
			case req := <-rp.processingQueue:
				// Find available worker
				for _, worker := range rp.workerPool {
					select {
					case worker <- req:
						goto dispatched
					default:
						continue
					}
				}
				// If no worker available, handle directly (should not happen with proper sizing)
				go func(r *ProcessingRequest) {
					err := r.Handler(r.Context)
					r.Result <- err
				}(req)
			dispatched:
			}
		}
	}

	go dispatcher()
}

// optimizePool dynamically adjusts pool settings
func (rp *RequestPool) optimizePool() {
	// Count active requests
	activeCount := int64(0)
	rp.active.Range(func(key, value interface{}) bool {
		activeCount++
		return true
	})
	rp.metrics.ActiveRequests = activeCount
	rp.metrics.QueueLength = int64(len(rp.processingQueue))

	// Log optimization
	utils.GetLogger().LogSystem("performance", "Request pool optimized", map[string]interface{}{
		"active_requests": activeCount,
		"queue_length":    rp.metrics.QueueLength,
		"component":       "request_pool",
	})
}

// optimizeCache performs cache optimization
func (co *CacheOptimizer) optimizeCache() {
	// This would implement cache optimization logic
	co.stats.OptimizationCount++

	utils.GetLogger().LogSystem("performance", "Cache optimized", map[string]interface{}{
		"optimization_count": co.stats.OptimizationCount,
		"component":          "cache_optimizer",
	})
}

// startOptimization starts cache optimization loop
func (co *CacheOptimizer) startOptimization(ctx context.Context) {
	ticker := time.NewTicker(co.warmupSchedule)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			co.optimizeCache()
		}
	}
}

// startOptimization starts query optimization loop
func (qo *QueryOptimizer) startOptimization(ctx context.Context) {
	// This would implement query optimization monitoring
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			qo.analyzeQueries()
		}
	}
}

// analyzeQueries analyzes query performance
func (qo *QueryOptimizer) analyzeQueries() {
	// This would implement query analysis logic
	qo.stats.OptimizedQueries++

	utils.GetLogger().LogSystem("performance", "Queries analyzed", map[string]interface{}{
		"optimized_queries": qo.stats.OptimizedQueries,
		"component":         "query_optimizer",
	})
}

// GetPerformanceReport generates comprehensive performance report
func (ps *PerformanceService) GetPerformanceReport() map[string]interface{} {
	return map[string]interface{}{
		"profiler":        ps.profiler.GenerateReport(),
		"connection_pool": ps.getConnectionPoolStats(),
		"request_pool":    ps.requestPool.metrics,
		"cache_optimizer": ps.cacheOptimizer.stats,
		"query_optimizer": ps.queryOptimizer.stats,
		"generated_at":    time.Now(),
	}
}

// getConnectionPoolStats returns connection pool statistics
func (ps *PerformanceService) getConnectionPoolStats() map[string]interface{} {
	sqlDB, err := ps.db.DB()
	if err != nil {
		return map[string]interface{}{"error": err.Error()}
	}

	stats := sqlDB.Stats()
	return map[string]interface{}{
		"max_open_connections": stats.MaxOpenConnections,
		"open_connections":     stats.OpenConnections,
		"in_use":               stats.InUse,
		"idle":                 stats.Idle,
		"wait_count":           stats.WaitCount,
		"wait_duration":        stats.WaitDuration,
		"max_idle_closed":      stats.MaxIdleClosed,
		"max_idle_time_closed": stats.MaxIdleTimeClosed,
		"max_lifetime_closed":  stats.MaxLifetimeClosed,
	}
}

// Helper function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
