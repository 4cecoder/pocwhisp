package services

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// CircuitBreakerState represents the current state of a circuit breaker
type CircuitBreakerState int

const (
	StateClosed CircuitBreakerState = iota
	StateHalfOpen
	StateOpen
)

func (s CircuitBreakerState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateHalfOpen:
		return "half-open"
	case StateOpen:
		return "open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig holds configuration for a circuit breaker
type CircuitBreakerConfig struct {
	Name               string
	MaxRequests        uint32
	Interval           time.Duration
	Timeout            time.Duration
	ReadyToTrip        func(counts Counts) bool
	OnStateChange      func(name string, from CircuitBreakerState, to CircuitBreakerState)
	IsSuccessful       func(err error) bool
	MaxConcurrentCalls int
	ShouldTrip         func(counts Counts) bool
}

// Counts holds the statistics for a circuit breaker
type Counts struct {
	Requests             uint32
	TotalSuccesses       uint32
	TotalFailures        uint32
	ConsecutiveSuccesses uint32
	ConsecutiveFailures  uint32
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	name          string
	maxRequests   uint32
	interval      time.Duration
	timeout       time.Duration
	readyToTrip   func(counts Counts) bool
	isSuccessful  func(err error) bool
	onStateChange func(name string, from CircuitBreakerState, to CircuitBreakerState)

	mutex      sync.Mutex
	state      CircuitBreakerState
	generation uint64
	counts     Counts
	expiry     time.Time

	// Concurrency control
	maxConcurrentCalls int
	activeCalls        int32
	callMutex          sync.Mutex
}

// CircuitBreakerError represents an error from the circuit breaker
type CircuitBreakerError struct {
	State   CircuitBreakerState
	Message string
}

func (e CircuitBreakerError) Error() string {
	return e.Message
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	cb := &CircuitBreaker{
		name:        config.Name,
		maxRequests: config.MaxRequests,
		interval:    config.Interval,
		timeout:     config.Timeout,
	}

	if config.ReadyToTrip == nil {
		cb.readyToTrip = defaultReadyToTrip
	} else {
		cb.readyToTrip = config.ReadyToTrip
	}

	if config.IsSuccessful == nil {
		cb.isSuccessful = defaultIsSuccessful
	} else {
		cb.isSuccessful = config.IsSuccessful
	}

	cb.onStateChange = config.OnStateChange

	if config.MaxConcurrentCalls > 0 {
		cb.maxConcurrentCalls = config.MaxConcurrentCalls
	} else {
		cb.maxConcurrentCalls = 1000 // Default max concurrent calls
	}

	cb.toNewGeneration(time.Now())

	return cb
}

// Execute runs the given request if the circuit breaker accepts it
func (cb *CircuitBreaker) Execute(fn func() (interface{}, error)) (interface{}, error) {
	return cb.ExecuteWithContext(context.Background(), func(ctx context.Context) (interface{}, error) {
		return fn()
	})
}

// ExecuteWithContext runs the given request with context if the circuit breaker accepts it
func (cb *CircuitBreaker) ExecuteWithContext(ctx context.Context, fn func(context.Context) (interface{}, error)) (interface{}, error) {
	generation, err := cb.beforeRequest()
	if err != nil {
		return nil, err
	}

	defer func() {
		cb.afterRequest(generation, err)
	}()

	result, err := fn(ctx)
	return result, err
}

// beforeRequest is called before a request
func (cb *CircuitBreaker) beforeRequest() (uint64, error) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	now := time.Now()
	state, generation := cb.currentState(now)

	if state == StateOpen {
		return generation, CircuitBreakerError{
			State:   StateOpen,
			Message: fmt.Sprintf("circuit breaker %s is open", cb.name),
		}
	}

	if state == StateHalfOpen && cb.counts.Requests >= cb.maxRequests {
		return generation, CircuitBreakerError{
			State:   StateHalfOpen,
			Message: fmt.Sprintf("circuit breaker %s is half-open and max requests exceeded", cb.name),
		}
	}

	// Check concurrency limit
	cb.callMutex.Lock()
	if cb.activeCalls >= int32(cb.maxConcurrentCalls) {
		cb.callMutex.Unlock()
		return generation, CircuitBreakerError{
			State:   state,
			Message: fmt.Sprintf("circuit breaker %s max concurrent calls exceeded", cb.name),
		}
	}
	cb.activeCalls++
	cb.callMutex.Unlock()

	cb.counts.onRequest()
	return generation, nil
}

// afterRequest is called after a request
func (cb *CircuitBreaker) afterRequest(before uint64, err error) {
	cb.callMutex.Lock()
	cb.activeCalls--
	cb.callMutex.Unlock()

	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	now := time.Now()
	state, generation := cb.currentState(now)
	if generation != before {
		return
	}

	if cb.isSuccessful(err) {
		cb.onSuccess(state, now)
	} else {
		cb.onFailure(state, now)
	}
}

