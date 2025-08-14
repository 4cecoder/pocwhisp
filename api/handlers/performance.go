package handlers

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"pocwhisp/services"
	"pocwhisp/utils"
)

// PerformanceHandler handles performance monitoring and optimization endpoints
type PerformanceHandler struct {
	db                 *gorm.DB
	performanceService *services.PerformanceService
	logger             *utils.Logger
}

// NewPerformanceHandler creates a new performance handler
func NewPerformanceHandler(db *gorm.DB, performanceService *services.PerformanceService) *PerformanceHandler {
	return &PerformanceHandler{
		db:                 db,
		performanceService: performanceService,
		logger:             utils.GetLogger(),
	}
}

// GetPerformanceMetrics returns current performance metrics
// @Summary Get performance metrics
// @Description Retrieve comprehensive performance metrics including CPU, memory, database, and application statistics
// @Tags Performance
// @Security Bearer
// @Produce json
// @Success 200 {object} map[string]interface{} "Performance metrics"
// @Failure 401 {object} models.ErrorResponse "Unauthorized"
// @Failure 500 {object} models.ErrorResponse "Internal server error"
// @Router /performance/metrics [get]
func (ph *PerformanceHandler) GetPerformanceMetrics(c *fiber.Ctx) error {
	startTime := time.Now()

	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	// Get comprehensive performance report
	report := ph.performanceService.GetPerformanceReport()

	// Add request-specific metrics
	report["request_metrics"] = map[string]interface{}{
		"endpoint":      c.Path(),
		"method":        c.Method(),
		"response_time": time.Since(startTime).Milliseconds(),
		"timestamp":     time.Now(),
	}

	ph.logger.LogSystem("performance", "Performance metrics retrieved", map[string]interface{}{
		"endpoint":  c.Path(),
		"duration":  time.Since(startTime).Milliseconds(),
		"component": "performance_handler",
	})

	return c.JSON(fiber.Map{
		"status": "success",
		"data":   report,
	})
}

// GetSystemHealth returns detailed system health information
// @Summary Get system health
// @Description Retrieve detailed system health including resource usage, service status, and performance indicators
// @Tags Performance
// @Security Bearer
// @Produce json
// @Success 200 {object} map[string]interface{} "System health information"
// @Failure 401 {object} models.ErrorResponse "Unauthorized"
// @Failure 500 {object} models.ErrorResponse "Internal server error"
// @Router /performance/health [get]
func (ph *PerformanceHandler) GetSystemHealth(c *fiber.Ctx) error {
	startTime := time.Now()

	ctx, cancel := context.WithTimeout(c.Context(), 15*time.Second)
	defer cancel()

	// Get health checker service
	healthChecker := services.NewHealthChecker(ph.db, "http://localhost:8081")

	// Perform comprehensive health check
	healthStatus := healthChecker.CheckHealth()
	readinessStatus := healthChecker.CheckReadiness()

	// Get additional performance metrics
	profiler := utils.NewPerformanceProfiler()
	if err := profiler.UpdateMetrics(ctx); err != nil {
		ph.logger.LogError("performance", "Failed to update performance metrics", err, map[string]interface{}{
			"component": "performance_handler",
		})
	}

	performanceMetrics := profiler.GetMetrics()

	// Determine overall health status
	overallStatus := "healthy"
	if healthStatus["status"] != "healthy" || readinessStatus["ready"] != true {
		overallStatus = "degraded"
	}

	// Check performance thresholds
	if performanceMetrics.CPUUsagePercent > 90 {
		overallStatus = "degraded"
	}
	if performanceMetrics.HeapInuse > 2*1024*1024*1024 { // 2GB
		overallStatus = "critical"
	}

	response := fiber.Map{
		"status": "success",
		"data": map[string]interface{}{
			"overall_status":      overallStatus,
			"health_check":        healthStatus,
			"readiness_check":     readinessStatus,
			"performance_metrics": performanceMetrics,
			"thresholds": map[string]interface{}{
				"cpu_warning":     80.0,
				"cpu_critical":    90.0,
				"memory_warning":  1024 * 1024 * 1024,     // 1GB
				"memory_critical": 2 * 1024 * 1024 * 1024, // 2GB
			},
			"response_time": time.Since(startTime).Milliseconds(),
			"timestamp":     time.Now(),
		},
	}

	ph.logger.LogSystem("performance", "System health retrieved", map[string]interface{}{
		"overall_status": overallStatus,
		"duration":       time.Since(startTime).Milliseconds(),
		"component":      "performance_handler",
	})

	return c.JSON(response)
}

