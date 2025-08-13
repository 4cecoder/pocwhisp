package handlers

import (
	"fmt"
	"runtime"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

var (
	startTime = time.Now()
	version   = "1.0.0"
)

// HealthHandler handles health check endpoints
type HealthHandler struct {
	db           *gorm.DB
	aiServiceURL string
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(db *gorm.DB, aiServiceURL string) *HealthHandler {
	return &HealthHandler{
		db:           db,
		aiServiceURL: aiServiceURL,
	}
}

// CheckHealth handles the GET /health endpoint
func (h *HealthHandler) CheckHealth(c *fiber.Ctx) error {
	now := time.Now()
	uptime := now.Sub(startTime).Seconds()

	// Get memory stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	memoryUsage := map[string]string{
		"alloc":       formatBytes(m.Alloc),
		"total_alloc": formatBytes(m.TotalAlloc),
		"sys":         formatBytes(m.Sys),
		"heap_alloc":  formatBytes(m.HeapAlloc),
		"heap_sys":    formatBytes(m.HeapSys),
		"num_gc":      fmt.Sprintf("%d", m.NumGC),
	}

	// Check database connectivity
	sqlDB, err := h.db.DB()
	dbStatus := "healthy"
	if err != nil {
		dbStatus = "unhealthy"
	} else {
		if err := sqlDB.Ping(); err != nil {
			dbStatus = "unhealthy"
		}
	}

	// TODO: Check AI service health when implemented
	models := map[string]string{
		"whisper": "not_loaded", // Will be updated when AI service is connected
		"llama":   "not_loaded", // Will be updated when AI service is connected
	}

	dependencies := map[string]string{
		"database":   dbStatus,
		"ai_service": "disconnected", // Will be updated when AI service is connected
		"filesystem": "healthy",
	}

	// Determine overall status
	status := "healthy"
	if dbStatus != "healthy" {
		status = "unhealthy"
	} else if models["whisper"] == "not_loaded" || models["llama"] == "not_loaded" {
		status = "degraded"
	}

	// Get database statistics
	dbStats := map[string]interface{}{
		"connections": "unknown",
	}
	if sqlDB != nil {
		dbStats["connections"] = sqlDB.Stats()
	}

	response := fiber.Map{
		"status":        status,
		"version":       version,
		"models":        models,
		"uptime":        uptime,
		"last_checked":  now,
		"dependencies":  dependencies,
		"gpu_available": false, // Will be updated when AI service is connected
		"memory_usage":  memoryUsage,
		"database":      dbStats,
	}

	// Return appropriate HTTP status based on health
	httpStatus := fiber.StatusOK
	if status == "degraded" {
		httpStatus = fiber.StatusServiceUnavailable
	} else if status == "unhealthy" {
		httpStatus = fiber.StatusServiceUnavailable
	}

	return c.Status(httpStatus).JSON(response)
}

// CheckReadiness handles the GET /ready endpoint
func (h *HealthHandler) CheckReadiness(c *fiber.Ctx) error {
	// Check if all critical components are ready
	ready := true
	components := make(map[string]bool)

	// Check database
	sqlDB, err := h.db.DB()
	if err != nil || sqlDB.Ping() != nil {
		components["database"] = false
		ready = false
	} else {
		components["database"] = true
	}

	// Check filesystem
	components["filesystem"] = true

	// TODO: Check AI service readiness
	components["ai_service"] = false

	response := fiber.Map{
		"ready":      ready,
		"components": components,
		"timestamp":  time.Now(),
	}

	if ready {
		return c.JSON(response)
	}
	return c.Status(fiber.StatusServiceUnavailable).JSON(response)
}

// CheckLiveness handles the GET /live endpoint
func (h *HealthHandler) CheckLiveness(c *fiber.Ctx) error {
	// Simple liveness check - if we can respond, we're alive
	return c.JSON(fiber.Map{
		"alive":     true,
		"timestamp": time.Now(),
		"uptime":    time.Since(startTime).Seconds(),
	})
}

// GetMetrics handles the GET /metrics endpoint (basic metrics)
func (h *HealthHandler) GetMetrics(c *fiber.Ctx) error {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Get database connection pool stats
	var dbStats interface{} = "unavailable"
	if sqlDB, err := h.db.DB(); err == nil {
		dbStats = sqlDB.Stats()
	}

	metrics := fiber.Map{
		"timestamp": time.Now(),
		"uptime":    time.Since(startTime).Seconds(),
		"memory": fiber.Map{
			"alloc":       m.Alloc,
			"total_alloc": m.TotalAlloc,
			"sys":         m.Sys,
			"heap_alloc":  m.HeapAlloc,
			"heap_sys":    m.HeapSys,
			"gc_runs":     m.NumGC,
			"goroutines":  runtime.NumGoroutine(),
		},
		"database": dbStats,
		"runtime": fiber.Map{
			"version":      runtime.Version(),
			"goos":         runtime.GOOS,
			"goarch":       runtime.GOARCH,
			"num_cpu":      runtime.NumCPU(),
			"num_cgo_call": runtime.NumCgoCall(),
		},
	}

	return c.JSON(metrics)
}

// formatBytes converts bytes to human-readable format
func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
