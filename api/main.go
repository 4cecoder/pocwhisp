package main

import (
	"log"
	"os"
	"os/signal"
	"pocwhisp/database"
	"pocwhisp/handlers"
	"pocwhisp/services"
	"syscall"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

const (
	DefaultPort      = "8080"
	DefaultAIService = "http://localhost:8081"
)

func main() {
	// Initialize database
	db, err := database.Initialize(nil) // Use default config
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close(db)

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

	app.Use(logger.New(logger.Config{
		Format:     "[${time}] ${status} - ${latency} ${method} ${path} ${queryParams} - ${ip} - ${ua}\n",
		TimeFormat: "2006-01-02 15:04:05",
		TimeZone:   "UTC",
	}))

	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowMethods: "GET,POST,HEAD,PUT,DELETE,PATCH,OPTIONS",
		AllowHeaders: "Origin,Content-Type,Accept,Authorization,X-Requested-With",
	}))

	// Get configuration from environment
	port := getEnv("PORT", DefaultPort)
	aiServiceURL := getEnv("AI_SERVICE_URL", DefaultAIService)

	// Initialize AI client
	aiClient := services.NewAIClient(aiServiceURL)

	// Initialize handlers
	healthHandler := handlers.NewHealthHandler(db, aiServiceURL)
	transcribeHandler := handlers.NewTranscribeHandler(db, aiServiceURL)
	wsManager := handlers.NewWebSocketManager(aiClient, db)

	// Routes
	setupRoutes(app, healthHandler, transcribeHandler, wsManager)

	// Graceful shutdown
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)
		<-sigint

		log.Println("Shutting down server...")

		// Cleanup WebSocket connections
		wsManager.Cleanup()

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
func setupRoutes(app *fiber.App, healthHandler *handlers.HealthHandler, transcribeHandler *handlers.TranscribeHandler, wsManager *handlers.WebSocketManager) {
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

	// Root endpoint
	app.Get("/", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"service":   "PocWhisp Audio Transcription API",
			"version":   "1.0.0",
			"status":    "running",
			"timestamp": time.Now(),
			"endpoints": fiber.Map{
				"health":     "/api/v1/health",
				"transcribe": "/api/v1/transcribe",
				"stream":     "/api/v1/stream",
				"metrics":    "/api/v1/metrics",
				"docs":       "/docs", // TODO: Add Swagger docs
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
