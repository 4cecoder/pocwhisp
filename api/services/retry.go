package services

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"
)

// RetryPolicy defines how retries should be performed
type RetryPolicy struct {
	MaxAttempts     int
	InitialInterval time.Duration
	MaxInterval     time.Duration
	Multiplier      float64
	Jitter          bool
	RetryableErrors []error
	ShouldRetry     func(error) bool
	OnRetry         func(attempt int, err error, delay time.Duration)
}

// RetryConfig provides configuration for retry operations
type RetryConfig struct {
	MaxAttempts     int
	InitialDelay    time.Duration
	MaxDelay        time.Duration
	BackoffFactor   float64
	Jitter          bool
	RetryableErrors []string
	ShouldRetry     func(error) bool
	OnRetry         func(attempt int, err error, delay time.Duration)
}

// RetryOperation represents an operation that can be retried
type RetryOperation func() error

// RetryOperationWithResult represents an operation that returns a result and can be retried
type RetryOperationWithResult[T any] func() (T, error)

// RetryContext represents an operation with context that can be retried
type RetryContext func(ctx context.Context) error

// RetryContextWithResult represents an operation with context that returns a result and can be retried
type RetryContextWithResult[T any] func(ctx context.Context) (T, error)

// RetryResult contains the result of a retry operation
type RetryResult struct {
	Attempts   int
	TotalDelay time.Duration
	LastError  error
	Success    bool
}

// Retrier provides retry functionality
type Retrier struct {
	config RetryConfig
}

// NewRetrier creates a new retrier with the given configuration
func NewRetrier(config RetryConfig) *Retrier {
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = 3
	}
	if config.InitialDelay <= 0 {
		config.InitialDelay = 1 * time.Second
	}
	if config.MaxDelay <= 0 {
		config.MaxDelay = 30 * time.Second
	}
	if config.BackoffFactor <= 0 {
		config.BackoffFactor = 2.0
	}

	return &Retrier{config: config}
}

// Execute executes an operation with retry logic
func (r *Retrier) Execute(operation RetryOperation) *RetryResult {
	return r.ExecuteWithContext(context.Background(), func(ctx context.Context) error {
		return operation()
	})
}

// ExecuteWithContext executes an operation with context and retry logic
func (r *Retrier) ExecuteWithContext(ctx context.Context, operation RetryContext) *RetryResult {
	result := &RetryResult{}
	delay := r.config.InitialDelay

	for attempt := 1; attempt <= r.config.MaxAttempts; attempt++ {
		result.Attempts = attempt

		// Execute the operation
		err := operation(ctx)
		if err == nil {
			result.Success = true
			return result
		}

		result.LastError = err

		// Check if we should retry
		if !r.shouldRetry(err) {
			break
		}

		// Don't sleep after the last attempt
		if attempt == r.config.MaxAttempts {
			break
		}

		// Check context cancellation
		if ctx.Err() != nil {
			result.LastError = ctx.Err()
			break
		}

		// Calculate delay with backoff and jitter
		actualDelay := r.calculateDelay(delay, attempt)
		result.TotalDelay += actualDelay

		// Call retry callback if provided
		if r.config.OnRetry != nil {
			r.config.OnRetry(attempt, err, actualDelay)
		}

		// Wait before retrying
		timer := time.NewTimer(actualDelay)
		select {
		case <-timer.C:
			// Continue to next attempt
		case <-ctx.Done():
			timer.Stop()
			result.LastError = ctx.Err()
			return result
		}

		// Update delay for next iteration
		delay = time.Duration(float64(delay) * r.config.BackoffFactor)
		if delay > r.config.MaxDelay {
			delay = r.config.MaxDelay
		}
	}

	return result
}

// ExecuteWithResult executes an operation that returns a result with retry logic
func (r *Retrier) ExecuteWithResult[T any](operation RetryOperationWithResult[T]) (T, *RetryResult) {
	return r.ExecuteWithResultAndContext(context.Background(), func(ctx context.Context) (T, error) {
		return operation()
	})
}

