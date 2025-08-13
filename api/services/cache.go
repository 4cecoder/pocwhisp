package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

// CacheLevel defines different cache levels
type CacheLevel int

const (
	CacheLevelL1 CacheLevel = iota // In-memory cache (fastest)
	CacheLevelL2                   // Redis cache (fast)
	CacheLevelL3                   // Database cache (slowest)
)

// CacheEntry represents a cached item
type CacheEntry struct {
	Key        string      `json:"key"`
	Value      interface{} `json:"value"`
	Expiration time.Time   `json:"expiration"`
	CreatedAt  time.Time   `json:"created_at"`
	AccessedAt time.Time   `json:"accessed_at"`
	HitCount   int64       `json:"hit_count"`
	Level      CacheLevel  `json:"level"`
}

// CacheStats holds cache statistics
type CacheStats struct {
	L1Stats CacheLevelStats `json:"l1_stats"`
	L2Stats CacheLevelStats `json:"l2_stats"`
	Overall CacheLevelStats `json:"overall"`
}

// CacheLevelStats holds statistics for a specific cache level
type CacheLevelStats struct {
	Hits        int64   `json:"hits"`
	Misses      int64   `json:"misses"`
	HitRate     float64 `json:"hit_rate"`
	Size        int64   `json:"size"`
	MemoryUsage int64   `json:"memory_usage_bytes"`
	Evictions   int64   `json:"evictions"`
}

// CacheConfig holds cache configuration
type CacheConfig struct {
	RedisURL        string
	L1MaxSize       int
	L1TTL           time.Duration
	L2TTL           time.Duration
	EnableMetrics   bool
	StatsInterval   time.Duration
	CleanupInterval time.Duration
}

// MultiLevelCache implements a multi-level caching system
type MultiLevelCache struct {
	config  *CacheConfig
	redis   *redis.Client
	l1Cache *InMemoryCache
	stats   *CacheStats
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.RWMutex
}

// InMemoryCache implements L1 cache
type InMemoryCache struct {
	items     map[string]*CacheEntry
	maxSize   int
	ttl       time.Duration
	mu        sync.RWMutex
	hitCount  int64
	missCount int64
	evictions int64
}

// Cache key patterns
const (
	KeyTranscript    = "pocwhisp:cache:transcript:{session_id}"
	KeySummary       = "pocwhisp:cache:summary:{session_id}"
	KeyAudioMetadata = "pocwhisp:cache:audio_meta:{file_hash}"
	KeyJobResult     = "pocwhisp:cache:job:{job_id}"
	KeyUserSession   = "pocwhisp:cache:user_session:{user_id}"
	KeyAPIResponse   = "pocwhisp:cache:api_response:{endpoint}:{params_hash}"
	KeyModelResult   = "pocwhisp:cache:model_result:{model}:{input_hash}"
	KeyHealthCheck   = "pocwhisp:cache:health:{service}"
	KeyStatsSnapshot = "pocwhisp:cache:stats:snapshot"
)

// NewMultiLevelCache creates a new multi-level cache
func NewMultiLevelCache(config *CacheConfig) (*MultiLevelCache, error) {
	if config == nil {
		config = DefaultCacheConfig()
	}

	// Initialize Redis client
	opt, err := redis.ParseURL(config.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid Redis URL: %w", err)
	}

	redisClient := redis.NewClient(opt)

	// Test Redis connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	ctx, cancel = context.WithCancel(context.Background())

	cache := &MultiLevelCache{
		config:  config,
		redis:   redisClient,
		l1Cache: NewInMemoryCache(config.L1MaxSize, config.L1TTL),
		stats: &CacheStats{
			L1Stats: CacheLevelStats{},
			L2Stats: CacheLevelStats{},
			Overall: CacheLevelStats{},
		},
		ctx:    ctx,
		cancel: cancel,
	}

	// Start background tasks
	if config.EnableMetrics {
		go cache.statsUpdater()
	}
	go cache.cleanupWorker()

	log.Printf("Multi-level cache initialized with Redis at %s", config.RedisURL)
	return cache, nil
}

// DefaultCacheConfig returns default cache configuration
func DefaultCacheConfig() *CacheConfig {
	return &CacheConfig{
		RedisURL:        "redis://localhost:6379",
		L1MaxSize:       10000,
		L1TTL:           5 * time.Minute,
		L2TTL:           1 * time.Hour,
		EnableMetrics:   true,
		StatsInterval:   30 * time.Second,
		CleanupInterval: 5 * time.Minute,
	}
}