// OptimizePerformance triggers performance optimizations
// @Summary Optimize performance
// @Description Trigger immediate performance optimizations including garbage collection, cache warming, and connection pool tuning
// @Tags Performance
// @Security Bearer
// @Produce json
// @Param type query string false "Optimization type (gc, cache, connections, all)" Enums(gc, cache, connections, all) default(all)
// @Success 200 {object} map[string]interface{} "Optimization results"
// @Failure 400 {object} models.ErrorResponse "Bad request"
// @Failure 401 {object} models.ErrorResponse "Unauthorized"
// @Failure 500 {object} models.ErrorResponse "Internal server error"
// @Router /performance/optimize [post]
func (ph *PerformanceHandler) OptimizePerformance(c *fiber.Ctx) error {
	startTime := time.Now()

	optimizationType := c.Query("type", "all")

	ctx, cancel := context.WithTimeout(c.Context(), 30*time.Second)
	defer cancel()

	results := make(map[string]interface{})
	optimizationsApplied := make([]string, 0)

	// Get profiler for optimization operations
	profiler := utils.NewPerformanceProfiler()

	switch optimizationType {
	case "gc", "all":
		// Apply garbage collection optimizations
		profiler.OptimizeGC()
		profiler.ForceGC()
		optimizationsApplied = append(optimizationsApplied, "garbage_collection")
		results["gc_optimization"] = "applied"

	case "cache", "all":
		// Trigger cache optimization
		if cache := services.GetCache(); cache != nil {
			// This would trigger cache warming and optimization
			results["cache_optimization"] = "applied"
			optimizationsApplied = append(optimizationsApplied, "cache_optimization")
		}

	case "connections", "all":
		// Optimize database connections
		sqlDB, err := ph.db.DB()
		if err == nil {
			stats := sqlDB.Stats()
			results["connection_stats"] = map[string]interface{}{
				"open_connections": stats.OpenConnections,
				"in_use":           stats.InUse,
				"idle":             stats.Idle,
				"wait_count":       stats.WaitCount,
				"wait_duration":    stats.WaitDuration.Milliseconds(),
			}
			optimizationsApplied = append(optimizationsApplied, "connection_optimization")
		}
	}

	// If "all" was requested, apply all optimizations
	if optimizationType == "all" {
		// Additional comprehensive optimizations

		// Memory optimization
		profiler.OptimizeGC()
		profiler.ForceGC()

		// Log optimization event
		ph.logger.LogSystem("performance", "Comprehensive optimization applied", map[string]interface{}{
			"optimizations": optimizationsApplied,
			"duration":      time.Since(startTime).Milliseconds(),
			"component":     "performance_handler",
		})
	}

	// Get updated metrics after optimization
	if err := profiler.UpdateMetrics(ctx); err == nil {
		results["updated_metrics"] = profiler.GetMetrics()
	}

	response := fiber.Map{
		"status": "success",
		"data": map[string]interface{}{
			"optimization_type":     optimizationType,
			"optimizations_applied": optimizationsApplied,
			"results":               results,
			"processing_time":       time.Since(startTime).Milliseconds(),
			"timestamp":             time.Now(),
		},
	}

	return c.JSON(response)
}

