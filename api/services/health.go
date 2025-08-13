package services

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"gorm.io/gorm"
)

// HealthStatus represents the health status of a component
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// HealthCheck represents a single health check
type HealthCheck struct {
	Name        string                 `json:"name"`
	Status      HealthStatus           `json:"status"`
	Message     string                 `json:"message,omitempty"`
	Duration    time.Duration          `json:"duration"`
	Timestamp   time.Time              `json:"timestamp"`
	Details     map[string]interface{} `json:"details,omitempty"`
	Critical    bool                   `json:"critical"`
	LastSuccess *time.Time             `json:"last_success,omitempty"`
	LastFailure *time.Time             `json:"last_failure,omitempty"`
}

// OverallHealth represents the overall system health
type OverallHealth struct {
	Status      HealthStatus           `json:"status"`
	Version     string                 `json:"version"`
	Uptime      time.Duration          `json:"uptime"`
	Timestamp   time.Time              `json:"timestamp"`
	Checks      []HealthCheck          `json:"checks"`
	Summary     map[string]interface{} `json:"summary"`
	Environment string                 `json:"environment"`
}

// HealthChecker interface for health check implementations
type HealthChecker interface {
	Check(ctx context.Context) HealthCheck
	Name() string
	IsCritical() bool
}

// HealthMonitor manages and executes health checks
type HealthMonitor struct {
	checkers    []HealthChecker
	results     map[string]HealthCheck
	mutex       sync.RWMutex
	startTime   time.Time
	version     string
	environment string
}

// NewHealthMonitor creates a new health monitor
func NewHealthMonitor(version, environment string) *HealthMonitor {
	return &HealthMonitor{
		checkers:    make([]HealthChecker, 0),
		results:     make(map[string]HealthCheck),
		startTime:   time.Now(),
		version:     version,
		environment: environment,
	}
}

// RegisterChecker registers a health checker
func (hm *HealthMonitor) RegisterChecker(checker HealthChecker) {
	hm.mutex.Lock()
	defer hm.mutex.Unlock()
	hm.checkers = append(hm.checkers, checker)
}

// CheckHealth runs all health checks and returns overall health
func (hm *HealthMonitor) CheckHealth(ctx context.Context) OverallHealth {
	hm.mutex.Lock()
	defer hm.mutex.Unlock()

	checks := make([]HealthCheck, 0, len(hm.checkers))
	healthyCount := 0
	degradedCount := 0
	unhealthyCount := 0
	criticalFailures := 0

	// Run all health checks
	for _, checker := range hm.checkers {
		check := checker.Check(ctx)
		check.Critical = checker.IsCritical()
		checks = append(checks, check)
		hm.results[checker.Name()] = check

		// Count statuses
		switch check.Status {
		case HealthStatusHealthy:
			healthyCount++
		case HealthStatusDegraded:
			degradedCount++
		case HealthStatusUnhealthy:
			unhealthyCount++
			if check.Critical {
				criticalFailures++
			}
		}
	}

	// Determine overall status
	var overallStatus HealthStatus
	if criticalFailures > 0 {
		overallStatus = HealthStatusUnhealthy
	} else if unhealthyCount > 0 || degradedCount > 0 {
		overallStatus = HealthStatusDegraded
	} else {
		overallStatus = HealthStatusHealthy
	}

	// Create summary
	summary := map[string]interface{}{
		"total_checks":      len(checks),
		"healthy_checks":    healthyCount,
		"degraded_checks":   degradedCount,
		"unhealthy_checks":  unhealthyCount,
		"critical_failures": criticalFailures,
		"success_rate":      float64(healthyCount) / float64(len(checks)),
		"system_info":       hm.getSystemInfo(),
	}

	return OverallHealth{
		Status:      overallStatus,
		Version:     hm.version,
		Uptime:      time.Since(hm.startTime),
		Timestamp:   time.Now(),
		Checks:      checks,
		Summary:     summary,
		Environment: hm.environment,
	}
}

// GetCachedResults returns cached health check results
func (hm *HealthMonitor) GetCachedResults() map[string]HealthCheck {
	hm.mutex.RLock()
	defer hm.mutex.RUnlock()

	results := make(map[string]HealthCheck)
	for name, check := range hm.results {
		results[name] = check
	}
	return results
}

// getSystemInfo returns system information
func (hm *HealthMonitor) getSystemInfo() map[string]interface{} {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return map[string]interface{}{
		"go_version":   runtime.Version(),
		"goroutines":   runtime.NumGoroutine(),
		"memory_alloc": m.Alloc,
		"memory_sys":   m.Sys,
		"gc_cycles":    m.NumGC,
		"cpu_count":    runtime.NumCPU(),
	}
}

// Specific Health Checkers

// DatabaseHealthChecker checks database connectivity
type DatabaseHealthChecker struct {
	db   *gorm.DB
	name string
}

// NewDatabaseHealthChecker creates a database health checker
func NewDatabaseHealthChecker(db *gorm.DB) *DatabaseHealthChecker {
	return &DatabaseHealthChecker{
		db:   db,
		name: "database",
	}
}