// Get retrieves a value from cache (tries L1, then L2)
func (c *MultiLevelCache) Get(key string) (interface{}, bool) {
	// Try L1 cache first
	if value, found := c.l1Cache.Get(key); found {
		c.recordHit(CacheLevelL1)
		return value, true
	}

	// Try L2 cache (Redis)
	if value, found := c.getFromRedis(key); found {
		c.recordHit(CacheLevelL2)
		// Promote to L1 cache
		c.l1Cache.Set(key, value, c.config.L1TTL)
		return value, true
	}

	c.recordMiss()
	return nil, false
}

// Set stores a value in cache (writes to both L1 and L2)
func (c *MultiLevelCache) Set(key string, value interface{}, ttl time.Duration) error {
	// Store in L1 cache
	c.l1Cache.Set(key, value, ttl)

	// Store in L2 cache (Redis)
	return c.setInRedis(key, value, ttl)
}

// Delete removes a value from all cache levels
func (c *MultiLevelCache) Delete(key string) error {
	// Remove from L1
	c.l1Cache.Delete(key)

	// Remove from L2 (Redis)
	return c.redis.Del(c.ctx, key).Err()
}

// GetTranscript retrieves cached transcription results
func (c *MultiLevelCache) GetTranscript(sessionID string) (interface{}, bool) {
	key := fmt.Sprintf(KeyTranscript, sessionID)
	return c.Get(key)
}

// SetTranscript caches transcription results
func (c *MultiLevelCache) SetTranscript(sessionID string, transcript interface{}) error {
	key := fmt.Sprintf(KeyTranscript, sessionID)
	return c.Set(key, transcript, c.config.L2TTL)
}

// GetSummary retrieves cached summary results
func (c *MultiLevelCache) GetSummary(sessionID string) (interface{}, bool) {
	key := fmt.Sprintf(KeySummary, sessionID)
	return c.Get(key)
}

// SetSummary caches summary results
func (c *MultiLevelCache) SetSummary(sessionID string, summary interface{}) error {
	key := fmt.Sprintf(KeySummary, sessionID)
	return c.Set(key, summary, c.config.L2TTL)
}

// GetAudioMetadata retrieves cached audio metadata
func (c *MultiLevelCache) GetAudioMetadata(fileHash string) (interface{}, bool) {
	key := fmt.Sprintf(KeyAudioMetadata, fileHash)
	return c.Get(key)
}

// SetAudioMetadata caches audio metadata
func (c *MultiLevelCache) SetAudioMetadata(fileHash string, metadata interface{}) error {
	key := fmt.Sprintf(KeyAudioMetadata, fileHash)
	return c.Set(key, metadata, 24*time.Hour) // Cache metadata for 24 hours
}

// GetJobResult retrieves cached job results
func (c *MultiLevelCache) GetJobResult(jobID string) (interface{}, bool) {
	key := fmt.Sprintf(KeyJobResult, jobID)
	return c.Get(key)
}

// SetJobResult caches job results
func (c *MultiLevelCache) SetJobResult(jobID string, result interface{}) error {
	key := fmt.Sprintf(KeyJobResult, jobID)
	return c.Set(key, result, c.config.L2TTL)
}

// GetAPIResponse retrieves cached API response
func (c *MultiLevelCache) GetAPIResponse(endpoint, paramsHash string) (interface{}, bool) {
	key := fmt.Sprintf(KeyAPIResponse, endpoint, paramsHash)
	return c.Get(key)
}

// SetAPIResponse caches API response
func (c *MultiLevelCache) SetAPIResponse(endpoint, paramsHash string, response interface{}, ttl time.Duration) error {
	key := fmt.Sprintf(KeyAPIResponse, endpoint, paramsHash)
	return c.Set(key, response, ttl)
}

// GetModelResult retrieves cached model inference results
func (c *MultiLevelCache) GetModelResult(modelName, inputHash string) (interface{}, bool) {
	key := fmt.Sprintf(KeyModelResult, modelName, inputHash)
	return c.Get(key)
}

