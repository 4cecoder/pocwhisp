package utils

import (
	"context"
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"
)

// PerformanceProfiler provides comprehensive performance monitoring and optimization
type PerformanceProfiler struct {
	metrics     *PerformanceMetrics
	mu          sync.RWMutex
	startTime   time.Time
	gcOptimized bool
	poolManager *PoolManager
	logger      *Logger
}

// PerformanceMetrics tracks various performance indicators
type PerformanceMetrics struct {
	// CPU Metrics
	CPUUsagePercent float64 `json:"cpu_usage_percent"`
	GoroutineCount  int     `json:"goroutine_count"`
	ThreadCount     int     `json:"thread_count"`

	// Memory Metrics
	HeapAlloc      uint64 `json:"heap_alloc_bytes"`
	HeapSys        uint64 `json:"heap_sys_bytes"`
	HeapInuse      uint64 `json:"heap_inuse_bytes"`
	HeapIdle       uint64 `json:"heap_idle_bytes"`
	StackInuse     uint64 `json:"stack_inuse_bytes"`
	SystemMemUsage uint64 `json:"system_mem_usage_bytes"`
	SystemMemTotal uint64 `json:"system_mem_total_bytes"`

	// GC Metrics
	GCCount      uint32 `json:"gc_count"`
	GCPauseTotal uint64 `json:"gc_pause_total_ns"`
	GCPauseAvg   uint64 `json:"gc_pause_avg_ns"`
	NextGC       uint64 `json:"next_gc_bytes"`

	// Request Metrics
	ActiveRequests    int64   `json:"active_requests"`
	RequestLatencyP50 float64 `json:"request_latency_p50_ms"`
	RequestLatencyP95 float64 `json:"request_latency_p95_ms"`
	RequestLatencyP99 float64 `json:"request_latency_p99_ms"`

	// Database Metrics
	DBConnections     int   `json:"db_connections"`
	DBIdleConnections int   `json:"db_idle_connections"`
	DBQueries         int64 `json:"db_queries_count"`
	DBSlowQueries     int64 `json:"db_slow_queries_count"`

	// Cache Metrics
	CacheHitRate float64 `json:"cache_hit_rate"`
	CacheSize    int64   `json:"cache_size_bytes"`

	// Processing Metrics
	AudioProcessingQueue  int     `json:"audio_processing_queue"`
	ProcessingErrors      int64   `json:"processing_errors"`
	AverageProcessingTime float64 `json:"avg_processing_time_ms"`

	// System Metrics
	LoadAverage         []float64 `json:"load_average"`
	OpenFileDescriptors int       `json:"open_file_descriptors"`

	LastUpdated time.Time `json:"last_updated"`
}

// PoolManager manages object pools for performance optimization
type PoolManager struct {
	bufferPool   sync.Pool
	requestPool  sync.Pool
	responsePool sync.Pool
	segmentPool  sync.Pool
	contextPool  sync.Pool
}

// RequestContext represents a pooled request context
type RequestContext struct {
	ID          string
	StartTime   time.Time
	UserID      string
	RequestType string
	Metadata    map[string]interface{}
	BufferPool  *[]byte
}

// PerformanceOptimizer provides various optimization utilities
type PerformanceOptimizer struct {
	profiler       *PerformanceProfiler
	circuitBreaker *CircuitBreaker
	rateLimiter    *RateLimiter
	cacheWarmer    *CacheWarmer
}

// CircuitBreaker implements the circuit breaker pattern for performance protection
type CircuitBreaker struct {
	maxFailures  int
	resetTimeout time.Duration
	currentState CircuitState
	failures     int
	lastFailTime time.Time
	mu           sync.RWMutex
}

type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

// RateLimiter implements token bucket rate limiting
type RateLimiter struct {
	tokens     float64
	maxTokens  float64
	refillRate float64
	lastRefill time.Time
	mu         sync.Mutex
}

// CacheWarmer preloads frequently accessed data
type CacheWarmer struct {
	warmupFunctions []WarmupFunction
	schedule        time.Duration
	running         bool
	stopChan        chan struct{}
	mu              sync.RWMutex
}

type WarmupFunction func(ctx context.Context) error