func (dhc *DatabaseHealthChecker) Name() string {
	return dhc.name
}

func (dhc *DatabaseHealthChecker) IsCritical() bool {
	return true
}

func (dhc *DatabaseHealthChecker) Check(ctx context.Context) HealthCheck {
	start := time.Now()

	check := HealthCheck{
		Name:      dhc.name,
		Timestamp: start,
		Critical:  true,
	}

	// Test database connection
	sqlDB, err := dhc.db.DB()
	if err != nil {
		check.Status = HealthStatusUnhealthy
		check.Message = fmt.Sprintf("Failed to get database instance: %v", err)
		check.Duration = time.Since(start)
		return check
	}

	// Test ping
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := sqlDB.PingContext(ctxWithTimeout); err != nil {
		check.Status = HealthStatusUnhealthy
		check.Message = fmt.Sprintf("Database ping failed: %v", err)
		check.Duration = time.Since(start)
		return check
	}

	// Get database stats
	stats := sqlDB.Stats()
	check.Details = map[string]interface{}{
		"open_connections": stats.OpenConnections,
		"in_use":           stats.InUse,
		"idle":             stats.Idle,
		"max_open":         stats.MaxOpenConnections,
		"max_idle":         10, // Default value since MaxIdleConnections might not be available
	}

	check.Status = HealthStatusHealthy
	check.Message = "Database connection is healthy"
	check.Duration = time.Since(start)
	return check
}

// RedisHealthChecker checks Redis connectivity
type RedisHealthChecker struct {
	cache *MultiLevelCache
	name  string
}

// NewRedisHealthChecker creates a Redis health checker
func NewRedisHealthChecker(cache *MultiLevelCache) *RedisHealthChecker {
	return &RedisHealthChecker{
		cache: cache,
		name:  "redis",
	}
}

func (rhc *RedisHealthChecker) Name() string {
	return rhc.name
}

func (rhc *RedisHealthChecker) IsCritical() bool {
	return false // Redis is not critical for basic functionality
}

func (rhc *RedisHealthChecker) Check(ctx context.Context) HealthCheck {
	start := time.Now()

	check := HealthCheck{
		Name:      rhc.name,
		Timestamp: start,
		Critical:  false,
	}

	if rhc.cache == nil {
		check.Status = HealthStatusUnhealthy
		check.Message = "Redis cache not initialized"
		check.Duration = time.Since(start)
		return check
	}

	// Test Redis connectivity by setting and getting a test value
	testKey := fmt.Sprintf("health_check_%d", time.Now().Unix())
	testValue := "ok"

	if err := rhc.cache.Set(testKey, testValue, 10*time.Second); err != nil {
		check.Status = HealthStatusUnhealthy
		check.Message = fmt.Sprintf("Redis write failed: %v", err)
		check.Duration = time.Since(start)
		return check
	}

	if value, found := rhc.cache.Get(testKey); !found || value != testValue {
		check.Status = HealthStatusUnhealthy
		check.Message = "Redis read failed"
		check.Duration = time.Since(start)
		return check
	}

	// Clean up test key
	rhc.cache.Delete(testKey)

	// Get cache stats
	stats := rhc.cache.GetStats()
	check.Details = map[string]interface{}{
		"l1_hit_rate": stats.L1Stats.HitRate,
		"l2_hit_rate": stats.L2Stats.HitRate,
		"l1_size":     stats.L1Stats.Size,
		"l2_size":     stats.L2Stats.Size,
	}

	check.Status = HealthStatusHealthy
	check.Message = "Redis connection is healthy"
	check.Duration = time.Since(start)
	return check
}

// AIServiceHealthChecker checks AI service connectivity
type AIServiceHealthChecker struct {
	client *AIClient
	name   string
}

// NewAIServiceHealthChecker creates an AI service health checker
func NewAIServiceHealthChecker(client *AIClient) *AIServiceHealthChecker {
	return &AIServiceHealthChecker{
		client: client,
		name:   "ai_service",
	}
}

func (asc *AIServiceHealthChecker) Name() string {
	return asc.name
}

func (asc *AIServiceHealthChecker) IsCritical() bool {
	return true
}

func (asc *AIServiceHealthChecker) Check(ctx context.Context) HealthCheck {
	start := time.Now()

	check := HealthCheck{
		Name:      asc.name,
		Timestamp: start,
		Critical:  true,
	}

	if asc.client == nil {
		check.Status = HealthStatusUnhealthy
		check.Message = "AI service client not initialized"
		check.Duration = time.Since(start)
		return check
	}

	// Check AI service health
	healthy, err := asc.client.IsHealthy()
	if err != nil {
		check.Status = HealthStatusUnhealthy
		check.Message = fmt.Sprintf("AI service health check failed: %v", err)
		check.Duration = time.Since(start)
		return check
	}

	if !healthy {
		check.Status = HealthStatusUnhealthy
		check.Message = "AI service reported unhealthy status"
		check.Duration = time.Since(start)
		return check
	}

	check.Status = HealthStatusHealthy
	check.Message = "AI service is healthy"
	check.Duration = time.Since(start)
	return check
}