// currentState returns the current state and generation
func (cb *CircuitBreaker) currentState(now time.Time) (CircuitBreakerState, uint64) {
	switch cb.state {
	case StateClosed:
		if !cb.expiry.IsZero() && cb.expiry.Before(now) {
			cb.toNewGeneration(now)
		}
	case StateOpen:
		if cb.expiry.Before(now) {
			cb.setState(StateHalfOpen, now)
		}
	}
	return cb.state, cb.generation
}

// onSuccess handles successful requests
func (cb *CircuitBreaker) onSuccess(state CircuitBreakerState, now time.Time) {
	switch state {
	case StateClosed:
		cb.counts.onSuccess()
	case StateHalfOpen:
		cb.counts.onSuccess()
		if cb.counts.ConsecutiveSuccesses >= cb.maxRequests {
			cb.setState(StateClosed, now)
		}
	}
}

// onFailure handles failed requests
func (cb *CircuitBreaker) onFailure(state CircuitBreakerState, now time.Time) {
	switch state {
	case StateClosed:
		cb.counts.onFailure()
		if cb.readyToTrip(cb.counts) {
			cb.setState(StateOpen, now)
		}
	case StateHalfOpen:
		cb.setState(StateOpen, now)
	}
}

// setState changes the state of the circuit breaker
func (cb *CircuitBreaker) setState(state CircuitBreakerState, now time.Time) {
	if cb.state == state {
		return
	}

	prev := cb.state
	cb.state = state

	cb.toNewGeneration(now)

	if cb.onStateChange != nil {
		cb.onStateChange(cb.name, prev, state)
	}
}

// toNewGeneration creates a new generation
func (cb *CircuitBreaker) toNewGeneration(now time.Time) {
	cb.generation++
	cb.counts.clear()

	var zero time.Time
	switch cb.state {
	case StateClosed:
		if cb.interval == 0 {
			cb.expiry = zero
		} else {
			cb.expiry = now.Add(cb.interval)
		}
	case StateOpen:
		cb.expiry = now.Add(cb.timeout)
	default: // StateHalfOpen
		cb.expiry = zero
	}
}

// State returns the current state of the circuit breaker
func (cb *CircuitBreaker) State() CircuitBreakerState {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	now := time.Now()
	state, _ := cb.currentState(now)
	return state
}

// Counts returns a copy of the current counts
func (cb *CircuitBreaker) Counts() Counts {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	return cb.counts
}

// Name returns the name of the circuit breaker
func (cb *CircuitBreaker) Name() string {
	return cb.name
}

// Reset resets the circuit breaker to its initial state
func (cb *CircuitBreaker) Reset() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.setState(StateClosed, time.Now())
}

// IsOpen returns true if the circuit breaker is open
func (cb *CircuitBreaker) IsOpen() bool {
	return cb.State() == StateOpen
}

// IsClosed returns true if the circuit breaker is closed
func (cb *CircuitBreaker) IsClosed() bool {
	return cb.State() == StateClosed
}

// IsHalfOpen returns true if the circuit breaker is half-open
func (cb *CircuitBreaker) IsHalfOpen() bool {
	return cb.State() == StateHalfOpen
}

// Methods for Counts

// onRequest increments the request count
func (c *Counts) onRequest() {
	c.Requests++
}

// onSuccess increments success counts
func (c *Counts) onSuccess() {
	c.TotalSuccesses++
	c.ConsecutiveSuccesses++
	c.ConsecutiveFailures = 0
}

// onFailure increments failure counts
func (c *Counts) onFailure() {
	c.TotalFailures++
	c.ConsecutiveFailures++
	c.ConsecutiveSuccesses = 0
}

// clear resets all counts
func (c *Counts) clear() {
	c.Requests = 0
	c.TotalSuccesses = 0
	c.TotalFailures = 0
	c.ConsecutiveSuccesses = 0
	c.ConsecutiveFailures = 0
}

// SuccessRate returns the success rate as a percentage
func (c *Counts) SuccessRate() float64 {
	if c.Requests == 0 {
		return 0
	}
	return float64(c.TotalSuccesses) / float64(c.Requests) * 100
}

// FailureRate returns the failure rate as a percentage
func (c *Counts) FailureRate() float64 {
	if c.Requests == 0 {
		return 0
	}
	return float64(c.TotalFailures) / float64(c.Requests) * 100
}

// Default functions

// defaultReadyToTrip is the default function to determine if the circuit should trip
func defaultReadyToTrip(counts Counts) bool {
	return counts.Requests >= 5 && counts.ConsecutiveFailures >= 5
}

// defaultIsSuccessful is the default function to determine if a request was successful
func defaultIsSuccessful(err error) bool {
	return err == nil
}

// CircuitBreakerManager manages multiple circuit breakers
type CircuitBreakerManager struct {
	breakers map[string]*CircuitBreaker
	mutex    sync.RWMutex
}