// NewPerformanceProfiler creates a new performance profiler
func NewPerformanceProfiler() *PerformanceProfiler {
	return &PerformanceProfiler{
		metrics:     &PerformanceMetrics{},
		startTime:   time.Now(),
		poolManager: NewPoolManager(),
		logger:      GetLogger(),
	}
}

// NewPoolManager creates a new pool manager with optimized pools
func NewPoolManager() *PoolManager {
	return &PoolManager{
		bufferPool: sync.Pool{
			New: func() interface{} {
				buf := make([]byte, 8192) // 8KB buffer
				return &buf
			},
		},
		requestPool: sync.Pool{
			New: func() interface{} {
				return &RequestContext{
					Metadata: make(map[string]interface{}),
				}
			},
		},
		responsePool: sync.Pool{
			New: func() interface{} {
				return make(map[string]interface{})
			},
		},
		segmentPool: sync.Pool{
			New: func() interface{} {
				return make([]interface{}, 0, 100) // Pre-allocate for typical segment count
			},
		},
		contextPool: sync.Pool{
			New: func() interface{} {
				return make(map[string]interface{})
			},
		},
	}
}

// GetBuffer gets a buffer from the pool
func (pm *PoolManager) GetBuffer() *[]byte {
	return pm.bufferPool.Get().(*[]byte)
}

// PutBuffer returns a buffer to the pool
func (pm *PoolManager) PutBuffer(buf *[]byte) {
	if buf != nil && cap(*buf) <= 65536 { // Don't pool very large buffers
		*buf = (*buf)[:0] // Reset length but keep capacity
		pm.bufferPool.Put(buf)
	}
}

// GetRequestContext gets a request context from the pool
func (pm *PoolManager) GetRequestContext() *RequestContext {
	ctx := pm.requestPool.Get().(*RequestContext)
	ctx.StartTime = time.Now()
	// Clear previous data
	for k := range ctx.Metadata {
		delete(ctx.Metadata, k)
	}
	return ctx
}

// PutRequestContext returns a request context to the pool
func (pm *PoolManager) PutRequestContext(ctx *RequestContext) {
	if ctx != nil {
		ctx.ID = ""
		ctx.UserID = ""
		ctx.RequestType = ""
		ctx.BufferPool = nil
		pm.requestPool.Put(ctx)
	}
}

// UpdateMetrics collects and updates all performance metrics
func (pp *PerformanceProfiler) UpdateMetrics(ctx context.Context) error {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Update memory metrics
	pp.metrics.HeapAlloc = memStats.HeapAlloc
	pp.metrics.HeapSys = memStats.HeapSys
	pp.metrics.HeapInuse = memStats.HeapInuse
	pp.metrics.HeapIdle = memStats.HeapIdle
	pp.metrics.StackInuse = memStats.StackInuse

	// Update GC metrics
	pp.metrics.GCCount = memStats.NumGC
	pp.metrics.GCPauseTotal = memStats.PauseTotalNs
	if memStats.NumGC > 0 {
		pp.metrics.GCPauseAvg = memStats.PauseTotalNs / uint64(memStats.NumGC)
	}
	pp.metrics.NextGC = memStats.NextGC

	// Update goroutine count
	pp.metrics.GoroutineCount = runtime.NumGoroutine()

	// Update system metrics
	if err := pp.updateSystemMetrics(ctx); err != nil {
		pp.logger.LogError(err, "Failed to update system metrics", LogContext{
			Component: "performance_profiler",
		}, map[string]interface{}{
			"operation": "update_system_metrics",
		})
	}

	pp.metrics.LastUpdated = time.Now()
	return nil
}

// updateSystemMetrics updates system-level performance metrics
func (pp *PerformanceProfiler) updateSystemMetrics(ctx context.Context) error {
	// CPU usage
	cpuPercent, err := cpu.PercentWithContext(ctx, time.Second, false)
	if err == nil && len(cpuPercent) > 0 {
		pp.metrics.CPUUsagePercent = cpuPercent[0]
	}

	// Memory usage
	vmStat, err := mem.VirtualMemoryWithContext(ctx)
	if err == nil {
		pp.metrics.SystemMemUsage = vmStat.Used
		pp.metrics.SystemMemTotal = vmStat.Total
	}

	// Process-specific metrics
	pid := int32(runtime.GOMAXPROCS(0))
	proc, err := process.NewProcess(pid)
	if err == nil {
		if threads, err := proc.NumThreadsWithContext(ctx); err == nil {
			pp.metrics.ThreadCount = int(threads)
		}

		if fds, err := proc.NumFDsWithContext(ctx); err == nil {
			pp.metrics.OpenFileDescriptors = int(fds)
		}
	}

	return nil
}

