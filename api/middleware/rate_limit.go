package middleware

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"pocwhisp/models"

	"github.com/go-redis/redis/v8"
	"github.com/gofiber/fiber/v2"
)

// RateLimitConfig holds configuration for rate limiting
type RateLimitConfig struct {
	// Rate limiting parameters
	MaxRequests            int           // Maximum requests allowed
	WindowSize             time.Duration // Time window for rate limiting
	SkipFailedRequests     bool          // Whether to skip failed requests from rate limiting
	SkipSuccessfulRequests bool          // Whether to skip successful requests from rate limiting

	// Key generation
	KeyGenerator func(c *fiber.Ctx) string // Function to generate rate limit key

	// Storage
	Storage  *redis.Client // Redis client for distributed rate limiting
	UseRedis bool          // Whether to use Redis for storage

	// Response configuration
	Message    string            // Custom message for rate limit exceeded
	StatusCode int               // HTTP status code for rate limit exceeded
	Headers    map[string]string // Custom headers to include

	// Advanced options
	SlidingWindow bool // Use sliding window instead of fixed window
	BurstSize     int  // Allow burst above normal rate

	// Callbacks
	LimitReached func(c *fiber.Ctx) error // Called when limit is reached
	Next         func(c *fiber.Ctx) bool  // Skip rate limiting for certain requests
}

// RateLimiter handles rate limiting logic
type RateLimiter struct {
	config        RateLimitConfig
	inMemoryStore map[string]*windowData
	mutex         sync.RWMutex

	// Cleanup for in-memory storage
	cleanupTicker *time.Ticker
	stopCleanup   chan bool
}

// windowData holds rate limiting data for a specific key
type windowData struct {
	requests   int
	resetTime  time.Time
	lastAccess time.Time

	// For sliding window
	requestTimes []time.Time
}

// DefaultRateLimitConfig returns default rate limiting configuration
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		MaxRequests:            100,
		WindowSize:             time.Hour,
		SkipFailedRequests:     false,
		SkipSuccessfulRequests: false,
		KeyGenerator:           defaultKeyGenerator,
		UseRedis:               false,
		Message:                "Too many requests",
		StatusCode:             fiber.StatusTooManyRequests,
		Headers:                map[string]string{},
		SlidingWindow:          false,
		BurstSize:              0,
		LimitReached:           nil,
		Next:                   nil,
	}
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(config RateLimitConfig) *RateLimiter {
	if config.KeyGenerator == nil {
		config.KeyGenerator = defaultKeyGenerator
	}

	if config.StatusCode == 0 {
		config.StatusCode = fiber.StatusTooManyRequests
	}

	if config.Message == "" {
		config.Message = "Too many requests"
	}

	rl := &RateLimiter{
		config:        config,
		inMemoryStore: make(map[string]*windowData),
		stopCleanup:   make(chan bool),
	}

	// Start cleanup routine for in-memory storage
	if !config.UseRedis {
		rl.startCleanup()
	}

	return rl
}

// RateLimit creates rate limiting middleware
func RateLimit(config ...RateLimitConfig) fiber.Handler {
	cfg := DefaultRateLimitConfig()
	if len(config) > 0 {
		cfg = config[0]
	}

	limiter := NewRateLimiter(cfg)

	return func(c *fiber.Ctx) error {
		// Skip if Next function returns true
		if cfg.Next != nil && cfg.Next(c) {
			return c.Next()
		}

		// Generate rate limit key
		key := cfg.KeyGenerator(c)

		// Check rate limit
		allowed, reset, remaining, err := limiter.checkLimit(key)
		if err != nil {
			// Log error but don't block request
			return c.Next()
		}

		// Set rate limit headers
		c.Set("X-RateLimit-Limit", strconv.Itoa(cfg.MaxRequests))
		c.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
		c.Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))

		// Add custom headers
		for key, value := range cfg.Headers {
			c.Set(key, value)
		}

		if !allowed {
			// Rate limit exceeded
			if cfg.LimitReached != nil {
				return cfg.LimitReached(c)
			}

			c.Set("Retry-After", strconv.FormatInt(int64(reset.Sub(time.Now()).Seconds()), 10))

			return c.Status(cfg.StatusCode).JSON(models.ErrorResponse{
				Error:     "rate_limit_exceeded",
				Message:   cfg.Message,
				Code:      cfg.StatusCode,
				RequestID: c.Get("X-Request-ID"),
				Timestamp: time.Now(),
			})
		}

		// Process request
		err = c.Next()

		// Update rate limit based on response if configured
		if err == nil && cfg.SkipSuccessfulRequests {
			limiter.decrementCount(key)
		} else if err != nil && cfg.SkipFailedRequests {
			limiter.decrementCount(key)
		}

		return err
	}
}