// NewCircuitBreakerManager creates a new circuit breaker manager
func NewCircuitBreakerManager() *CircuitBreakerManager {
	return &CircuitBreakerManager{
		breakers: make(map[string]*CircuitBreaker),
	}
}

// GetOrCreate gets an existing circuit breaker or creates a new one
func (cbm *CircuitBreakerManager) GetOrCreate(name string, config CircuitBreakerConfig) *CircuitBreaker {
	cbm.mutex.Lock()
	defer cbm.mutex.Unlock()

	if cb, exists := cbm.breakers[name]; exists {
		return cb
	}

	config.Name = name
	cb := NewCircuitBreaker(config)
	cbm.breakers[name] = cb
	return cb
}

// Get gets an existing circuit breaker
func (cbm *CircuitBreakerManager) Get(name string) (*CircuitBreaker, bool) {
	cbm.mutex.RLock()
	defer cbm.mutex.RUnlock()

	cb, exists := cbm.breakers[name]
	return cb, exists
}

// List returns all circuit breakers
func (cbm *CircuitBreakerManager) List() map[string]*CircuitBreaker {
	cbm.mutex.RLock()
	defer cbm.mutex.RUnlock()

	result := make(map[string]*CircuitBreaker)
	for name, cb := range cbm.breakers {
		result[name] = cb
	}
	return result
}

// Reset resets a specific circuit breaker
func (cbm *CircuitBreakerManager) Reset(name string) error {
	cbm.mutex.RLock()
	cb, exists := cbm.breakers[name]
	cbm.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("circuit breaker %s not found", name)
	}

	cb.Reset()
	return nil
}

// ResetAll resets all circuit breakers
func (cbm *CircuitBreakerManager) ResetAll() {
	cbm.mutex.RLock()
	defer cbm.mutex.RUnlock()

	for _, cb := range cbm.breakers {
		cb.Reset()
	}
}

// GetStats returns statistics for all circuit breakers
func (cbm *CircuitBreakerManager) GetStats() map[string]map[string]interface{} {
	cbm.mutex.RLock()
	defer cbm.mutex.RUnlock()

	stats := make(map[string]map[string]interface{})
	for name, cb := range cbm.breakers {
		counts := cb.Counts()
		stats[name] = map[string]interface{}{
			"name":                  cb.Name(),
			"state":                 cb.State().String(),
			"requests":              counts.Requests,
			"total_successes":       counts.TotalSuccesses,
			"total_failures":        counts.TotalFailures,
			"consecutive_successes": counts.ConsecutiveSuccesses,
			"consecutive_failures":  counts.ConsecutiveFailures,
			"success_rate":          counts.SuccessRate(),
			"failure_rate":          counts.FailureRate(),
		}
	}
	return stats
}

// Global circuit breaker manager
var globalCircuitBreakerManager *CircuitBreakerManager

// InitializeCircuitBreakers initializes the global circuit breaker manager
func InitializeCircuitBreakers() {
	globalCircuitBreakerManager = NewCircuitBreakerManager()
}

// GetCircuitBreakerManager returns the global circuit breaker manager
func GetCircuitBreakerManager() *CircuitBreakerManager {
	if globalCircuitBreakerManager == nil {
		InitializeCircuitBreakers()
	}
	return globalCircuitBreakerManager
}

// Predefined circuit breaker configurations
func GetDatabaseCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		MaxRequests: 3,
		Interval:    60 * time.Second,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts Counts) bool {
			return counts.Requests >= 3 && counts.ConsecutiveFailures >= 3
		},
		OnStateChange: func(name string, from CircuitBreakerState, to CircuitBreakerState) {
			// Logger would go here in production
			// logger := utils.GetLogger()
			// if logger != nil {
			//   logger.LogError(fmt.Errorf("circuit breaker state change"), "circuit_breaker", utils.LogContext{}, nil)
			// }
		},
	}
}

func GetAIServiceCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		MaxRequests: 5,
		Interval:    120 * time.Second,
		Timeout:     60 * time.Second,
		ReadyToTrip: func(counts Counts) bool {
			return counts.Requests >= 5 && (counts.ConsecutiveFailures >= 3 || counts.FailureRate() > 50)
		},
		OnStateChange: func(name string, from CircuitBreakerState, to CircuitBreakerState) {
			// Logger would go here in production
		},
		MaxConcurrentCalls: 10,
	}
}

func GetRedisCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		MaxRequests: 5,
		Interval:    30 * time.Second,
		Timeout:     15 * time.Second,
		ReadyToTrip: func(counts Counts) bool {
			return counts.Requests >= 10 && counts.ConsecutiveFailures >= 5
		},
		OnStateChange: func(name string, from CircuitBreakerState, to CircuitBreakerState) {
			// Logger would go here in production
		},
		IsSuccessful: func(err error) bool {
			// Consider timeout errors as failures, but not certain Redis errors that are recoverable
			if err == nil {
				return true
			}
			// You can add specific Redis error handling here
			return false
		},
	}
}