// GetMetrics returns current performance metrics
func (pp *PerformanceProfiler) GetMetrics() *PerformanceMetrics {
	pp.mu.RLock()
	defer pp.mu.RUnlock()

	// Create a copy to avoid race conditions
	metricsCopy := *pp.metrics
	return &metricsCopy
}

// OptimizeGC applies garbage collection optimizations
func (pp *PerformanceProfiler) OptimizeGC() {
	if pp.gcOptimized {
		return
	}

	// Set GC target percentage based on available memory
	vmStat, err := mem.VirtualMemory()
	if err == nil {
		// If we have lots of memory, be more relaxed with GC
		if vmStat.Available > 8*1024*1024*1024 { // 8GB
			debug.SetGCPercent(200) // Run GC when heap grows 200%
		} else if vmStat.Available > 4*1024*1024*1024 { // 4GB
			debug.SetGCPercent(150)
		} else {
			debug.SetGCPercent(100) // Default
		}
	}

	// Set memory limit if available (Go 1.19+)
	if vmStat != nil && vmStat.Available > 2*1024*1024*1024 {
		// Use up to 80% of available memory
		memLimit := int64(float64(vmStat.Available) * 0.8)
		debug.SetMemoryLimit(memLimit)
	}

	pp.gcOptimized = true

	pp.logger.WithFields(map[string]interface{}{
		"gc_percent":       debug.SetGCPercent(-1), // Get current value
		"available_memory": vmStat.Available,
		"component":        "performance_profiler",
		"type":             "gc_optimization",
	}).Info("GC optimization applied")
}

// ForceGC triggers garbage collection if memory usage is high
func (pp *PerformanceProfiler) ForceGC() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Force GC if heap is over threshold
	heapThreshold := uint64(512 * 1024 * 1024) // 512MB
	if memStats.HeapInuse > heapThreshold {
		runtime.GC()
		pp.logger.WithFields(map[string]interface{}{
			"heap_inuse_before": memStats.HeapInuse,
			"threshold":         heapThreshold,
			"component":         "performance_profiler",
			"type":              "forced_gc",
		}).Info("Forced garbage collection")
	}
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures:  maxFailures,
		resetTimeout: resetTimeout,
		currentState: CircuitClosed,
	}
}

// Execute runs a function with circuit breaker protection
func (cb *CircuitBreaker) Execute(fn func() error) error {
	cb.mu.RLock()
	state := cb.currentState
	lastFailTime := cb.lastFailTime
	cb.mu.RUnlock()

	// Check if circuit should transition from Open to HalfOpen
	if state == CircuitOpen && time.Since(lastFailTime) > cb.resetTimeout {
		cb.mu.Lock()
		if cb.currentState == CircuitOpen {
			cb.currentState = CircuitHalfOpen
		}
		cb.mu.Unlock()
		state = CircuitHalfOpen
	}

	// Reject request if circuit is open
	if state == CircuitOpen {
		return fmt.Errorf("circuit breaker is open")
	}

	// Execute the function
	err := fn()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.failures++
		cb.lastFailTime = time.Now()

		// Open circuit if failure threshold reached
		if cb.failures >= cb.maxFailures {
			cb.currentState = CircuitOpen
		}
		return err
	}

	// Success - reset failures and close circuit
	cb.failures = 0
	cb.currentState = CircuitClosed
	return nil
}