// SetModelResult caches model inference results
func (c *MultiLevelCache) SetModelResult(modelName, inputHash string, result interface{}) error {
	key := fmt.Sprintf(KeyModelResult, modelName, inputHash)
	return c.Set(key, result, 2*time.Hour) // Cache model results for 2 hours
}

// Invalidate removes all cached data for a session
func (c *MultiLevelCache) Invalidate(sessionID string) error {
	keys := []string{
		fmt.Sprintf(KeyTranscript, sessionID),
		fmt.Sprintf(KeySummary, sessionID),
	}

	for _, key := range keys {
		if err := c.Delete(key); err != nil {
			log.Printf("Failed to invalidate cache key %s: %v", key, err)
		}
	}

	return nil
}

// InvalidatePattern removes all keys matching a pattern
func (c *MultiLevelCache) InvalidatePattern(pattern string) error {
	// Get matching keys from Redis
	keys, err := c.redis.Keys(c.ctx, pattern).Result()
	if err != nil {
		return err
	}

	// Delete from Redis
	if len(keys) > 0 {
		if err := c.redis.Del(c.ctx, keys...).Err(); err != nil {
			return err
		}
	}

	// Delete from L1 cache (simplified pattern matching)
	c.l1Cache.InvalidatePattern(pattern)

	log.Printf("Invalidated %d keys matching pattern: %s", len(keys), pattern)
	return nil
}

// GetStats returns cache statistics
func (c *MultiLevelCache) GetStats() *CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Update L1 stats
	l1Stats := c.l1Cache.GetStats()

	// Calculate overall stats
	totalHits := c.stats.L1Stats.Hits + c.stats.L2Stats.Hits
	totalMisses := c.stats.L1Stats.Misses + c.stats.L2Stats.Misses
	totalRequests := totalHits + totalMisses

	overall := CacheLevelStats{
		Hits:    totalHits,
		Misses:  totalMisses,
		HitRate: 0,
		Size:    c.stats.L1Stats.Size + c.stats.L2Stats.Size,
	}

	if totalRequests > 0 {
		overall.HitRate = float64(totalHits) / float64(totalRequests)
	}

	return &CacheStats{
		L1Stats: l1Stats,
		L2Stats: c.stats.L2Stats,
		Overall: overall,
	}
}

// WarmUp preloads frequently accessed data
func (c *MultiLevelCache) WarmUp(keys []string) error {
	log.Printf("Starting cache warm-up for %d keys", len(keys))

	for i, key := range keys {
		// Check if key exists in Redis and promote to L1
		if value, found := c.getFromRedis(key); found {
			c.l1Cache.Set(key, value, c.config.L1TTL)
		}

		if (i+1)%100 == 0 {
			log.Printf("Warmed up %d/%d keys", i+1, len(keys))
		}
	}

	log.Printf("Cache warm-up completed")
	return nil
}

// Shutdown gracefully shuts down the cache
func (c *MultiLevelCache) Shutdown() error {
	log.Println("Shutting down multi-level cache...")

	c.cancel()

	if err := c.redis.Close(); err != nil {
		return fmt.Errorf("failed to close Redis connection: %w", err)
	}

	c.l1Cache.Clear()

	log.Println("Multi-level cache shutdown complete")
	return nil
}

// Internal methods

// getFromRedis retrieves a value from Redis
func (c *MultiLevelCache) getFromRedis(key string) (interface{}, bool) {
	data, err := c.redis.Get(c.ctx, key).Result()
	if err == redis.Nil {
		return nil, false
	}
	if err != nil {
		log.Printf("Redis get error for key %s: %v", key, err)
		return nil, false
	}

	var value interface{}
	if err := json.Unmarshal([]byte(data), &value); err != nil {
		log.Printf("Failed to unmarshal cached value for key %s: %v", key, err)
		return nil, false
	}

	return value, true
}

// setInRedis stores a value in Redis
func (c *MultiLevelCache) setInRedis(key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	return c.redis.Set(c.ctx, key, data, ttl).Err()
}

// recordHit records a cache hit
func (c *MultiLevelCache) recordHit(level CacheLevel) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch level {
	case CacheLevelL1:
		c.stats.L1Stats.Hits++
	case CacheLevelL2:
		c.stats.L2Stats.Hits++
	}
}

// recordMiss records a cache miss
func (c *MultiLevelCache) recordMiss() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.stats.L1Stats.Misses++
	c.stats.L2Stats.Misses++
}