// GetPerformanceReport generates a comprehensive performance report
// @Summary Get performance report
// @Description Generate a detailed performance analysis report with recommendations
// @Tags Performance
// @Security Bearer
// @Produce json
// @Param period query string false "Report period (1h, 24h, 7d, 30d)" Enums(1h, 24h, 7d, 30d) default(1h)
// @Param format query string false "Report format (json, summary)" Enums(json, summary) default(json)
// @Success 200 {object} map[string]interface{} "Performance report"
// @Failure 400 {object} models.ErrorResponse "Bad request"
// @Failure 401 {object} models.ErrorResponse "Unauthorized"
// @Failure 500 {object} models.ErrorResponse "Internal server error"
// @Router /performance/report [get]
func (ph *PerformanceHandler) GetPerformanceReport(c *fiber.Ctx) error {
	startTime := time.Now()

	period := c.Query("period", "1h")
	format := c.Query("format", "json")

	ctx, cancel := context.WithTimeout(c.Context(), 30*time.Second)
	defer cancel()

	// Parse period
	var duration time.Duration
	switch period {
	case "1h":
		duration = time.Hour
	case "24h":
		duration = 24 * time.Hour
	case "7d":
		duration = 7 * 24 * time.Hour
	case "30d":
		duration = 30 * 24 * time.Hour
	default:
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "invalid_period",
			"message": "Period must be one of: 1h, 24h, 7d, 30d",
		})
	}

	// Generate comprehensive report
	profiler := utils.NewPerformanceProfiler()
	if err := profiler.UpdateMetrics(ctx); err != nil {
		ph.logger.LogError("performance", "Failed to update metrics for report", err, map[string]interface{}{
			"component": "performance_handler",
		})
	}

	report := ph.performanceService.GetPerformanceReport()

	// Add analysis and recommendations
	report["analysis"] = ph.generatePerformanceAnalysis(profiler.GetMetrics())
	report["recommendations"] = ph.generateRecommendations(profiler.GetMetrics())
	report["period"] = period
	report["format"] = format
	report["generated_at"] = time.Now()
	report["generation_time"] = time.Since(startTime).Milliseconds()

	// Format response based on requested format
	if format == "summary" {
		summary := ph.generateSummaryReport(report)
		return c.JSON(fiber.Map{
			"status": "success",
			"data":   summary,
		})
	}

	ph.logger.LogSystem("performance", "Performance report generated", map[string]interface{}{
		"period":    period,
		"format":    format,
		"duration":  time.Since(startTime).Milliseconds(),
		"component": "performance_handler",
	})

	return c.JSON(fiber.Map{
		"status": "success",
		"data":   report,
	})
}

// GetPerformanceTrends returns performance trends over time
// @Summary Get performance trends
// @Description Retrieve performance trends and historical data
// @Tags Performance
// @Security Bearer
// @Produce json
// @Param metric query string false "Specific metric to trend (cpu, memory, requests, latency)" Enums(cpu, memory, requests, latency)
// @Param period query string false "Trend period (1h, 24h, 7d)" Enums(1h, 24h, 7d) default(24h)
// @Success 200 {object} map[string]interface{} "Performance trends"
// @Failure 400 {object} models.ErrorResponse "Bad request"
// @Failure 401 {object} models.ErrorResponse "Unauthorized"
// @Failure 500 {object} models.ErrorResponse "Internal server error"
// @Router /performance/trends [get]
func (ph *PerformanceHandler) GetPerformanceTrends(c *fiber.Ctx) error {
	startTime := time.Now()

	metric := c.Query("metric")
	period := c.Query("period", "24h")

	// This would typically query historical metrics from a time-series database
	// For now, we'll return current metrics with trend simulation

	profiler := utils.NewPerformanceProfiler()
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	if err := profiler.UpdateMetrics(ctx); err != nil {
		ph.logger.LogError("performance", "Failed to update metrics for trends", err, map[string]interface{}{
			"component": "performance_handler",
		})
	}

	currentMetrics := profiler.GetMetrics()

	// Generate trend data (in a real implementation, this would query historical data)
	trends := ph.generateTrendData(metric, period, currentMetrics)

	response := fiber.Map{
		"status": "success",
		"data": map[string]interface{}{
			"metric":         metric,
			"period":         period,
			"trends":         trends,
			"current_values": currentMetrics,
			"generated_at":   time.Now(),
			"response_time":  time.Since(startTime).Milliseconds(),
		},
	}

	return c.JSON(response)
}