// NewRateLimiter creates a new token bucket rate limiter
func NewRateLimiter(maxTokens, refillRate float64) *RateLimiter {
	return &RateLimiter{
		tokens:     maxTokens,
		maxTokens:  maxTokens,
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

// Allow checks if a request is allowed by the rate limiter
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastRefill).Seconds()

	// Refill tokens
	rl.tokens = min(rl.maxTokens, rl.tokens+rl.refillRate*elapsed)
	rl.lastRefill = now

	if rl.tokens >= 1 {
		rl.tokens--
		return true
	}
	return false
}

// NewCacheWarmer creates a new cache warmer
func NewCacheWarmer(schedule time.Duration) *CacheWarmer {
	return &CacheWarmer{
		warmupFunctions: make([]WarmupFunction, 0),
		schedule:        schedule,
		stopChan:        make(chan struct{}),
	}
}

// AddWarmupFunction adds a function to be called during cache warmup
func (cw *CacheWarmer) AddWarmupFunction(fn WarmupFunction) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	cw.warmupFunctions = append(cw.warmupFunctions, fn)
}

// Start begins the cache warming process
func (cw *CacheWarmer) Start(ctx context.Context) {
	cw.mu.Lock()
	if cw.running {
		cw.mu.Unlock()
		return
	}
	cw.running = true
	cw.mu.Unlock()

	ticker := time.NewTicker(cw.schedule)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-cw.stopChan:
			return
		case <-ticker.C:
			cw.warmupCache(ctx)
		}
	}
}

// Stop stops the cache warming process
func (cw *CacheWarmer) Stop() {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	if cw.running {
		close(cw.stopChan)
		cw.running = false
	}
}

// warmupCache executes all warmup functions
func (cw *CacheWarmer) warmupCache(ctx context.Context) {
	cw.mu.RLock()
	functions := make([]WarmupFunction, len(cw.warmupFunctions))
	copy(functions, cw.warmupFunctions)
	cw.mu.RUnlock()

	for _, fn := range functions {
		if err := fn(ctx); err != nil {
			// Log error but continue with other warmup functions
			GetLogger().LogError(err, "Cache warmup function failed", LogContext{
				Component: "cache_warmer",
			}, map[string]interface{}{
				"operation": "cache_warmup",
			})
		}
	}
}

// Helper function for min
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// PerformanceReport generates a comprehensive performance report
func (pp *PerformanceProfiler) GenerateReport() map[string]interface{} {
	metrics := pp.GetMetrics()
	uptime := time.Since(pp.startTime)

	return map[string]interface{}{
		"uptime_seconds":  uptime.Seconds(),
		"metrics":         metrics,
		"recommendations": pp.generateRecommendations(metrics),
		"gc_optimized":    pp.gcOptimized,
		"generated_at":    time.Now(),
	}
}

// generateRecommendations provides performance optimization recommendations
func (pp *PerformanceProfiler) generateRecommendations(metrics *PerformanceMetrics) []string {
	var recommendations []string

	// Memory recommendations
	if metrics.HeapInuse > 1024*1024*1024 { // 1GB
		recommendations = append(recommendations, "High memory usage detected. Consider implementing memory pooling or reducing object allocations.")
	}

	// CPU recommendations
	if metrics.CPUUsagePercent > 80 {
		recommendations = append(recommendations, "High CPU usage detected. Consider implementing request throttling or scaling horizontally.")
	}

	// Goroutine recommendations
	if metrics.GoroutineCount > 10000 {
		recommendations = append(recommendations, "High goroutine count detected. Check for goroutine leaks or implement goroutine pooling.")
	}

	// GC recommendations
	if metrics.GCPauseAvg > 10*1000*1000 { // 10ms
		recommendations = append(recommendations, "High GC pause times detected. Consider tuning GC parameters or reducing allocations.")
	}

	return recommendations
}

// StartPerformanceMonitoring starts continuous performance monitoring
func (pp *PerformanceProfiler) StartPerformanceMonitoring(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := pp.UpdateMetrics(ctx); err != nil {
				pp.logger.LogError(err, "Failed to update performance metrics", LogContext{
					Component: "performance_profiler",
				}, map[string]interface{}{
					"operation": "update_metrics",
				})
			}

			// Check for optimization opportunities
			metrics := pp.GetMetrics()
			if metrics.HeapInuse > 512*1024*1024 { // 512MB threshold
				pp.ForceGC()
			}
		}
	}
}
