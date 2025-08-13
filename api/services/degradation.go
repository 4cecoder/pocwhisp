package services

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// DegradationLevel represents the level of service degradation
type DegradationLevel int

const (
	DegradationNone DegradationLevel = iota
	DegradationMinor
	DegradationMajor
	DegradationSevere
)

func (d DegradationLevel) String() string {
	switch d {
	case DegradationNone:
		return "none"
	case DegradationMinor:
		return "minor"
	case DegradationMajor:
		return "major"
	case DegradationSevere:
		return "severe"
	default:
		return "unknown"
	}
}

// ServiceHealth represents the health status of a service
type ServiceHealth struct {
	Name                string           `json:"name"`
	IsHealthy           bool             `json:"is_healthy"`
	DegradationLevel    DegradationLevel `json:"degradation_level"`
	ErrorRate           float64          `json:"error_rate"`
	ResponseTime        time.Duration    `json:"response_time"`
	LastCheck           time.Time        `json:"last_check"`
	ConsecutiveFailures int              `json:"consecutive_failures"`
	Message             string           `json:"message,omitempty"`
}

// FallbackStrategy defines how to handle service degradation
type FallbackStrategy interface {
	Execute(ctx context.Context, service string, level DegradationLevel) (interface{}, error)
	CanHandle(service string, level DegradationLevel) bool
	Priority() int
}

// DegradationManager manages service degradation and fallback strategies
type DegradationManager struct {
	services   map[string]*ServiceHealth
	strategies map[string][]FallbackStrategy
	callbacks  []DegradationCallback
	mutex      sync.RWMutex
}

// DegradationCallback is called when service degradation changes
type DegradationCallback func(service string, oldLevel, newLevel DegradationLevel, health ServiceHealth)

// NewDegradationManager creates a new degradation manager
func NewDegradationManager() *DegradationManager {
	return &DegradationManager{
		services:   make(map[string]*ServiceHealth),
		strategies: make(map[string][]FallbackStrategy),
		callbacks:  make([]DegradationCallback, 0),
	}
}

// RegisterService registers a service for degradation monitoring
func (dm *DegradationManager) RegisterService(name string) {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	if _, exists := dm.services[name]; !exists {
		dm.services[name] = &ServiceHealth{
			Name:             name,
			IsHealthy:        true,
			DegradationLevel: DegradationNone,
			LastCheck:        time.Now(),
		}
	}
}

// RegisterFallbackStrategy registers a fallback strategy for a service
func (dm *DegradationManager) RegisterFallbackStrategy(service string, strategy FallbackStrategy) {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	if _, exists := dm.strategies[service]; !exists {
		dm.strategies[service] = make([]FallbackStrategy, 0)
	}

	dm.strategies[service] = append(dm.strategies[service], strategy)

	// Sort strategies by priority (higher priority first)
	strategies := dm.strategies[service]
	for i := 0; i < len(strategies)-1; i++ {
		for j := i + 1; j < len(strategies); j++ {
			if strategies[i].Priority() < strategies[j].Priority() {
				strategies[i], strategies[j] = strategies[j], strategies[i]
			}
		}
	}
}

// RegisterCallback registers a callback for degradation changes
func (dm *DegradationManager) RegisterCallback(callback DegradationCallback) {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()
	dm.callbacks = append(dm.callbacks, callback)
}

// UpdateServiceHealth updates the health status of a service
func (dm *DegradationManager) UpdateServiceHealth(
	name string,
	isHealthy bool,
	errorRate float64,
	responseTime time.Duration,
	message string,
) {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	service, exists := dm.services[name]
	if !exists {
		service = &ServiceHealth{Name: name}
		dm.services[name] = service
	}

	oldLevel := service.DegradationLevel

	// Update health metrics
	service.IsHealthy = isHealthy
	service.ErrorRate = errorRate
	service.ResponseTime = responseTime
	service.LastCheck = time.Now()
	service.Message = message

	// Update failure count
	if !isHealthy {
		service.ConsecutiveFailures++
	} else {
		service.ConsecutiveFailures = 0
	}

	// Calculate degradation level
	newLevel := dm.calculateDegradationLevel(service)
	service.DegradationLevel = newLevel

	// Trigger callbacks if degradation level changed
	if oldLevel != newLevel {
		dm.triggerCallbacks(name, oldLevel, newLevel, *service)
	}
}

