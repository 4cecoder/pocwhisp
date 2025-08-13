package middleware

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"pocwhisp/models"
	"pocwhisp/services"
	"pocwhisp/utils"

	"github.com/gofiber/fiber/v2"
)

// ResilienceConfig holds configuration for resilience middleware
type ResilienceConfig struct {
	EnableCircuitBreaker bool
	EnableRetry          bool
	EnableDegradation    bool
	EnableRecovery       bool
	EnableTimeout        bool
	TimeoutDuration      time.Duration
	MaxRequestSize       int64
	RateLimitEnabled     bool
	RateLimitRPS         int
}

// DefaultResilienceConfig returns default resilience configuration
func DefaultResilienceConfig() ResilienceConfig {
	return ResilienceConfig{
		EnableCircuitBreaker: true,
		EnableRetry:          true,
		EnableDegradation:    true,
		EnableRecovery:       true,
		EnableTimeout:        true,
		TimeoutDuration:      30 * time.Second,
		MaxRequestSize:       100 * 1024 * 1024, // 100MB
		RateLimitEnabled:     true,
		RateLimitRPS:         100,
	}
}

// ResilienceMiddleware creates a comprehensive resilience middleware
func ResilienceMiddleware(config ResilienceConfig) fiber.Handler {
	return func(c *fiber.Ctx) error {
		startTime := time.Now()
		requestID := c.Get("X-Request-ID")
		if requestID == "" {
			requestID = generateRequestID()
			c.Set("X-Request-ID", requestID)
		}

		// Set up context with timeout
		var ctx context.Context
		var cancel context.CancelFunc
		if config.EnableTimeout {
			ctx, cancel = context.WithTimeout(c.Context(), config.TimeoutDuration)
			defer cancel()
		} else {
			ctx = c.Context()
		}

		// Store context in fiber context
		c.SetUserContext(ctx)

		// Recovery wrapper
		if config.EnableRecovery {
			defer func() {
				if r := recover(); r != nil {
					handlePanic(c, r, requestID)
				}
			}()
		}

		// Request size validation
		if config.MaxRequestSize > 0 {
			if len(c.Body()) > int(config.MaxRequestSize) {
				return handleError(c, fiber.StatusRequestEntityTooLarge, "request_too_large",
					fmt.Sprintf("Request size exceeds maximum allowed size of %d bytes", config.MaxRequestSize), requestID)
			}
		}

		// Rate limiting (simplified implementation)
		if config.RateLimitEnabled {
			if !checkRateLimit(c, config.RateLimitRPS) {
				return handleError(c, fiber.StatusTooManyRequests, "rate_limit_exceeded",
					"Too many requests", requestID)
			}
		}

		// Execute request with resilience patterns
		var err error
		if config.EnableCircuitBreaker || config.EnableRetry || config.EnableDegradation {
			err = executeWithResilience(c, config, requestID)
		} else {
			err = c.Next()
		}

		// Handle timeout
		if ctx.Err() == context.DeadlineExceeded {
			return handleError(c, fiber.StatusRequestTimeout, "request_timeout",
				"Request timed out", requestID)
		}

		// Log request completion
		duration := time.Since(startTime)
		logger := utils.GetLogger()
		if logger != nil {
			logger.LogHTTPRequest(
				c.Method(),
				c.Path(),
				c.Get("User-Agent"),
				c.IP(),
				c.Response().StatusCode(),
				duration,
				len(c.Body()),
				len(c.Response().Body()),
				requestID,
			)
		}

		return err
	}
}

// executeWithResilience executes the request with resilience patterns
func executeWithResilience(c *fiber.Ctx, config ResilienceConfig, requestID string) error {
	endpoint := c.Method() + ":" + c.Path()

	// Get or create circuit breaker
	if config.EnableCircuitBreaker {
		cbManager := services.GetCircuitBreakerManager()
		circuitBreakerConfig := getCircuitBreakerConfigForEndpoint(endpoint)
		circuitBreaker := cbManager.GetOrCreate(endpoint, circuitBreakerConfig)

		// Execute with circuit breaker
		result, err := circuitBreaker.ExecuteWithContext(c.UserContext(), func(ctx context.Context) (interface{}, error) {
			return nil, c.Next()
		})
		_ = result // Ignore result for HTTP requests

		if err != nil {
			// Check if it's a circuit breaker error
			if cbErr, ok := err.(services.CircuitBreakerError); ok {
				return handleCircuitBreakerError(c, cbErr, requestID)
			}
			return err
		}
		return nil
	}

	// Execute with retry if circuit breaker is disabled
	if config.EnableRetry {
		retryConfig := getRetryConfigForEndpoint(endpoint)
		retrier := services.NewRetrier(retryConfig)

		result := retrier.ExecuteWithContext(c.UserContext(), func(ctx context.Context) error {
			return c.Next()
		})

		return result.LastError
	}

	// Execute normally
	return c.Next()
}