// ExecuteWithResultAndContext executes an operation with context that returns a result with retry logic
func (r *Retrier) ExecuteWithResultAndContext[T any](ctx context.Context, operation RetryContextWithResult[T]) (T, *RetryResult) {
	var result T
	retryResult := &RetryResult{}
	delay := r.config.InitialDelay

	for attempt := 1; attempt <= r.config.MaxAttempts; attempt++ {
		retryResult.Attempts = attempt

		// Execute the operation
		res, err := operation(ctx)
		if err == nil {
			retryResult.Success = true
			return res, retryResult
		}

		retryResult.LastError = err

		// Check if we should retry
		if !r.shouldRetry(err) {
			break
		}

		// Don't sleep after the last attempt
		if attempt == r.config.MaxAttempts {
			break
		}

		// Check context cancellation
		if ctx.Err() != nil {
			retryResult.LastError = ctx.Err()
			break
		}

		// Calculate delay with backoff and jitter
		actualDelay := r.calculateDelay(delay, attempt)
		retryResult.TotalDelay += actualDelay

		// Call retry callback if provided
		if r.config.OnRetry != nil {
			r.config.OnRetry(attempt, err, actualDelay)
		}

		// Wait before retrying
		timer := time.NewTimer(actualDelay)
		select {
		case <-timer.C:
			// Continue to next attempt
		case <-ctx.Done():
			timer.Stop()
			retryResult.LastError = ctx.Err()
			return result, retryResult
		}

		// Update delay for next iteration
		delay = time.Duration(float64(delay) * r.config.BackoffFactor)
		if delay > r.config.MaxDelay {
			delay = r.config.MaxDelay
		}
	}

	return result, retryResult
}

// shouldRetry determines if an error should trigger a retry
func (r *Retrier) shouldRetry(err error) bool {
	if r.config.ShouldRetry != nil {
		return r.config.ShouldRetry(err)
	}

	// Default retry logic for common transient errors
	return isRetryableError(err)
}

// calculateDelay calculates the delay for the next retry with jitter
func (r *Retrier) calculateDelay(baseDelay time.Duration, attempt int) time.Duration {
	delay := baseDelay

	if r.config.Jitter {
		// Add random jitter (±25% of the delay)
		jitterRange := float64(delay) * 0.25
		jitter := time.Duration(rand.Float64()*jitterRange*2 - jitterRange)
		delay += jitter
	}

	// Ensure delay is not negative
	if delay < 0 {
		delay = r.config.InitialDelay
	}

	// Ensure delay doesn't exceed maximum
	if delay > r.config.MaxDelay {
		delay = r.config.MaxDelay
	}

	return delay
}

// Predefined retry configurations

// DatabaseRetryConfig returns a retry configuration optimized for database operations
func DatabaseRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:   3,
		InitialDelay:  500 * time.Millisecond,
		MaxDelay:      5 * time.Second,
		BackoffFactor: 2.0,
		Jitter:        true,
		ShouldRetry: func(err error) bool {
			// Retry on connection errors, timeouts, etc.
			return isTransientDatabaseError(err)
		},
		OnRetry: func(attempt int, err error, delay time.Duration) {
			logger := GetLogger()
			if logger != nil {
				logger.LogError(err, "database_retry", LogContext{
					Component: "database",
					Operation: "retry",
				}, map[string]interface{}{
					"attempt": attempt,
					"delay":   delay.String(),
				})
			}
		},
	}
}

// AIServiceRetryConfig returns a retry configuration optimized for AI service calls
func AIServiceRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:   5,
		InitialDelay:  1 * time.Second,
		MaxDelay:      30 * time.Second,
		BackoffFactor: 2.0,
		Jitter:        true,
		ShouldRetry: func(err error) bool {
			return isTransientAIServiceError(err)
		},
		OnRetry: func(attempt int, err error, delay time.Duration) {
			logger := GetLogger()
			if logger != nil {
				logger.LogAIService("ai_service", "retry", 0, delay, false, err.Error())
			}
		},
	}
}

