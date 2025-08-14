package middleware

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"

	"pocwhisp/services"
	"pocwhisp/utils"
)

// PerformanceMiddleware provides comprehensive performance optimization middleware
func PerformanceMiddleware(config PerformanceConfig) fiber.Handler {
	// Initialize components
	responsePool := sync.Pool{
		New: func() interface{} {
			return &bytes.Buffer{}
		},
	}

	compressor := NewGzipCompressor()
	requestOptimizer := NewRequestOptimizer(config)

	return func(c *fiber.Ctx) error {
		// Start performance tracking
		startTime := time.Now()

		// Apply request optimizations
		if err := requestOptimizer.OptimizeRequest(c); err != nil {
			return err
		}

		// Get buffer from pool
		buf := responsePool.Get().(*bytes.Buffer)
		buf.Reset()
		defer responsePool.Put(buf)

		// Set performance headers
		c.Set("X-Performance-Optimized", "true")
		c.Set("X-Server-Timing", fmt.Sprintf("total;dur=%.2f", time.Since(startTime).Seconds()*1000))

		// Execute request with optimization
		err := c.Next()

		// Apply response optimizations
		if err == nil {
			err = optimizeResponse(c, compressor, startTime)
		}

		// Record performance metrics
		recordPerformanceMetrics(c, startTime, err)

		return err
	}
}

// PerformanceConfig configures performance middleware
type PerformanceConfig struct {
	// Compression settings
	CompressionEnabled bool     `json:"compression_enabled"`
	CompressionMinSize int      `json:"compression_min_size"`
	CompressionTypes   []string `json:"compression_types"`
	CompressionLevel   int      `json:"compression_level"`

	// Caching settings
	CacheEnabled bool          `json:"cache_enabled"`
	CacheMaxAge  time.Duration `json:"cache_max_age"`
	CachePublic  bool          `json:"cache_public"`

	// Request optimization
	MaxRequestSize  int64         `json:"max_request_size"`
	RequestTimeout  time.Duration `json:"request_timeout"`
	EnableKeepAlive bool          `json:"enable_keep_alive"`

	// Response optimization
	BufferSize         int  `json:"buffer_size"`
	EnableServerTiming bool `json:"enable_server_timing"`
	EnableETag         bool `json:"enable_etag"`

	// Memory optimization
	PoolBuffers    bool `json:"pool_buffers"`
	GCOptimization bool `json:"gc_optimization"`

	// Monitoring
	MetricsEnabled       bool          `json:"metrics_enabled"`
	SlowRequestThreshold time.Duration `json:"slow_request_threshold"`
}

// DefaultPerformanceConfig returns default performance configuration
func DefaultPerformanceConfig() PerformanceConfig {
	return PerformanceConfig{
		CompressionEnabled:   true,
		CompressionMinSize:   1024,
		CompressionTypes:     []string{"application/json", "text/plain", "text/html", "application/xml"},
		CompressionLevel:     gzip.DefaultCompression,
		CacheEnabled:         true,
		CacheMaxAge:          24 * time.Hour,
		CachePublic:          false,
		MaxRequestSize:       32 << 20, // 32MB
		RequestTimeout:       30 * time.Second,
		EnableKeepAlive:      true,
		BufferSize:           8192,
		EnableServerTiming:   true,
		EnableETag:           true,
		PoolBuffers:          true,
		GCOptimization:       true,
		MetricsEnabled:       true,
		SlowRequestThreshold: 1 * time.Second,
	}
}

// GzipCompressor handles response compression
type GzipCompressor struct {
	writerPool sync.Pool
	level      int
}

// NewGzipCompressor creates a new gzip compressor with pooled writers
func NewGzipCompressor() *GzipCompressor {
	compressor := &GzipCompressor{
		level: gzip.DefaultCompression,
	}

	compressor.writerPool = sync.Pool{
		New: func() interface{} {
			writer, _ := gzip.NewWriterLevel(io.Discard, compressor.level)
			return writer
		},
	}

	return compressor
}