// statsUpdater updates cache statistics periodically
func (c *MultiLevelCache) statsUpdater() {
	ticker := time.NewTicker(c.config.StatsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.updateRedisStats()
		}
	}
}

// updateRedisStats updates Redis cache statistics
func (c *MultiLevelCache) updateRedisStats() {
	// Get Redis info
	info, err := c.redis.Info(c.ctx, "memory").Result()
	if err != nil {
		log.Printf("Failed to get Redis info: %v", err)
		return
	}

	// Parse memory usage (simplified)
	// In production, you'd parse the info string properly
	c.mu.Lock()
	c.stats.L2Stats.MemoryUsage = 1024 * 1024 // Placeholder: 1MB
	c.mu.Unlock()
}

// cleanupWorker performs periodic cleanup
func (c *MultiLevelCache) cleanupWorker() {
	ticker := time.NewTicker(c.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.l1Cache.Cleanup()
		}
	}
}

// InMemoryCache implementation

// NewInMemoryCache creates a new in-memory cache
func NewInMemoryCache(maxSize int, ttl time.Duration) *InMemoryCache {
	return &InMemoryCache{
		items:   make(map[string]*CacheEntry),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

// Get retrieves a value from L1 cache
func (c *InMemoryCache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.items[key]
	if !exists {
		c.missCount++
		return nil, false
	}

	// Check expiration
	if time.Now().After(entry.Expiration) {
		c.mu.RUnlock()
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()
		c.mu.RLock()
		c.missCount++
		return nil, false
	}

	// Update access time and hit count
	entry.AccessedAt = time.Now()
	entry.HitCount++
	c.hitCount++

	return entry.Value, true
}

// Set stores a value in L1 cache
func (c *InMemoryCache) Set(key string, value interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if we need to evict items
	if len(c.items) >= c.maxSize {
		c.evictLRU()
	}

	entry := &CacheEntry{
		Key:        key,
		Value:      value,
		Expiration: time.Now().Add(ttl),
		CreatedAt:  time.Now(),
		AccessedAt: time.Now(),
		HitCount:   0,
		Level:      CacheLevelL1,
	}

	c.items[key] = entry
}

// Delete removes a value from L1 cache
func (c *InMemoryCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

// Clear removes all items from L1 cache
func (c *InMemoryCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*CacheEntry)
}

// InvalidatePattern removes items matching a pattern
func (c *InMemoryCache) InvalidatePattern(pattern string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Simple pattern matching (in production, use proper regex)
	for key := range c.items {
		if len(pattern) > 0 && pattern[len(pattern)-1] == '*' {
			prefix := pattern[:len(pattern)-1]
			if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
				delete(c.items, key)
			}
		}
	}
}

// Cleanup removes expired items
func (c *InMemoryCache) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, entry := range c.items {
		if now.After(entry.Expiration) {
			delete(c.items, key)
		}
	}
}

// evictLRU evicts the least recently used item
func (c *InMemoryCache) evictLRU() {
	var oldestKey string
	var oldestTime time.Time

	for key, entry := range c.items {
		if oldestKey == "" || entry.AccessedAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.AccessedAt
		}
	}

	if oldestKey != "" {
		delete(c.items, oldestKey)
		c.evictions++
	}
}

// GetStats returns L1 cache statistics
func (c *InMemoryCache) GetStats() CacheLevelStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	totalRequests := c.hitCount + c.missCount
	hitRate := 0.0
	if totalRequests > 0 {
		hitRate = float64(c.hitCount) / float64(totalRequests)
	}

	return CacheLevelStats{
		Hits:      c.hitCount,
		Misses:    c.missCount,
		HitRate:   hitRate,
		Size:      int64(len(c.items)),
		Evictions: c.evictions,
	}
}

// Global cache instance
var globalCache *MultiLevelCache

// InitializeCache initializes the global cache instance
func InitializeCache(config *CacheConfig) error {
	cache, err := NewMultiLevelCache(config)
	if err != nil {
		return err
	}
	globalCache = cache
	return nil
}

// GetCache returns the global cache instance
func GetCache() *MultiLevelCache {
	return globalCache
}

// ShutdownCache shuts down the global cache
func ShutdownCache() error {
	if globalCache != nil {
		return globalCache.Shutdown()
	}
	return nil
}