// RedisRetryConfig returns a retry configuration optimized for Redis operations
func RedisRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:   3,
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      2 * time.Second,
		BackoffFactor: 1.5,
		Jitter:        true,
		ShouldRetry: func(err error) bool {
			return isTransientRedisError(err)
		},
		OnRetry: func(attempt int, err error, delay time.Duration) {
			logger := GetLogger()
			if logger != nil {
				logger.LogCacheOperation("retry", "unknown", false, delay, "redis")
			}
		},
	}
}

// QueueRetryConfig returns a retry configuration optimized for queue operations
func QueueRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:   3,
		InitialDelay:  2 * time.Second,
		MaxDelay:      10 * time.Second,
		BackoffFactor: 2.0,
		Jitter:        true,
		ShouldRetry: func(err error) bool {
			return isTransientQueueError(err)
		},
		OnRetry: func(attempt int, err error, delay time.Duration) {
			logger := GetLogger()
			if logger != nil {
				logger.LogQueueJob("unknown", "unknown", 0, "retry", delay, "unknown", err.Error())
			}
		},
	}
}

// HTTP client retry config
func HTTPRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:   3,
		InitialDelay:  1 * time.Second,
		MaxDelay:      10 * time.Second,
		BackoffFactor: 2.0,
		Jitter:        true,
		ShouldRetry: func(err error) bool {
			return isRetryableHTTPError(err)
		},
		OnRetry: func(attempt int, err error, delay time.Duration) {
			logger := GetLogger()
			if logger != nil {
				logger.LogError(err, "http_retry", LogContext{
					Component: "http_client",
					Operation: "retry",
				}, map[string]interface{}{
					"attempt": attempt,
					"delay":   delay.String(),
				})
			}
		},
	}
}

// Helper functions to determine if errors are retryable

// isRetryableError determines if an error is generally retryable
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for common retryable error patterns
	errorMsg := err.Error()
	
	retryablePatterns := []string{
		"connection refused",
		"connection reset",
		"timeout",
		"temporary failure",
		"service unavailable",
		"too many requests",
		"rate limit",
		"network error",
		"socket error",
	}

	for _, pattern := range retryablePatterns {
		if contains(errorMsg, pattern) {
			return true
		}
	}

	return false
}

// isTransientDatabaseError determines if a database error is transient
func isTransientDatabaseError(err error) bool {
	if err == nil {
		return false
	}

	errorMsg := err.Error()
	
	transientPatterns := []string{
		"connection refused",
		"connection timeout",
		"connection reset",
		"database is locked",
		"deadlock",
		"too many connections",
		"server has gone away",
	}

	for _, pattern := range transientPatterns {
		if contains(errorMsg, pattern) {
			return true
		}
	}

	return false
}

// isTransientAIServiceError determines if an AI service error is transient
func isTransientAIServiceError(err error) bool {
	if err == nil {
		return false
	}

	errorMsg := err.Error()
	
	transientPatterns := []string{
		"connection refused",
		"timeout",
		"service unavailable",
		"rate limit",
		"too many requests",
		"model loading",
		"memory error",
		"cuda error",
		"gpu error",
	}

	for _, pattern := range transientPatterns {
		if contains(errorMsg, pattern) {
			return true
		}
	}

	return false
}

// isTransientRedisError determines if a Redis error is transient
func isTransientRedisError(err error) bool {
	if err == nil {
		return false
	}

	errorMsg := err.Error()
	
	transientPatterns := []string{
		"connection refused",
		"connection timeout",
		"connection reset",
		"redis timeout",
		"read timeout",
		"write timeout",
		"no connection available",
	}

	for _, pattern := range transientPatterns {
		if contains(errorMsg, pattern) {
			return true
		}
	}

	return false
}