// calculateDegradationLevel determines the degradation level based on metrics
func (dm *DegradationManager) calculateDegradationLevel(service *ServiceHealth) DegradationLevel {
	if !service.IsHealthy {
		// Severe degradation if service has been down for multiple consecutive checks
		if service.ConsecutiveFailures >= 5 {
			return DegradationSevere
		}
		// Major degradation if service is down
		return DegradationMajor
	}

	// Check error rate and response time for minor degradation
	if service.ErrorRate > 0.1 || service.ResponseTime > 10*time.Second {
		return DegradationMinor
	}

	return DegradationNone
}

// triggerCallbacks calls all registered degradation callbacks
func (dm *DegradationManager) triggerCallbacks(service string, oldLevel, newLevel DegradationLevel, health ServiceHealth) {
	for _, callback := range dm.callbacks {
		go func(cb DegradationCallback) {
			defer func() {
				if r := recover(); r != nil {
					// Logger would go here in production
					fmt.Printf("Degradation callback panic: %v\n", r)
				}
			}()
			cb(service, oldLevel, newLevel, health)
		}(callback)
	}
}

// GetServiceHealth returns the current health status of a service
func (dm *DegradationManager) GetServiceHealth(name string) (*ServiceHealth, bool) {
	dm.mutex.RLock()
	defer dm.mutex.RUnlock()

	service, exists := dm.services[name]
	if !exists {
		return nil, false
	}

	// Return a copy to avoid race conditions
	healthCopy := *service
	return &healthCopy, true
}

// GetAllServiceHealth returns the health status of all services
func (dm *DegradationManager) GetAllServiceHealth() map[string]ServiceHealth {
	dm.mutex.RLock()
	defer dm.mutex.RUnlock()

	result := make(map[string]ServiceHealth)
	for name, service := range dm.services {
		result[name] = *service
	}
	return result
}

// ExecuteWithFallback executes an operation with fallback strategies
func (dm *DegradationManager) ExecuteWithFallback(
	ctx context.Context,
	service string,
	primaryOperation func() (interface{}, error),
) (interface{}, error) {
	// Try primary operation first
	result, err := primaryOperation()
	if err == nil {
		// Update service health on success
		dm.UpdateServiceHealth(service, true, 0, 0, "")
		return result, nil
	}

	// Update service health on failure
	dm.UpdateServiceHealth(service, false, 1.0, 0, err.Error())

	// Get current service health
	health, exists := dm.GetServiceHealth(service)
	if !exists {
		return nil, fmt.Errorf("service %s not registered", service)
	}

	// Try fallback strategies
	dm.mutex.RLock()
	strategies, hasStrategies := dm.strategies[service]
	dm.mutex.RUnlock()

	if !hasStrategies || len(strategies) == 0 {
		return nil, fmt.Errorf("no fallback strategies available for service %s: %w", service, err)
	}

	// Try each strategy in priority order
	for _, strategy := range strategies {
		if strategy.CanHandle(service, health.DegradationLevel) {
			result, fallbackErr := strategy.Execute(ctx, service, health.DegradationLevel)
			if fallbackErr == nil {
				logger := GetLogger()
				if logger != nil {
					logger.LogError(err, "fallback_executed", LogContext{
						Component: "degradation_manager",
						Operation: "fallback",
					}, map[string]interface{}{
						"service":           service,
						"degradation_level": health.DegradationLevel.String(),
						"strategy_priority": strategy.Priority(),
					})
				}
				return result, nil
			}
		}
	}

	return nil, fmt.Errorf("all fallback strategies failed for service %s: %w", service, err)
}

// Predefined Fallback Strategies

// CacheFallbackStrategy uses cached data as a fallback
type CacheFallbackStrategy struct {
	cache    *MultiLevelCache
	priority int
	ttl      time.Duration
}

// NewCacheFallbackStrategy creates a cache-based fallback strategy
func NewCacheFallbackStrategy(cache *MultiLevelCache, priority int, ttl time.Duration) *CacheFallbackStrategy {
	return &CacheFallbackStrategy{
		cache:    cache,
		priority: priority,
		ttl:      ttl,
	}
}

