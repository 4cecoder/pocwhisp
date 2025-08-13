package middleware

import (
	"strconv"
	"time"

	"pocwhisp/services"

	"github.com/gofiber/fiber/v2"
)

// MetricsMiddleware creates a middleware for automatic metrics collection
func MetricsMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		startTime := time.Now()

		// Get request size
		requestSize := len(c.Body())

		// Continue processing
		err := c.Next()

		// Record metrics after request completion
		duration := time.Since(startTime)
		statusCode := strconv.Itoa(c.Response().StatusCode())
		responseSize := len(c.Response().Body())

		// Get metrics collector
		metrics := services.GetMetrics()
		if metrics != nil {
			metrics.RecordHTTPRequest(
				c.Method(),
				c.Path(),
				statusCode,
				duration,
				requestSize,
				responseSize,
			)
		}

		return err
	}
}

// PrometheusHandler creates a handler for Prometheus metrics endpoint
func PrometheusHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// This would typically use promhttp.Handler() from prometheus/client_golang
		// For now, return a simple response
		return c.SendString("# Prometheus metrics endpoint\n# Integration with promhttp.Handler() needed")
	}
}
