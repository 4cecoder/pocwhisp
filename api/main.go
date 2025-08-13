// PocWhisp Audio Transcription API
//
// A high-performance, production-ready API for audio transcription and summarization
// using Whisper Large V3 and Llama models. Features include real-time transcription,
// batch processing, WebSocket streaming, comprehensive security, and enterprise-grade monitoring.
//
// Terms Of Service: https://github.com/fource/pocwhisp
// Schemes: http, https
// Host: localhost:8080
// BasePath: /api/v1
// Version: 1.0.0
// License: MIT https://opensource.org/licenses/MIT
// Contact: PocWhisp Support <support@pocwhisp.com> https://github.com/fource/pocwhisp/issues
//
// Consumes:
// - application/json
// - multipart/form-data
// - application/octet-stream
//
// Produces:
// - application/json
//
// SecurityDefinitions:
// Bearer:
//
//	type: apiKey
//	name: Authorization
//	in: header
//	description: Type "Bearer" followed by a space and JWT token.
//
// ApiKeyAuth:
//
//	type: apiKey
//	name: X-API-Key
//	in: header
//	description: API key for service-to-service authentication
//
// Security:
// - Bearer: []
// - ApiKeyAuth: []
//
//go:generate swag init
package main

import (
	"log"
	"os"
	"os/signal"
	"pocwhisp/database"
	"pocwhisp/docs"
	"pocwhisp/handlers"
	"pocwhisp/middleware"
	"pocwhisp/models"
	"pocwhisp/services"
	"pocwhisp/utils"
	"syscall"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	fiberlogger "github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	fiberSwagger "github.com/swaggo/fiber-swagger"
)

const (
	DefaultPort      = "8080"
	DefaultAIService = "http://localhost:8081"
)