// checkLimit checks if request is within rate limit
func (rl *RateLimiter) checkLimit(key string) (allowed bool, reset time.Time, remaining int, err error) {
	if rl.config.UseRedis && rl.config.Storage != nil {
		return rl.checkRedisLimit(key)
	}
	return rl.checkInMemoryLimit(key)
}

// checkInMemoryLimit checks rate limit using in-memory storage
func (rl *RateLimiter) checkInMemoryLimit(key string) (bool, time.Time, int, error) {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	now := time.Now()

	// Get or create window data
	data, exists := rl.inMemoryStore[key]
	if !exists {
		data = &windowData{
			requests:     0,
			resetTime:    now.Add(rl.config.WindowSize),
			lastAccess:   now,
			requestTimes: make([]time.Time, 0),
		}
		rl.inMemoryStore[key] = data
	}

	data.lastAccess = now

	if rl.config.SlidingWindow {
		return rl.checkSlidingWindow(data, now)
	}

	return rl.checkFixedWindow(data, now)
}

// checkFixedWindow implements fixed window rate limiting
func (rl *RateLimiter) checkFixedWindow(data *windowData, now time.Time) (bool, time.Time, int, error) {
	// Reset window if expired
	if now.After(data.resetTime) {
		data.requests = 0
		data.resetTime = now.Add(rl.config.WindowSize)
	}

	// Check if within limit
	if data.requests >= rl.config.MaxRequests {
		remaining := 0
		return false, data.resetTime, remaining, nil
	}

	// Increment request count
	data.requests++
	remaining := rl.config.MaxRequests - data.requests

	return true, data.resetTime, remaining, nil
}

// checkSlidingWindow implements sliding window rate limiting
func (rl *RateLimiter) checkSlidingWindow(data *windowData, now time.Time) (bool, time.Time, int, error) {
	windowStart := now.Add(-rl.config.WindowSize)

	// Remove old requests outside the window
	validRequests := make([]time.Time, 0)
	for _, reqTime := range data.requestTimes {
		if reqTime.After(windowStart) {
			validRequests = append(validRequests, reqTime)
		}
	}
	data.requestTimes = validRequests

	// Check if within limit
	if len(data.requestTimes) >= rl.config.MaxRequests {
		nextReset := data.requestTimes[0].Add(rl.config.WindowSize)
		remaining := 0
		return false, nextReset, remaining, nil
	}

	// Add current request
	data.requestTimes = append(data.requestTimes, now)
	remaining := rl.config.MaxRequests - len(data.requestTimes)

	// Calculate next reset time
	nextReset := now.Add(rl.config.WindowSize)
	if len(data.requestTimes) > 0 {
		nextReset = data.requestTimes[0].Add(rl.config.WindowSize)
	}

	return true, nextReset, remaining, nil
}

// checkRedisLimit checks rate limit using Redis storage
func (rl *RateLimiter) checkRedisLimit(key string) (bool, time.Time, int, error) {
	ctx := rl.config.Storage.Context()
	redisKey := fmt.Sprintf("rate_limit:%s", key)

	if rl.config.SlidingWindow {
		return rl.checkRedisSlidingWindow(redisKey)
	}

	return rl.checkRedisFixedWindow(redisKey)
}