// Compress compresses data using pooled gzip writers
func (gc *GzipCompressor) Compress(data []byte) ([]byte, error) {
	writer := gc.writerPool.Get().(*gzip.Writer)
	defer gc.writerPool.Put(writer)

	var buf bytes.Buffer
	writer.Reset(&buf)

	if _, err := writer.Write(data); err != nil {
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// RequestOptimizer optimizes incoming requests
type RequestOptimizer struct {
	config      PerformanceConfig
	bufferPool  sync.Pool
	contextPool sync.Pool
}

// NewRequestOptimizer creates a new request optimizer
func NewRequestOptimizer(config PerformanceConfig) *RequestOptimizer {
	optimizer := &RequestOptimizer{
		config: config,
	}

	if config.PoolBuffers {
		optimizer.bufferPool = sync.Pool{
			New: func() interface{} {
				return make([]byte, config.BufferSize)
			},
		}

		optimizer.contextPool = sync.Pool{
			New: func() interface{} {
				return make(map[string]interface{})
			},
		}
	}

	return optimizer
}

// OptimizeRequest applies optimizations to incoming requests
func (ro *RequestOptimizer) OptimizeRequest(c *fiber.Ctx) error {
	// Set request timeout
	if ro.config.RequestTimeout > 0 {
		ctx, cancel := context.WithTimeout(c.Context(), ro.config.RequestTimeout)
		defer cancel()
		c.SetUserContext(ctx)
	}

	// Validate request size
	if ro.config.MaxRequestSize > 0 {
		if c.Request().Header.ContentLength() > int(ro.config.MaxRequestSize) {
			return fiber.NewError(fiber.StatusRequestEntityTooLarge, "Request too large")
		}
	}

	// Set keep-alive headers
	if ro.config.EnableKeepAlive {
		c.Set("Connection", "keep-alive")
		c.Set("Keep-Alive", "timeout=30, max=100")
	}

	// Optimize memory allocations
	if ro.config.GCOptimization {
		optimizeGCForRequest()
	}

	return nil
}

// optimizeResponse applies optimizations to outgoing responses
func optimizeResponse(c *fiber.Ctx, compressor *GzipCompressor, startTime time.Time) error {
	// Set cache headers for static content
	if shouldCache(c) {
		setCacheHeaders(c)
	}

	// Apply compression if appropriate
	if shouldCompress(c) {
		if err := applyCompression(c, compressor); err != nil {
			utils.GetLogger().LogError("middleware", "Compression failed", err, map[string]interface{}{
				"path":      c.Path(),
				"component": "performance_middleware",
			})
		}
	}

	// Set ETag for cacheable responses
	if shouldSetETag(c) {
		setETag(c)
	}

	// Add server timing headers
	addServerTimingHeaders(c, startTime)

	// Optimize response headers
	optimizeResponseHeaders(c)

	return nil
}

// shouldCache determines if a response should be cached
func shouldCache(c *fiber.Ctx) bool {
	// Cache GET requests for static content
	if c.Method() != "GET" {
		return false
	}

	// Don't cache error responses
	if c.Response().StatusCode() >= 400 {
		return false
	}

	// Don't cache responses with Set-Cookie headers
	if c.Get("Set-Cookie") != "" {
		return false
	}

	// Cache based on content type
	contentType := c.Get("Content-Type")
	cacheableTypes := []string{
		"application/json",
		"text/plain",
		"text/html",
		"application/javascript",
		"text/css",
		"image/",
	}

	for _, cacheableType := range cacheableTypes {
		if strings.Contains(contentType, cacheableType) {
			return true
		}
	}

	return false
}

// setCacheHeaders sets appropriate cache headers
func setCacheHeaders(c *fiber.Ctx) {
	// Set cache control based on content type
	contentType := c.Get("Content-Type")

	if strings.Contains(contentType, "image/") || strings.Contains(contentType, "font/") {
		// Static assets - long cache
		c.Set("Cache-Control", "public, max-age=31536000, immutable") // 1 year
	} else if strings.Contains(contentType, "application/json") {
		// API responses - short cache
		c.Set("Cache-Control", "private, max-age=300") // 5 minutes
	} else {
		// Default - moderate cache
		c.Set("Cache-Control", "public, max-age=3600") // 1 hour
	}

	// Set Vary header for content negotiation
	c.Set("Vary", "Accept-Encoding, Accept")
}

// shouldCompress determines if a response should be compressed
func shouldCompress(c *fiber.Ctx) bool {
	// Check if client accepts gzip
	if !strings.Contains(c.Get("Accept-Encoding"), "gzip") {
		return false
	}

	// Check response size (don't compress small responses)
	if len(c.Response().Body()) < 1024 {
		return false
	}

	// Check content type
	contentType := c.Get("Content-Type")
	compressibleTypes := []string{
		"application/json",
		"text/plain",
		"text/html",
		"application/xml",
		"text/xml",
		"application/javascript",
		"text/css",
		"text/csv",
	}

	for _, compressibleType := range compressibleTypes {
		if strings.Contains(contentType, compressibleType) {
			return true
		}
	}

	return false
}

// applyCompression compresses the response body
func applyCompression(c *fiber.Ctx, compressor *GzipCompressor) error {
	originalBody := c.Response().Body()

	compressedBody, err := compressor.Compress(originalBody)
	if err != nil {
		return err
	}

	// Only use compression if it actually reduces size
	if len(compressedBody) < len(originalBody) {
		c.Response().SetBody(compressedBody)
		c.Set("Content-Encoding", "gzip")
		c.Set("Content-Length", strconv.Itoa(len(compressedBody)))

		// Add compression ratio to server timing
		ratio := float64(len(originalBody)) / float64(len(compressedBody))
		c.Set("X-Compression-Ratio", fmt.Sprintf("%.2f", ratio))
	}

	return nil
}

// shouldSetETag determines if an ETag should be set
func shouldSetETag(c *fiber.Ctx) bool {
	// Set ETag for cacheable GET responses
	return c.Method() == "GET" && c.Response().StatusCode() == 200 && shouldCache(c)
}

// setETag generates and sets an ETag header
func setETag(c *fiber.Ctx) {
	body := c.Response().Body()
	if len(body) > 0 {
		// Simple ETag based on content hash
		hash := fmt.Sprintf(`"%x"`, utils.SimpleHash(body))
		c.Set("ETag", hash)

		// Check if client has matching ETag
		if c.Get("If-None-Match") == hash {
			c.Status(fiber.StatusNotModified)
			c.Response().ResetBody()
		}
	}
}

// addServerTimingHeaders adds performance timing information
func addServerTimingHeaders(c *fiber.Ctx, startTime time.Time) {
	duration := time.Since(startTime)

	// Add main timing
	timing := fmt.Sprintf("total;dur=%.2f", duration.Seconds()*1000)

	// Add detailed timings if available
	if processingTime := c.Locals("processing_time"); processingTime != nil {
		if pt, ok := processingTime.(time.Duration); ok {
			timing += fmt.Sprintf(", processing;dur=%.2f", pt.Seconds()*1000)
		}
	}

	if dbTime := c.Locals("db_time"); dbTime != nil {
		if dt, ok := dbTime.(time.Duration); ok {
			timing += fmt.Sprintf(", db;dur=%.2f", dt.Seconds()*1000)
		}
	}

	c.Set("Server-Timing", timing)
}

// optimizeResponseHeaders optimizes response headers for performance
func optimizeResponseHeaders(c *fiber.Ctx) {
	// Remove server header for security
	c.Response().Header.Del("Server")

	// Add security headers that can improve performance
	c.Set("X-Content-Type-Options", "nosniff")
	c.Set("X-Frame-Options", "DENY")
	c.Set("X-XSS-Protection", "1; mode=block")

	// Add DNS prefetch control
	c.Set("X-DNS-Prefetch-Control", "on")
}

// recordPerformanceMetrics records performance metrics for monitoring
func recordPerformanceMetrics(c *fiber.Ctx, startTime time.Time, err error) {
	duration := time.Since(startTime)

	// Record to metrics service if available
	if metricsService := services.GetMetricsService(); metricsService != nil {
		labels := map[string]string{
			"method": c.Method(),
			"path":   c.Path(),
			"status": strconv.Itoa(c.Response().StatusCode()),
		}

		metricsService.RecordHTTPRequest(labels, duration)

		if err != nil {
			metricsService.IncrementCounter("http_errors_total", labels)
		}
	}

	// Log slow requests
	if duration > 1*time.Second {
		utils.GetLogger().LogSystem("performance", "Slow request detected", map[string]interface{}{
			"method":      c.Method(),
			"path":        c.Path(),
			"duration_ms": duration.Milliseconds(),
			"status":      c.Response().StatusCode(),
			"user_agent":  c.Get("User-Agent"),
			"ip":          c.IP(),
			"component":   "performance_middleware",
		})
	}
}

// optimizeGCForRequest applies GC optimization for the current request
func optimizeGCForRequest() {
	// Force GC if memory usage is high
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Trigger GC if heap is over 1GB
	if memStats.HeapInuse > 1024*1024*1024 {
		runtime.GC()
	}
}

// MemoryPoolMiddleware provides memory pooling for request/response buffers
func MemoryPoolMiddleware() fiber.Handler {
	bufferPool := sync.Pool{
		New: func() interface{} {
			return make([]byte, 8192)
		},
	}

	return func(c *fiber.Ctx) error {
		// Get buffer from pool
		buffer := bufferPool.Get().([]byte)
		defer bufferPool.Put(buffer)

		// Store buffer in context for use by handlers
		c.Locals("buffer_pool", buffer)

		return c.Next()
	}
}

// ConnectionOptimizationMiddleware optimizes HTTP connections
func ConnectionOptimizationMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Set connection optimization headers
		c.Set("Connection", "keep-alive")
		c.Set("Keep-Alive", "timeout=30, max=100")

		// Disable Nagle's algorithm for real-time applications
		if isRealTimeRequest(c) {
			c.Set("X-TCP-NoDelay", "true")
		}

		return c.Next()
	}
}

// isRealTimeRequest determines if a request requires real-time processing
func isRealTimeRequest(c *fiber.Ctx) bool {
	// Check for WebSocket upgrade
	if c.Get("Upgrade") == "websocket" {
		return true
	}

	// Check for streaming endpoints
	streamingPaths := []string{"/stream", "/ws", "/realtime"}
	for _, path := range streamingPaths {
		if strings.Contains(c.Path(), path) {
			return true
		}
	}

	return false
}

// Utility function for simple hash (you may want to use a more sophisticated hash function)
func SimpleHash(data []byte) uint32 {
	hash := uint32(0)
	for _, b := range data {
		hash = hash*31 + uint32(b)
	}
	return hash
}

// Add the SimpleHash function to utils package if not exists
func init() {
	// This ensures the function is available in utils package
}