func main() {
	// Initialize logging
	utils.InitializeLogger()
	_ = utils.GetLogger() // Initialize logger

	// Initialize metrics
	services.InitializeMetrics()

	// Initialize circuit breakers
	services.InitializeCircuitBreakers()

	// Initialize degradation manager
	services.InitializeDegradationManager()

	// Initialize health monitoring
	services.InitializeHealthMonitor("1.0.0", getEnv("ENVIRONMENT", "development"))

	// Initialize database
	db, err := database.Initialize(nil) // Use default config
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close(db)

	// Initialize cache
	cacheConfig := services.DefaultCacheConfig()
	redisURL := getEnv("REDIS_URL", cacheConfig.RedisURL)
	cacheConfig.RedisURL = redisURL

	var cache *services.MultiLevelCache
	if err := services.InitializeCache(cacheConfig); err != nil {
		log.Printf("Failed to initialize cache: %v", err)
		log.Println("Continuing without cache...")
	} else {
		log.Printf("Cache initialized with Redis at %s", redisURL)
		cache = services.GetCache()
		defer services.ShutdownCache()
	}

	// Create Fiber app with configuration
	app := fiber.New(fiber.Config{
		AppName:               "PocWhisp Audio Transcription API",
		ServerHeader:          "PocWhisp/1.0",
		DisableStartupMessage: false,
		ErrorHandler:          errorHandler,
		BodyLimit:             1024 * 1024 * 1024, // 1GB for audio files
		ReadTimeout:           30 * time.Second,
		WriteTimeout:          30 * time.Second,
		IdleTimeout:           60 * time.Second,
	})

	// Middleware
	app.Use(recover.New(recover.Config{
		EnableStackTrace: true,
	}))

	app.Use(requestid.New())

	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowMethods: "GET,POST,HEAD,PUT,DELETE,PATCH,OPTIONS",
		AllowHeaders: "Origin,Content-Type,Accept,Authorization,X-Requested-With,X-Request-ID",
	}))

	// Add resilience middleware
	resilienceConfig := middleware.DefaultResilienceConfig()
	app.Use(middleware.ResilienceMiddleware(resilienceConfig))

	// Add metrics middleware
	app.Use(middleware.MetricsMiddleware())

	app.Use(fiberlogger.New(fiberlogger.Config{
		Format:     "[${time}] ${status} - ${latency} ${method} ${path} ${queryParams} - ${ip} - ${ua}\n",
		TimeFormat: "2006-01-02 15:04:05",
		TimeZone:   "UTC",
	}))

	// Get configuration from environment
	port := getEnv("PORT", DefaultPort)
	aiServiceURL := getEnv("AI_SERVICE_URL", DefaultAIService)

	// Initialize AI client
	aiClient := services.NewAIClient(aiServiceURL, 60*time.Second)

	// Register health checkers
	services.RegisterHealthCheckers(db, cache, aiClient, nil)

	// Setup fallback strategies
	services.SetupFallbackStrategies(cache, nil)

	// Initialize batch processor
	batchConfig := services.BatchProcessorConfig{
		MaxConcurrentJobs: 3,
		StorageBasePath:   getEnv("BATCH_STORAGE_PATH", "/tmp/batch_processing"),
		MaxFileSize:       500 * 1024 * 1024, // 500MB
		SupportedFormats:  []string{"wav", "mp3", "flac", "m4a"},
		DefaultConfig: models.BatchJobConfig{
			EnableTranscription:     true,
			EnableSummarization:     true,
			EnableChannelSeparation: true,
			MaxConcurrentFiles:      3,
			MaxConcurrentChunks:     5,
			TranscriptionQuality:    "high",
			SummarizationLength:     "medium",
			OutputFormat:            "json",
			IncludeTimestamps:       true,
			IncludeConfidence:       true,
			UseGPUAcceleration:      true,
			ModelCacheEnabled:       true,
		},
	}
	batchProcessor := services.NewBatchProcessor(db, aiClient, nil, batchConfig)

	// Initialize authentication service
	authService := services.GetAuthService()

	// Initialize handlers
	healthHandler := handlers.NewHealthHandler(db, aiServiceURL)
	transcribeHandler := handlers.NewTranscribeHandler(db, aiServiceURL)
	cacheHandler := handlers.NewCacheHandler()
	batchHandler := handlers.NewBatchHandler(db, batchProcessor)
	authHandler := handlers.NewAuthHandler(db, authService)
	wsManager := handlers.NewWebSocketManager(aiClient, db)

	// Routes
	setupRoutes(app, healthHandler, transcribeHandler, cacheHandler, batchHandler, authHandler, wsManager)

	// Graceful shutdown
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)
		<-sigint

		log.Println("Shutting down server...")

		// Cleanup WebSocket connections
		wsManager.Cleanup()

		// Shutdown batch processor
		if err := batchProcessor.Shutdown(); err != nil {
			log.Printf("Error shutting down batch processor: %v", err)
		}

		if err := app.Shutdown(); err != nil {
			log.Printf("Server forced to shutdown: %v", err)
		}
	}()

	// Start server
	log.Printf("Starting server on port %s", port)
	log.Printf("AI Service URL: %s", aiServiceURL)
	if err := app.Listen(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// setupRoutes configures all application routes
func setupRoutes(app *fiber.App, healthHandler *handlers.HealthHandler, transcribeHandler *handlers.TranscribeHandler, cacheHandler *handlers.CacheHandler, batchHandler *handlers.BatchHandler, authHandler *handlers.AuthHandler, wsManager *handlers.WebSocketManager) {
	// Initialize Swagger docs
	docs.SwaggerInfo.Host = getEnv("SWAGGER_HOST", "localhost:8080")
	docs.SwaggerInfo.BasePath = "/api/v1"
	docs.SwaggerInfo.Schemes = []string{"http", "https"}

	// Swagger documentation endpoint
	app.Get("/docs/*", fiberSwagger.WrapHandler)
	// API version prefix
	api := app.Group("/api/v1")

	// Health endpoints
	api.Get("/health", healthHandler.CheckHealth)
	api.Get("/ready", healthHandler.CheckReadiness)
	api.Get("/live", healthHandler.CheckLiveness)
	api.Get("/metrics", healthHandler.GetMetrics)

	// Transcription endpoints
	api.Post("/transcribe", transcribeHandler.UploadAudio)
	api.Get("/transcribe", transcribeHandler.ListTranscriptions)
	api.Get("/transcribe/:id", transcribeHandler.GetTranscription)

	// WebSocket endpoint for streaming
	api.Use("/stream", func(c *fiber.Ctx) error {
		// Check if request is WebSocket upgrade
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})
	api.Get("/stream", wsManager.HandleWebSocketUpgrade())

	// Authentication endpoints (public)
	auth := api.Group("/auth")
	auth.Post("/register", authHandler.Register)
	auth.Post("/login", authHandler.Login)
	auth.Post("/refresh", authHandler.RefreshToken)
	auth.Post("/validate", authHandler.ValidateToken)

	// Protected authentication endpoints
	auth.Get("/profile", authHandler.GetProfile)
	auth.Put("/profile", authHandler.UpdateProfile)
	auth.Post("/logout", authHandler.Logout)
	auth.Post("/api-key", authHandler.GenerateAPIKey)

	// Cache management endpoints
	cache := api.Group("/cache")
	cache.Get("/stats", cacheHandler.GetStats)
	cache.Get("/health", cacheHandler.GetHealth)
	cache.Post("/warmup", cacheHandler.WarmUp)
	cache.Delete("/key/:key", cacheHandler.InvalidateKey)
	cache.Post("/invalidate/pattern", cacheHandler.InvalidatePattern)
	cache.Delete("/session/:sessionId", cacheHandler.InvalidateSession)

	// Batch processing endpoints
	batch := api.Group("/batch")
	batch.Post("/jobs", batchHandler.CreateBatchJob)
	batch.Get("/jobs", batchHandler.ListBatchJobs)
	batch.Get("/jobs/:jobId", batchHandler.GetBatchJob)
	batch.Post("/jobs/:jobId/start", batchHandler.StartBatchJob)
	batch.Get("/jobs/:jobId/progress", batchHandler.GetBatchJobProgress)
	batch.Post("/jobs/:jobId/cancel", batchHandler.CancelBatchJob)
	batch.Post("/jobs/:jobId/retry", batchHandler.RetryBatchJob)
	batch.Get("/jobs/:jobId/files", batchHandler.GetBatchJobFiles)

	// Root endpoint
	app.Get("/", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"service":     "PocWhisp Audio Transcription API",
			"version":     "1.0.0",
			"status":      "running",
			"timestamp":   time.Now(),
			"description": "High-performance audio transcription and summarization API using Whisper Large V3 and Llama models",
			"endpoints": fiber.Map{
				"health":     "/api/v1/health",
				"transcribe": "/api/v1/transcribe",
				"stream":     "/api/v1/stream",
				"cache":      "/api/v1/cache",
				"batch":      "/api/v1/batch",
				"auth":       "/api/v1/auth",
				"metrics":    "/api/v1/metrics",
				"docs":       "/docs/",
			},
			"documentation": fiber.Map{
				"swagger_ui": "/docs/",
				"openapi":    "/docs/doc.json",
			},
			"features": []string{
				"Real-time audio transcription",
				"Batch processing",
				"WebSocket streaming",
				"JWT authentication",
				"Rate limiting",
				"Comprehensive monitoring",
				"Multi-level caching",
				"Circuit breakers",
				"Speaker diarization",
				"AI-powered summarization",
			},
		})
	})

	// 404 handler
	app.Use("*", func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error":   "not_found",
			"message": "The requested endpoint was not found",
			"path":    c.Path(),
		})
	})
}

// errorHandler handles application errors
func errorHandler(c *fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError

	if e, ok := err.(*fiber.Error); ok {
		code = e.Code
	}

	// Log error
	log.Printf("Error %d: %v - %s %s", code, err, c.Method(), c.Path())

	// Return error response
	return c.Status(code).JSON(fiber.Map{
		"error":     "server_error",
		"message":   err.Error(),
		"code":      code,
		"timestamp": time.Now(),
		"path":      c.Path(),
	})
}

// getEnv gets environment variable with fallback
func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