func (cfs *CacheFallbackStrategy) Execute(ctx context.Context, service string, level DegradationLevel) (interface{}, error) {
	if cfs.cache == nil {
		return nil, fmt.Errorf("cache not available")
	}

	cacheKey := fmt.Sprintf("fallback:%s:last_success", service)
	if result, found := cfs.cache.Get(cacheKey); found {
		return result, nil
	}

	return nil, fmt.Errorf("no cached data available for fallback")
}

func (cfs *CacheFallbackStrategy) CanHandle(service string, level DegradationLevel) bool {
	return level <= DegradationMajor && cfs.cache != nil
}

func (cfs *CacheFallbackStrategy) Priority() int {
	return cfs.priority
}

// DefaultValueFallbackStrategy returns default values as a fallback
type DefaultValueFallbackStrategy struct {
	defaultValues map[string]interface{}
	priority      int
}

// NewDefaultValueFallbackStrategy creates a default value fallback strategy
func NewDefaultValueFallbackStrategy(defaultValues map[string]interface{}, priority int) *DefaultValueFallbackStrategy {
	return &DefaultValueFallbackStrategy{
		defaultValues: defaultValues,
		priority:      priority,
	}
}

func (dvfs *DefaultValueFallbackStrategy) Execute(ctx context.Context, service string, level DegradationLevel) (interface{}, error) {
	if value, exists := dvfs.defaultValues[service]; exists {
		return value, nil
	}
	return nil, fmt.Errorf("no default value configured for service %s", service)
}

func (dvfs *DefaultValueFallbackStrategy) CanHandle(service string, level DegradationLevel) bool {
	_, exists := dvfs.defaultValues[service]
	return exists
}

func (dvfs *DefaultValueFallbackStrategy) Priority() int {
	return dvfs.priority
}

// QueueFallbackStrategy queues requests for later processing
type QueueFallbackStrategy struct {
	queueManager *QueueManager
	priority     int
}

// NewQueueFallbackStrategy creates a queue-based fallback strategy
func NewQueueFallbackStrategy(queueManager *QueueManager, priority int) *QueueFallbackStrategy {
	return &QueueFallbackStrategy{
		queueManager: queueManager,
		priority:     priority,
	}
}

func (qfs *QueueFallbackStrategy) Execute(ctx context.Context, service string, level DegradationLevel) (interface{}, error) {
	if qfs.queueManager == nil {
		return nil, fmt.Errorf("queue manager not available")
	}

	// Create a job for later processing
	payload := map[string]interface{}{
		"service":           service,
		"degradation_level": level.String(),
		"retry_count":       0,
		"queued_at":         time.Now(),
	}

	job, err := qfs.queueManager.EnqueueJob(JobTypeTranscription, PriorityLow, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to queue job: %w", err)
	}

	return map[string]interface{}{
		"status":  "queued",
		"job_id":  job.ID,
		"message": "Request queued for processing when service recovers",
	}, nil
}

func (qfs *QueueFallbackStrategy) CanHandle(service string, level DegradationLevel) bool {
	return level >= DegradationMajor && qfs.queueManager != nil
}

func (qfs *QueueFallbackStrategy) Priority() int {
	return qfs.priority
}

// Service-specific degradation handlers

// AIServiceDegradationHandler handles AI service degradation
type AIServiceDegradationHandler struct {
	cache        *MultiLevelCache
	queueManager *QueueManager
}

// NewAIServiceDegradationHandler creates an AI service degradation handler
func NewAIServiceDegradationHandler(cache *MultiLevelCache, queueManager *QueueManager) *AIServiceDegradationHandler {
	return &AIServiceDegradationHandler{
		cache:        cache,
		queueManager: queueManager,
	}
}