// checkRedisFixedWindow implements Redis-based fixed window rate limiting
func (rl *RateLimiter) checkRedisFixedWindow(redisKey string) (bool, time.Time, int, error) {
	ctx := rl.config.Storage.Context()
	now := time.Now()

	// Use Redis pipeline for atomic operations
	pipe := rl.config.Storage.Pipeline()

	// Get current count
	getCurrentCmd := pipe.Get(ctx, redisKey)

	// Increment counter
	incrCmd := pipe.Incr(ctx, redisKey)

	// Set expiration if this is the first request in window
	pipe.Expire(ctx, redisKey, rl.config.WindowSize)

	// Execute pipeline
	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return true, now.Add(rl.config.WindowSize), rl.config.MaxRequests - 1, err
	}

	// Get current count
	current := int(incrCmd.Val())

	// Calculate reset time
	ttl := rl.config.Storage.TTL(ctx, redisKey).Val()
	reset := now.Add(ttl)

	// Check if over limit
	if current > rl.config.MaxRequests {
		// Decrement back since we're rejecting this request
		rl.config.Storage.Decr(ctx, redisKey)
		remaining := 0
		return false, reset, remaining, nil
	}

	remaining := rl.config.MaxRequests - current
	return true, reset, remaining, nil
}

// checkRedisSlidingWindow implements Redis-based sliding window rate limiting
func (rl *RateLimiter) checkRedisSlidingWindow(redisKey string) (bool, time.Time, int, error) {
	ctx := rl.config.Storage.Context()
	now := time.Now()
	nowScore := float64(now.UnixNano())
	windowStart := now.Add(-rl.config.WindowSize)
	windowStartScore := float64(windowStart.UnixNano())

	// Use Redis sorted set for sliding window
	pipe := rl.config.Storage.Pipeline()

	// Remove expired entries
	pipe.ZRemRangeByScore(ctx, redisKey, "0", fmt.Sprintf("%.0f", windowStartScore))

	// Count current requests in window
	countCmd := pipe.ZCard(ctx, redisKey)

	// Execute pipeline
	_, err := pipe.Exec(ctx)
	if err != nil {
		return true, now.Add(rl.config.WindowSize), rl.config.MaxRequests - 1, err
	}

	currentCount := int(countCmd.Val())

	// Check if over limit
	if currentCount >= rl.config.MaxRequests {
		// Calculate next reset time
		oldestEntries := rl.config.Storage.ZRangeWithScores(ctx, redisKey, 0, 0).Val()
		var nextReset time.Time
		if len(oldestEntries) > 0 {
			oldestTime := time.Unix(0, int64(oldestEntries[0].Score))
			nextReset = oldestTime.Add(rl.config.WindowSize)
		} else {
			nextReset = now.Add(rl.config.WindowSize)
		}

		return false, nextReset, 0, nil
	}

	// Add current request
	rl.config.Storage.ZAdd(ctx, redisKey, &redis.Z{
		Score:  nowScore,
		Member: fmt.Sprintf("%d", now.UnixNano()),
	})

	// Set expiration
	rl.config.Storage.Expire(ctx, redisKey, rl.config.WindowSize)

	remaining := rl.config.MaxRequests - currentCount - 1
	reset := now.Add(rl.config.WindowSize)

	return true, reset, remaining, nil
}

// decrementCount decrements the request count (used when skipping failed/successful requests)
func (rl *RateLimiter) decrementCount(key string) {
	if rl.config.UseRedis && rl.config.Storage != nil {
		ctx := rl.config.Storage.Context()
		redisKey := fmt.Sprintf("rate_limit:%s", key)
		rl.config.Storage.Decr(ctx, redisKey)
	} else {
		rl.mutex.Lock()
		defer rl.mutex.Unlock()

		if data, exists := rl.inMemoryStore[key]; exists {
			if rl.config.SlidingWindow && len(data.requestTimes) > 0 {
				// Remove last request time
				data.requestTimes = data.requestTimes[:len(data.requestTimes)-1]
			} else if data.requests > 0 {
				data.requests--
			}
		}
	}
}