// handlePanic handles panics and returns appropriate error response
func handlePanic(c *fiber.Ctx, r interface{}, requestID string) {
	stack := debug.Stack()

	logger := utils.GetLogger()
	if logger != nil {
		logger.LogError(fmt.Errorf("panic: %v", r), "panic_recovery", utils.LogContext{
			RequestID: requestID,
			Component: "middleware",
			Operation: "recovery",
		}, map[string]interface{}{
			"stack_trace": string(stack),
		})
	}

	// Return internal server error
	c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
		Error:     "internal_server_error",
		Message:   "An internal error occurred",
		Code:      fiber.StatusInternalServerError,
		RequestID: requestID,
		Timestamp: time.Now(),
	})
}

// handleError handles errors and returns appropriate error response
func handleError(c *fiber.Ctx, statusCode int, errorType, message, requestID string) error {
	return c.Status(statusCode).JSON(models.ErrorResponse{
		Error:     errorType,
		Message:   message,
		Code:      statusCode,
		RequestID: requestID,
		Timestamp: time.Now(),
	})
}

// handleCircuitBreakerError handles circuit breaker errors
func handleCircuitBreakerError(c *fiber.Ctx, cbErr services.CircuitBreakerError, requestID string) error {
	var statusCode int
	var message string

	switch cbErr.State {
	case services.StateOpen:
		statusCode = fiber.StatusServiceUnavailable
		message = "Service temporarily unavailable"
	case services.StateHalfOpen:
		statusCode = fiber.StatusServiceUnavailable
		message = "Service is recovering, please try again later"
	default:
		statusCode = fiber.StatusServiceUnavailable
		message = "Service unavailable"
	}

	// Try degradation fallback
	degradationManager := services.GetDegradationManager()
	if degradationManager != nil {
		endpoint := c.Method() + ":" + c.Path()
		fallbackResult, err := degradationManager.ExecuteWithFallback(
			c.UserContext(),
			endpoint,
			func() (interface{}, error) {
				return nil, cbErr
			},
		)

		if err == nil && fallbackResult != nil {
			return c.JSON(fiber.Map{
				"status":     "degraded",
				"data":       fallbackResult,
				"message":    "Response from fallback service",
				"request_id": requestID,
			})
		}
	}

	return handleError(c, statusCode, "circuit_breaker_open", message, requestID)
}

// checkRateLimit performs simple rate limiting check
func checkRateLimit(c *fiber.Ctx, rps int) bool {
	// This is a simplified implementation
	// In production, you'd use a proper rate limiting algorithm like token bucket or sliding window
	clientIP := c.IP()

	// For now, always allow (implement proper rate limiting with Redis)
	_ = clientIP
	_ = rps
	return true
}

// generateRequestID generates a unique request ID
func generateRequestID() string {
	return fmt.Sprintf("req_%d_%d", time.Now().UnixNano(), time.Now().Nanosecond()%1000)
}

// getCircuitBreakerConfigForEndpoint returns circuit breaker config for specific endpoints
func getCircuitBreakerConfigForEndpoint(endpoint string) services.CircuitBreakerConfig {
	// Default configuration
	config := services.CircuitBreakerConfig{
		MaxRequests: 5,
		Interval:    60 * time.Second,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts services.Counts) bool {
			return counts.Requests >= 5 && counts.ConsecutiveFailures >= 3
		},
		OnStateChange: func(name string, from services.CircuitBreakerState, to services.CircuitBreakerState) {
			logger := utils.GetLogger()
			if logger != nil {
				logger.LogError(fmt.Errorf("circuit breaker state change"), "circuit_breaker_state_change", utils.LogContext{
					Component: "middleware",
					Operation: "circuit_breaker",
				}, map[string]interface{}{
					"endpoint":   name,
					"from_state": from.String(),
					"to_state":   to.String(),
				})
			}
		},
	}

	// Customize based on endpoint
	switch {
	case contains(endpoint, "/transcribe"):
		config.MaxRequests = 3
		config.Timeout = 60 * time.Second
		config.ReadyToTrip = func(counts services.Counts) bool {
			return counts.Requests >= 3 && counts.ConsecutiveFailures >= 2
		}
	case contains(endpoint, "/stream"):
		config.MaxRequests = 10
		config.Timeout = 120 * time.Second
		config.ReadyToTrip = func(counts services.Counts) bool {
			return counts.Requests >= 10 && counts.ConsecutiveFailures >= 5
		}
	case contains(endpoint, "/cache"):
		config.MaxRequests = 10
		config.Timeout = 10 * time.Second
		config.ReadyToTrip = func(counts services.Counts) bool {
			return counts.Requests >= 10 && counts.ConsecutiveFailures >= 5
		}
	}

	return config
}

