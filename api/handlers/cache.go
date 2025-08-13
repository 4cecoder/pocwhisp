package handlers

import (
	"fmt"
	"time"

	"pocwhisp/services"

	"github.com/gofiber/fiber/v2"
)

// CacheHandler handles cache management endpoints
type CacheHandler struct {
	cache *services.MultiLevelCache
}

// NewCacheHandler creates a new cache handler
func NewCacheHandler() *CacheHandler {
	return &CacheHandler{
		cache: services.GetCache(),
	}
}

// GetStats returns cache statistics
func (h *CacheHandler) GetStats(c *fiber.Ctx) error {
	if h.cache == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error":   "cache_unavailable",
			"message": "Cache service is not available",
		})
	}

	stats := h.cache.GetStats()

	return c.JSON(fiber.Map{
		"status":    "success",
		"data":      stats,
		"timestamp": time.Now(),
	})
}

// WarmUp warms up the cache with frequently accessed data
func (h *CacheHandler) WarmUp(c *fiber.Ctx) error {
	if h.cache == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error":   "cache_unavailable",
			"message": "Cache service is not available",
		})
	}

	// Get warm-up keys from request body
	var request struct {
		Keys []string `json:"keys"`
	}

	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "invalid_request",
			"message": "Invalid request body",
		})
	}

	if len(request.Keys) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "missing_keys",
			"message": "No keys provided for warm-up",
		})
	}

	// Perform warm-up
	start := time.Now()
	if err := h.cache.WarmUp(request.Keys); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "warmup_failed",
			"message": fmt.Sprintf("Cache warm-up failed: %v", err),
		})
	}

	return c.JSON(fiber.Map{
		"status":      "success",
		"message":     "Cache warm-up completed",
		"keys_count":  len(request.Keys),
		"duration_ms": time.Since(start).Milliseconds(),
		"timestamp":   time.Now(),
	})
}

// InvalidateKey invalidates a specific cache key
func (h *CacheHandler) InvalidateKey(c *fiber.Ctx) error {
	if h.cache == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error":   "cache_unavailable",
			"message": "Cache service is not available",
		})
	}

	key := c.Params("key")
	if key == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "missing_key",
			"message": "Cache key is required",
		})
	}

	if err := h.cache.Delete(key); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "invalidation_failed",
			"message": fmt.Sprintf("Failed to invalidate key: %v", err),
		})
	}

	return c.JSON(fiber.Map{
		"status":    "success",
		"message":   "Cache key invalidated",
		"key":       key,
		"timestamp": time.Now(),
	})
}

// InvalidatePattern invalidates cache keys matching a pattern
func (h *CacheHandler) InvalidatePattern(c *fiber.Ctx) error {
	if h.cache == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error":   "cache_unavailable",
			"message": "Cache service is not available",
		})
	}

	var request struct {
		Pattern string `json:"pattern"`
	}

	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "invalid_request",
			"message": "Invalid request body",
		})
	}

	if request.Pattern == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "missing_pattern",
			"message": "Pattern is required",
		})
	}

	if err := h.cache.InvalidatePattern(request.Pattern); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "invalidation_failed",
			"message": fmt.Sprintf("Failed to invalidate pattern: %v", err),
		})
	}

	return c.JSON(fiber.Map{
		"status":    "success",
		"message":   "Cache pattern invalidated",
		"pattern":   request.Pattern,
		"timestamp": time.Now(),
	})
}

// InvalidateSession invalidates all cache entries for a session
func (h *CacheHandler) InvalidateSession(c *fiber.Ctx) error {
	if h.cache == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error":   "cache_unavailable",
			"message": "Cache service is not available",
		})
	}

	sessionID := c.Params("sessionId")
	if sessionID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "missing_session_id",
			"message": "Session ID is required",
		})
	}

	if err := h.cache.Invalidate(sessionID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "invalidation_failed",
			"message": fmt.Sprintf("Failed to invalidate session: %v", err),
		})
	}

	return c.JSON(fiber.Map{
		"status":     "success",
		"message":    "Session cache invalidated",
		"session_id": sessionID,
		"timestamp":  time.Now(),
	})
}

// GetHealth returns cache health status
func (h *CacheHandler) GetHealth(c *fiber.Ctx) error {
	if h.cache == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"status":    "unhealthy",
			"error":     "cache_unavailable",
			"message":   "Cache service is not available",
			"timestamp": time.Now(),
		})
	}

	// Test cache connectivity
	testKey := "health_check_" + fmt.Sprintf("%d", time.Now().Unix())
	testValue := "ok"

	// Try to set and get a test value
	if err := h.cache.Set(testKey, testValue, 10*time.Second); err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"status":    "unhealthy",
			"error":     "cache_write_failed",
			"message":   fmt.Sprintf("Failed to write to cache: %v", err),
			"timestamp": time.Now(),
		})
	}

	if value, found := h.cache.Get(testKey); !found || value != testValue {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"status":    "unhealthy",
			"error":     "cache_read_failed",
			"message":   "Failed to read from cache",
			"timestamp": time.Now(),
		})
	}

	// Clean up test key
	h.cache.Delete(testKey)

	// Get stats for health info
	stats := h.cache.GetStats()

	return c.JSON(fiber.Map{
		"status":           "healthy",
		"message":          "Cache is operational",
		"l1_hit_rate":      stats.L1Stats.HitRate,
		"l2_hit_rate":      stats.L2Stats.HitRate,
		"overall_hit_rate": stats.Overall.HitRate,
		"timestamp":        time.Now(),
	})
}