// QueueHealthChecker checks queue system health
type QueueHealthChecker struct {
	queueManager *QueueManager
	name         string
}

// NewQueueHealthChecker creates a queue health checker
func NewQueueHealthChecker(queueManager *QueueManager) *QueueHealthChecker {
	return &QueueHealthChecker{
		queueManager: queueManager,
		name:         "queue",
	}
}

func (qhc *QueueHealthChecker) Name() string {
	return qhc.name
}

func (qhc *QueueHealthChecker) IsCritical() bool {
	return false
}

func (qhc *QueueHealthChecker) Check(ctx context.Context) HealthCheck {
	start := time.Now()

	check := HealthCheck{
		Name:      qhc.name,
		Timestamp: start,
		Critical:  false,
	}

	if qhc.queueManager == nil {
		check.Status = HealthStatusUnhealthy
		check.Message = "Queue manager not initialized"
		check.Duration = time.Since(start)
		return check
	}

	// Get queue stats
	stats, err := qhc.queueManager.GetStats()
	if err != nil {
		check.Status = HealthStatusUnhealthy
		check.Message = fmt.Sprintf("Failed to get queue stats: %v", err)
		check.Duration = time.Since(start)
		return check
	}

	check.Details = map[string]interface{}{
		"total_jobs":      stats.TotalJobs,
		"pending_jobs":    stats.PendingJobs,
		"processing_jobs": stats.ProcessingJobs,
		"completed_jobs":  stats.CompletedJobs,
		"failed_jobs":     stats.FailedJobs,
		"active_workers":  stats.ActiveWorkers,
		"throughput":      stats.Throughput,
	}

	// Determine status based on metrics
	if stats.ActiveWorkers == 0 && stats.PendingJobs > 0 {
		check.Status = HealthStatusDegraded
		check.Message = "No active workers available for pending jobs"
	} else if stats.FailedJobs*10 > stats.CompletedJobs { // More than 10% failure rate
		check.Status = HealthStatusDegraded
		check.Message = "High job failure rate detected"
	} else {
		check.Status = HealthStatusHealthy
		check.Message = "Queue system is healthy"
	}

	check.Duration = time.Since(start)
	return check
}

// SystemResourcesHealthChecker checks system resources
type SystemResourcesHealthChecker struct {
	name string
}

// NewSystemResourcesHealthChecker creates a system resources health checker
func NewSystemResourcesHealthChecker() *SystemResourcesHealthChecker {
	return &SystemResourcesHealthChecker{
		name: "system_resources",
	}
}

func (src *SystemResourcesHealthChecker) Name() string {
	return src.name
}

func (src *SystemResourcesHealthChecker) IsCritical() bool {
	return false
}

func (src *SystemResourcesHealthChecker) Check(ctx context.Context) HealthCheck {
	start := time.Now()

	check := HealthCheck{
		Name:      src.name,
		Timestamp: start,
		Critical:  false,
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Convert bytes to MB for readability
	allocMB := float64(m.Alloc) / 1024 / 1024
	sysMB := float64(m.Sys) / 1024 / 1024

	check.Details = map[string]interface{}{
		"memory_alloc_mb": allocMB,
		"memory_sys_mb":   sysMB,
		"goroutines":      runtime.NumGoroutine(),
		"gc_cycles":       m.NumGC,
		"cpu_count":       runtime.NumCPU(),
	}

	// Simple thresholds for health determination
	var status HealthStatus
	var message string

	if allocMB > 1024 { // More than 1GB allocated
		status = HealthStatusDegraded
		message = "High memory usage detected"
	} else if runtime.NumGoroutine() > 10000 { // More than 10k goroutines
		status = HealthStatusDegraded
		message = "High goroutine count detected"
	} else {
		status = HealthStatusHealthy
		message = "System resources are healthy"
	}

	check.Status = status
	check.Message = message
	check.Duration = time.Since(start)
	return check
}

// Global health monitor instance
var globalHealthMonitor *HealthMonitor

// InitializeHealthMonitor initializes the global health monitor
func InitializeHealthMonitor(version, environment string) {
	globalHealthMonitor = NewHealthMonitor(version, environment)
}

// GetHealthMonitor returns the global health monitor
func GetHealthMonitor() *HealthMonitor {
	return globalHealthMonitor
}

// RegisterHealthCheckers registers all standard health checkers
func RegisterHealthCheckers(
	db *gorm.DB,
	cache *MultiLevelCache,
	aiClient *AIClient,
	queueManager *QueueManager,
) {
	if globalHealthMonitor == nil {
		return
	}

	globalHealthMonitor.RegisterChecker(NewDatabaseHealthChecker(db))

	if cache != nil {
		globalHealthMonitor.RegisterChecker(NewRedisHealthChecker(cache))
	}

	if aiClient != nil {
		globalHealthMonitor.RegisterChecker(NewAIServiceHealthChecker(aiClient))
	}

	if queueManager != nil {
		globalHealthMonitor.RegisterChecker(NewQueueHealthChecker(queueManager))
	}

	globalHealthMonitor.RegisterChecker(NewSystemResourcesHealthChecker())
}