// getRetryConfigForEndpoint returns retry config for specific endpoints
func getRetryConfigForEndpoint(endpoint string) services.RetryConfig {
	// Default configuration
	config := services.RetryConfig{
		MaxAttempts:   3,
		InitialDelay:  1 * time.Second,
		MaxDelay:      10 * time.Second,
		BackoffFactor: 2.0,
		Jitter:        true,
		ShouldRetry: func(err error) bool {
			// Don't retry client errors (4xx), only server errors (5xx)
			if fiberErr, ok := err.(*fiber.Error); ok {
				return fiberErr.Code >= 500
			}
			return true
		},
	}

	// Customize based on endpoint
	switch {
	case contains(endpoint, "/transcribe"):
		config.MaxAttempts = 2
		config.InitialDelay = 2 * time.Second
		config.MaxDelay = 30 * time.Second
	case contains(endpoint, "/health"):
		config.MaxAttempts = 1 // Don't retry health checks
	case contains(endpoint, "/cache"):
		config.MaxAttempts = 2
		config.InitialDelay = 500 * time.Millisecond
		config.MaxDelay = 5 * time.Second
	}

	return config
}

// ErrorRecoveryMiddleware creates a middleware for error recovery and logging
func ErrorRecoveryMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()
				requestID := c.Get("X-Request-ID")

				logger := utils.GetLogger()
				if logger != nil {
					logger.LogError(fmt.Errorf("panic: %v", r), "panic_recovery", utils.LogContext{
						RequestID: requestID,
						Component: "error_recovery",
						Operation: "recovery",
					}, map[string]interface{}{
						"stack_trace": string(stack),
						"method":      c.Method(),
						"path":        c.Path(),
						"user_agent":  c.Get("User-Agent"),
						"client_ip":   c.IP(),
					})
				}

				// Record metrics
				metrics := services.GetMetrics()
				if metrics != nil {
					metrics.RecordHTTPRequest(c.Method(), c.Path(), "500", 0, 0, 0)
				}

				c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
					Error:     "internal_server_error",
					Message:   "An internal error occurred",
					Code:      fiber.StatusInternalServerError,
					RequestID: requestID,
					Timestamp: time.Now(),
				})
			}
		}()

		return c.Next()
	}
}

// TimeoutMiddleware creates a middleware that enforces request timeouts
func TimeoutMiddleware(timeout time.Duration) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Create context with timeout
		ctx, cancel := context.WithTimeout(c.Context(), timeout)
		defer cancel()

		// Set the timeout context
		c.SetUserContext(ctx)

		// Channel to signal completion
		done := make(chan error, 1)

		// Execute request in goroutine
		go func() {
			done <- c.Next()
		}()

		// Wait for completion or timeout
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			requestID := c.Get("X-Request-ID")
			logger := utils.GetLogger()
			if logger != nil {
				logger.LogError(ctx.Err(), "request_timeout", utils.LogContext{
					RequestID: requestID,
					Component: "timeout_middleware",
					Operation: "timeout",
				}, map[string]interface{}{
					"timeout":   timeout.String(),
					"method":    c.Method(),
					"path":      c.Path(),
					"client_ip": c.IP(),
				})
			}

			return c.Status(fiber.StatusRequestTimeout).JSON(models.ErrorResponse{
				Error:     "request_timeout",
				Message:   fmt.Sprintf("Request timed out after %v", timeout),
				Code:      fiber.StatusRequestTimeout,
				RequestID: requestID,
				Timestamp: time.Now(),
			})
		}
	}
}

// CircuitBreakerMiddleware creates a middleware that applies circuit breaker pattern
func CircuitBreakerMiddleware(serviceName string) fiber.Handler {
	cbManager := services.GetCircuitBreakerManager()

	return func(c *fiber.Ctx) error {
		endpoint := fmt.Sprintf("%s:%s:%s", serviceName, c.Method(), c.Path())
		config := getCircuitBreakerConfigForEndpoint(endpoint)
		circuitBreaker := cbManager.GetOrCreate(endpoint, config)

		result, err := circuitBreaker.ExecuteWithContext(c.UserContext(), func(ctx context.Context) (interface{}, error) {
			return nil, c.Next()
		})
		_ = result

		if err != nil {
			if cbErr, ok := err.(services.CircuitBreakerError); ok {
				requestID := c.Get("X-Request-ID")
				return handleCircuitBreakerError(c, cbErr, requestID)
			}
		}

		return err
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