// startCleanup starts the cleanup routine for in-memory storage
func (rl *RateLimiter) startCleanup() {
	rl.cleanupTicker = time.NewTicker(5 * time.Minute)

	go func() {
		for {
			select {
			case <-rl.cleanupTicker.C:
				rl.cleanup()
			case <-rl.stopCleanup:
				rl.cleanupTicker.Stop()
				return
			}
		}
	}()
}

// cleanup removes expired entries from in-memory storage
func (rl *RateLimiter) cleanup() {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	now := time.Now()
	expiry := 2 * rl.config.WindowSize // Keep entries for 2x window size

	for key, data := range rl.inMemoryStore {
		if now.Sub(data.lastAccess) > expiry {
			delete(rl.inMemoryStore, key)
		}
	}
}

// Stop stops the rate limiter and cleanup routines
func (rl *RateLimiter) Stop() {
	if rl.cleanupTicker != nil {
		close(rl.stopCleanup)
	}
}

// Predefined rate limit configurations

// DefaultAPIRateLimit provides standard API rate limiting
func DefaultAPIRateLimit() RateLimitConfig {
	return RateLimitConfig{
		MaxRequests:   1000,
		WindowSize:    time.Hour,
		KeyGenerator:  ipBasedKeyGenerator,
		Message:       "API rate limit exceeded",
		SlidingWindow: false,
	}
}

// StrictAPIRateLimit provides strict API rate limiting
func StrictAPIRateLimit() RateLimitConfig {
	return RateLimitConfig{
		MaxRequests:   100,
		WindowSize:    time.Hour,
		KeyGenerator:  userBasedKeyGenerator,
		Message:       "API rate limit exceeded",
		SlidingWindow: true,
	}
}

// TranscriptionRateLimit provides rate limiting for transcription endpoints
func TranscriptionRateLimit() RateLimitConfig {
	return RateLimitConfig{
		MaxRequests:   50,
		WindowSize:    time.Hour,
		KeyGenerator:  userBasedKeyGenerator,
		Message:       "Transcription rate limit exceeded",
		SlidingWindow: true,
		BurstSize:     10,
	}
}

// BatchRateLimit provides rate limiting for batch processing
func BatchRateLimit() RateLimitConfig {
	return RateLimitConfig{
		MaxRequests:   10,
		WindowSize:    24 * time.Hour,
		KeyGenerator:  userBasedKeyGenerator,
		Message:       "Batch processing rate limit exceeded",
		SlidingWindow: true,
	}
}

// WebSocketRateLimit provides rate limiting for WebSocket connections
func WebSocketRateLimit() RateLimitConfig {
	return RateLimitConfig{
		MaxRequests:   100,
		WindowSize:    time.Minute,
		KeyGenerator:  ipBasedKeyGenerator,
		Message:       "WebSocket rate limit exceeded",
		SlidingWindow: true,
	}
}

// Key generator functions

// defaultKeyGenerator generates rate limit key based on IP
func defaultKeyGenerator(c *fiber.Ctx) string {
	return c.IP()
}

// ipBasedKeyGenerator generates key based on client IP
func ipBasedKeyGenerator(c *fiber.Ctx) string {
	return fmt.Sprintf("ip:%s", c.IP())
}

// userBasedKeyGenerator generates key based on authenticated user
func userBasedKeyGenerator(c *fiber.Ctx) string {
	userID := GetUserID(c)
	if userID != "" {
		return fmt.Sprintf("user:%s", userID)
	}
	return fmt.Sprintf("ip:%s", c.IP())
}

// apiKeyBasedKeyGenerator generates key based on API key
func apiKeyBasedKeyGenerator(c *fiber.Ctx) string {
	apiKey := c.Locals("api_key")
	if apiKey != nil {
		return fmt.Sprintf("api_key:%s", apiKey.(string))
	}
	return userBasedKeyGenerator(c)
}

// operationBasedKeyGenerator generates key based on operation and user
func operationBasedKeyGenerator(operation string) func(c *fiber.Ctx) string {
	return func(c *fiber.Ctx) string {
		userID := GetUserID(c)
		if userID != "" {
			return fmt.Sprintf("user:%s:op:%s", userID, operation)
		}
		return fmt.Sprintf("ip:%s:op:%s", c.IP(), operation)
	}
}