// isTransientQueueError determines if a queue error is transient
func isTransientQueueError(err error) bool {
	if err == nil {
		return false
	}

	errorMsg := err.Error()
	
	transientPatterns := []string{
		"connection refused",
		"timeout",
		"queue full",
		"worker unavailable",
		"redis",
	}

	for _, pattern := range transientPatterns {
		if contains(errorMsg, pattern) {
			return true
		}
	}

	return false
}

// isRetryableHTTPError determines if an HTTP error is retryable
func isRetryableHTTPError(err error) bool {
	if err == nil {
		return false
	}

	errorMsg := err.Error()
	
	retryablePatterns := []string{
		"connection refused",
		"timeout",
		"503", // Service Unavailable
		"502", // Bad Gateway
		"504", // Gateway Timeout
		"429", // Too Many Requests
		"500", // Internal Server Error (sometimes retryable)
	}

	for _, pattern := range retryablePatterns {
		if contains(errorMsg, pattern) {
			return true
		}
	}

	return false
}

// Helper function to check if a string contains a substring (case-insensitive)
func contains(str, substr string) bool {
	return len(str) >= len(substr) && 
		   (str == substr || 
		    len(str) > len(substr) && 
		    (str[:len(substr)] == substr || 
		     str[len(str)-len(substr):] == substr ||
		     indexOf(str, substr) >= 0))
}

// Simple indexOf implementation
func indexOf(str, substr string) int {
	for i := 0; i <= len(str)-len(substr); i++ {
		if str[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// Convenience functions for common retry patterns

// WithDatabaseRetry wraps a database operation with retry logic
func WithDatabaseRetry(operation RetryOperation) error {
	retrier := NewRetrier(DatabaseRetryConfig())
	result := retrier.Execute(operation)
	return result.LastError
}

// WithAIServiceRetry wraps an AI service operation with retry logic
func WithAIServiceRetry(operation RetryOperation) error {
	retrier := NewRetrier(AIServiceRetryConfig())
	result := retrier.Execute(operation)
	return result.LastError
}

// WithRedisRetry wraps a Redis operation with retry logic
func WithRedisRetry(operation RetryOperation) error {
	retrier := NewRetrier(RedisRetryConfig())
	result := retrier.Execute(operation)
	return result.LastError
}

// WithHTTPRetry wraps an HTTP operation with retry logic
func WithHTTPRetry(operation RetryOperation) error {
	retrier := NewRetrier(HTTPRetryConfig())
	result := retrier.Execute(operation)
	return result.LastError
}

// WithCustomRetry wraps an operation with custom retry configuration
func WithCustomRetry(config RetryConfig, operation RetryOperation) error {
	retrier := NewRetrier(config)
	result := retrier.Execute(operation)
	return result.LastError
}

// ExponentialBackoff calculates delay using exponential backoff
func ExponentialBackoff(attempt int, baseDelay time.Duration, maxDelay time.Duration, jitter bool) time.Duration {
	delay := time.Duration(float64(baseDelay) * math.Pow(2, float64(attempt-1)))
	
	if jitter {
		// Add random jitter (±25% of the delay)
		jitterRange := float64(delay) * 0.25
		jitterValue := time.Duration(rand.Float64()*jitterRange*2 - jitterRange)
		delay += jitterValue
	}
	
	if delay > maxDelay {
		delay = maxDelay
	}
	
	if delay < baseDelay {
		delay = baseDelay
	}
	
	return delay
}

// LinearBackoff calculates delay using linear backoff
func LinearBackoff(attempt int, baseDelay time.Duration, maxDelay time.Duration) time.Duration {
	delay := time.Duration(int64(baseDelay) * int64(attempt))
	
	if delay > maxDelay {
		delay = maxDelay
	}
	
	return delay
}

// FixedBackoff returns a fixed delay regardless of attempt number
func FixedBackoff(baseDelay time.Duration) time.Duration {
	return baseDelay
}