// HandleTranscriptionDegradation handles transcription service degradation
func (asdh *AIServiceDegradationHandler) HandleTranscriptionDegradation(
	ctx context.Context,
	sessionID string,
	level DegradationLevel,
) (interface{}, error) {
	switch level {
	case DegradationMinor:
		// Return cached results if available
		if asdh.cache != nil {
			if result, found := asdh.cache.GetTranscript(sessionID); found {
				return result, nil
			}
		}
		return nil, fmt.Errorf("transcription service experiencing minor issues, no cached data available")

	case DegradationMajor:
		// Queue for later processing
		if asdh.queueManager != nil {
			payload := map[string]interface{}{
				"session_id": sessionID,
				"operation":  "transcription",
				"priority":   "high",
			}
			job, err := asdh.queueManager.EnqueueJob(JobTypeTranscription, PriorityHigh, payload)
			if err == nil {
				return map[string]interface{}{
					"status":  "queued",
					"job_id":  job.ID,
					"message": "Transcription queued for processing when service recovers",
				}, nil
			}
		}
		return nil, fmt.Errorf("transcription service unavailable, queuing failed")

	case DegradationSevere:
		// Return error with helpful message
		return nil, fmt.Errorf("transcription service is severely degraded, please try again later")

	default:
		return nil, fmt.Errorf("unknown degradation level")
	}
}

// DatabaseDegradationHandler handles database degradation
type DatabaseDegradationHandler struct {
	cache *MultiLevelCache
}

// NewDatabaseDegradationHandler creates a database degradation handler
func NewDatabaseDegradationHandler(cache *MultiLevelCache) *DatabaseDegradationHandler {
	return &DatabaseDegradationHandler{
		cache: cache,
	}
}

// HandleReadDegradation handles database read degradation
func (ddh *DatabaseDegradationHandler) HandleReadDegradation(
	ctx context.Context,
	key string,
	level DegradationLevel,
) (interface{}, error) {
	switch level {
	case DegradationMinor, DegradationMajor:
		// Try cache for read operations
		if ddh.cache != nil {
			if result, found := ddh.cache.Get(key); found {
				return result, nil
			}
		}
		return nil, fmt.Errorf("database read failed and no cached data available")

	case DegradationSevere:
		return nil, fmt.Errorf("database is severely degraded")

	default:
		return nil, fmt.Errorf("unknown degradation level")
	}
}

// Global degradation manager
var globalDegradationManager *DegradationManager

// InitializeDegradationManager initializes the global degradation manager
func InitializeDegradationManager() {
	globalDegradationManager = NewDegradationManager()

	// Register common services
	globalDegradationManager.RegisterService("database")
	globalDegradationManager.RegisterService("ai_service")
	globalDegradationManager.RegisterService("redis")
	globalDegradationManager.RegisterService("queue")

	// Register default callback for logging
	globalDegradationManager.RegisterCallback(func(service string, oldLevel, newLevel DegradationLevel, health ServiceHealth) {
		// Logger would go here in production
		// logger := utils.GetLogger()
		// if logger != nil {
		//   logger.LogError(fmt.Errorf("service degradation"), "degradation", utils.LogContext{}, nil)
		// }
	})
}

// GetDegradationManager returns the global degradation manager
func GetDegradationManager() *DegradationManager {
	if globalDegradationManager == nil {
		InitializeDegradationManager()
	}
	return globalDegradationManager
}

// SetupFallbackStrategies sets up default fallback strategies
func SetupFallbackStrategies(cache *MultiLevelCache, queueManager *QueueManager) {
	dm := GetDegradationManager()

	// Cache fallback for all services
	if cache != nil {
		cacheFallback := NewCacheFallbackStrategy(cache, 100, 5*time.Minute)
		dm.RegisterFallbackStrategy("ai_service", cacheFallback)
		dm.RegisterFallbackStrategy("database", cacheFallback)
	}

	// Queue fallback for AI service
	if queueManager != nil {
		queueFallback := NewQueueFallbackStrategy(queueManager, 50)
		dm.RegisterFallbackStrategy("ai_service", queueFallback)
	}

	// Default values for non-critical operations
	defaultValues := map[string]interface{}{
		"ai_service": map[string]interface{}{
			"status":  "degraded",
			"message": "AI service temporarily unavailable",
		},
		"redis": map[string]interface{}{
			"status":  "degraded",
			"message": "Cache temporarily unavailable",
		},
	}
	defaultFallback := NewDefaultValueFallbackStrategy(defaultValues, 10)
	dm.RegisterFallbackStrategy("ai_service", defaultFallback)
	dm.RegisterFallbackStrategy("redis", defaultFallback)
}