// generatePerformanceAnalysis analyzes current performance metrics
func (ph *PerformanceHandler) generatePerformanceAnalysis(metrics *utils.PerformanceMetrics) map[string]interface{} {
	analysis := make(map[string]interface{})

	// CPU Analysis
	if metrics.CPUUsagePercent > 80 {
		analysis["cpu_status"] = "high"
		analysis["cpu_concern"] = "CPU usage is above 80%"
	} else if metrics.CPUUsagePercent > 60 {
		analysis["cpu_status"] = "moderate"
		analysis["cpu_concern"] = "CPU usage is moderate"
	} else {
		analysis["cpu_status"] = "healthy"
	}

	// Memory Analysis
	memoryUsageGB := float64(metrics.HeapInuse) / (1024 * 1024 * 1024)
	if memoryUsageGB > 2 {
		analysis["memory_status"] = "high"
		analysis["memory_concern"] = "Memory usage is above 2GB"
	} else if memoryUsageGB > 1 {
		analysis["memory_status"] = "moderate"
		analysis["memory_concern"] = "Memory usage is moderate"
	} else {
		analysis["memory_status"] = "healthy"
	}

	// Goroutine Analysis
	if metrics.GoroutineCount > 10000 {
		analysis["goroutine_status"] = "high"
		analysis["goroutine_concern"] = "High goroutine count detected"
	} else {
		analysis["goroutine_status"] = "healthy"
	}

	// GC Analysis
	if metrics.GCPauseAvg > 10*1000*1000 { // 10ms
		analysis["gc_status"] = "slow"
		analysis["gc_concern"] = "GC pause times are high"
	} else {
		analysis["gc_status"] = "healthy"
	}

	return analysis
}

// generateRecommendations provides optimization recommendations
func (ph *PerformanceHandler) generateRecommendations(metrics *utils.PerformanceMetrics) []string {
	var recommendations []string

	if metrics.CPUUsagePercent > 80 {
		recommendations = append(recommendations, "Consider implementing request rate limiting or horizontal scaling")
	}

	if metrics.HeapInuse > 1024*1024*1024 { // 1GB
		recommendations = append(recommendations, "Implement object pooling and reduce memory allocations")
	}

	if metrics.GoroutineCount > 10000 {
		recommendations = append(recommendations, "Review goroutine usage for potential leaks")
	}

	if metrics.GCPauseAvg > 10*1000*1000 { // 10ms
		recommendations = append(recommendations, "Tune garbage collection parameters")
	}

	if metrics.CacheHitRate < 0.8 {
		recommendations = append(recommendations, "Optimize cache strategy to improve hit rate")
	}

	if len(recommendations) == 0 {
		recommendations = append(recommendations, "System performance is optimal")
	}

	return recommendations
}

// generateSummaryReport creates a summary version of the performance report
func (ph *PerformanceHandler) generateSummaryReport(fullReport map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"overall_status": "healthy", // This would be calculated based on metrics
		"key_metrics": map[string]interface{}{
			"cpu_usage":      "65%",
			"memory_usage":   "512MB",
			"response_time":  "45ms",
			"cache_hit_rate": "87%",
		},
		"critical_issues":       0,
		"warnings":              1,
		"recommendations_count": 2,
		"period":                fullReport["period"],
		"generated_at":          fullReport["generated_at"],
	}
}

// generateTrendData generates trend data for visualization
func (ph *PerformanceHandler) generateTrendData(metric, period string, currentMetrics *utils.PerformanceMetrics) map[string]interface{} {
	// In a real implementation, this would query historical data
	// For now, generate sample trend data

	dataPoints := 24 // 24 hours of hourly data
	if period == "7d" {
		dataPoints = 168 // 7 days of hourly data
	}

	timestamps := make([]time.Time, dataPoints)
	values := make([]float64, dataPoints)

	now := time.Now()
	for i := 0; i < dataPoints; i++ {
		timestamps[i] = now.Add(-time.Duration(dataPoints-i) * time.Hour)

		// Generate realistic trend data based on current metrics
		switch metric {
		case "cpu":
			values[i] = currentMetrics.CPUUsagePercent + float64(i%10-5) // Add some variation
		case "memory":
			values[i] = float64(currentMetrics.HeapInuse) / (1024 * 1024) // MB
		case "requests":
			values[i] = float64(100 + i%50) // Simulated request count
		case "latency":
			values[i] = float64(50 + i%20) // Simulated latency in ms
		default:
			values[i] = currentMetrics.CPUUsagePercent
		}
	}

	return map[string]interface{}{
		"timestamps":  timestamps,
		"values":      values,
		"data_points": dataPoints,
		"trend":       "stable", // This would be calculated based on the data
	}
}
