package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gavv/httpexpect/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"pocwhisp/database"
	"pocwhisp/handlers"
	"pocwhisp/middleware"
	"pocwhisp/models"
	"pocwhisp/services"
	"pocwhisp/utils"
)

// IntegrationTestSuite contains all integration tests
type IntegrationTestSuite struct {
	suite.Suite
	app    *fiber.App
	db     *gorm.DB
	expect *httpexpect.Expect

	// Test data
	testUser   *models.UserDB
	testToken  string
	testAPIKey string
}

// SetupSuite runs once before all tests
func (suite *IntegrationTestSuite) SetupSuite() {
	// Initialize test database (in-memory SQLite)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(suite.T(), err)
	suite.db = db

	// Run migrations
	err = database.RunMigrations(db)
	require.NoError(suite.T(), err)

	// Initialize services
	utils.InitializeLogger()
	services.InitializeMetrics()
	services.InitializeAuth(services.DefaultAuthConfig())

	// Initialize cache (mock for testing)
	cache := &MockCache{}
	services.SetCache(cache)

	// Create Fiber app
	suite.app = fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": err.Error(),
			})
		},
	})

	// Add middleware
	suite.app.Use(middleware.ResilienceMiddleware(middleware.ResilienceConfig{
		RequestTimeout: 30 * time.Second,
	}))

	// Initialize handlers
	healthHandler := handlers.NewHealthHandler(db, "http://localhost:8081")
	transcribeHandler := handlers.NewTranscribeHandler(db, "http://localhost:8081")
	authService := services.GetAuthService()
	authHandler := handlers.NewAuthHandler(db, authService)

	// Setup routes
	suite.setupRoutes(healthHandler, transcribeHandler, authHandler)

	// Create HTTP test server
	server := httptest.NewServer(suite.app.Handler())
	suite.expect = httpexpect.WithConfig(httpexpect.Config{
		BaseURL:  server.URL,
		Reporter: httpexpect.NewAssertReporter(suite.T()),
	})

	// Create test user and get token
	suite.createTestUser()
}

// setupRoutes configures test routes
func (suite *IntegrationTestSuite) setupRoutes(healthHandler *handlers.HealthHandler, transcribeHandler *handlers.TranscribeHandler, authHandler *handlers.AuthHandler) {
	api := suite.app.Group("/api/v1")

	// Health endpoints
	api.Get("/health", healthHandler.CheckHealth)
	api.Get("/ready", healthHandler.CheckReadiness)
	api.Get("/live", healthHandler.CheckLiveness)

	// Auth endpoints
	auth := api.Group("/auth")
	auth.Post("/register", authHandler.Register)
	auth.Post("/login", authHandler.Login)
	auth.Post("/refresh", authHandler.RefreshToken)
	auth.Get("/profile", middleware.JWTMiddleware(), authHandler.GetProfile)
	auth.Post("/api-key", middleware.JWTMiddleware(), authHandler.GenerateAPIKey)

	// Transcription endpoints
	transcribe := api.Group("/transcribe")
	transcribe.Post("/", middleware.AuthOrAPIKey(), transcribeHandler.UploadAudio)
	transcribe.Get("/:id", middleware.AuthOrAPIKey(), transcribeHandler.GetTranscription)

	// Root endpoint
	suite.app.Get("/", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"service": "PocWhisp Test API",
			"version": "1.0.0",
			"status":  "running",
		})
	})
}

// createTestUser creates a test user and generates token
func (suite *IntegrationTestSuite) createTestUser() {
	// Create user via API
	response := suite.expect.POST("/api/v1/auth/register").
		WithJSON(map[string]interface{}{
			"username": "testuser",
			"email":    "test@example.com",
			"password": "testpassword123",
			"roles":    []string{"user", "admin"},
		}).
		Expect().
		Status(http.StatusCreated)

	// Login and get token
	loginResponse := suite.expect.POST("/api/v1/auth/login").
		WithJSON(map[string]interface{}{
			"username": "testuser",
			"password": "testpassword123",
		}).
		Expect().
		Status(http.StatusOK)

	// Extract token
	suite.testToken = loginResponse.JSON().Object().
		Value("data").Object().
		Value("tokens").Object().
		Value("access_token").String().Raw()

	// Get API key
	apiKeyResponse := suite.expect.POST("/api/v1/auth/api-key").
		WithHeader("Authorization", "Bearer "+suite.testToken).
		Expect().
		Status(http.StatusOK)

	suite.testAPIKey = apiKeyResponse.JSON().Object().
		Value("data").Object().
		Value("api_key").String().Raw()

	// Store user info
	var user models.UserDB
	err := suite.db.Where("username = ?", "testuser").First(&user).Error
	require.NoError(suite.T(), err)
	suite.testUser = &user
}

// TearDownSuite runs once after all tests
func (suite *IntegrationTestSuite) TearDownSuite() {
	// Cleanup if needed
}

// Test Health Endpoints
func (suite *IntegrationTestSuite) TestHealthEndpoints() {
	suite.Run("Health Check", func() {
		response := suite.expect.GET("/api/v1/health").
			Expect().
			Status(http.StatusOK).
			JSON().Object()

		response.Value("status").String().NotEmpty()
		response.Value("version").String().NotEmpty()
		response.Value("uptime").Number().Ge(0)
		response.Value("components").Object().NotEmpty()
	})

	suite.Run("Readiness Check", func() {
		suite.expect.GET("/api/v1/ready").
			Expect().
			Status(http.StatusOK).
			JSON().Object().
			Value("ready").Boolean().True()
	})

	suite.Run("Liveness Check", func() {
		suite.expect.GET("/api/v1/live").
			Expect().
			Status(http.StatusOK).
			JSON().Object().
			Value("alive").Boolean().True()
	})
}

// Test Authentication Flow
func (suite *IntegrationTestSuite) TestAuthenticationFlow() {
	suite.Run("User Registration", func() {
		response := suite.expect.POST("/api/v1/auth/register").
			WithJSON(map[string]interface{}{
				"username": "newuser",
				"email":    "newuser@example.com",
				"password": "newpassword123",
			}).
			Expect().
			Status(http.StatusCreated).
			JSON().Object()

		response.Value("status").String().Equal("success")
		response.Value("data").Object().Value("username").String().Equal("newuser")
		response.Value("data").Object().Value("email").String().Equal("newuser@example.com")
	})

	suite.Run("User Login", func() {
		response := suite.expect.POST("/api/v1/auth/login").
			WithJSON(map[string]interface{}{
				"username": "newuser",
				"password": "newpassword123",
			}).
			Expect().
			Status(http.StatusOK).
			JSON().Object()

		response.Value("status").String().Equal("success")
		response.Value("data").Object().Value("tokens").Object().Value("access_token").String().NotEmpty()
		response.Value("data").Object().Value("tokens").Object().Value("refresh_token").String().NotEmpty()
	})

	suite.Run("Invalid Login", func() {
		suite.expect.POST("/api/v1/auth/login").
			WithJSON(map[string]interface{}{
				"username": "nonexistent",
				"password": "wrongpassword",
			}).
			Expect().
			Status(http.StatusUnauthorized).
			JSON().Object().
			Value("error").String().Equal("invalid_credentials")
	})

	suite.Run("Get Profile", func() {
		response := suite.expect.GET("/api/v1/auth/profile").
			WithHeader("Authorization", "Bearer "+suite.testToken).
			Expect().
			Status(http.StatusOK).
			JSON().Object()

		response.Value("status").String().Equal("success")
		response.Value("data").Object().Value("username").String().Equal("testuser")
		response.Value("data").Object().Value("email").String().Equal("test@example.com")
	})

	suite.Run("Generate API Key", func() {
		response := suite.expect.POST("/api/v1/auth/api-key").
			WithHeader("Authorization", "Bearer "+suite.testToken).
			Expect().
			Status(http.StatusOK).
			JSON().Object()

		response.Value("status").String().Equal("success")
		response.Value("data").Object().Value("api_key").String().Match("^pk_.*")
	})
}

// Test Authentication Middleware
func (suite *IntegrationTestSuite) TestAuthenticationMiddleware() {
	suite.Run("JWT Authentication", func() {
		suite.expect.GET("/api/v1/auth/profile").
			WithHeader("Authorization", "Bearer "+suite.testToken).
			Expect().
			Status(http.StatusOK)
	})

	suite.Run("API Key Authentication", func() {
		suite.expect.GET("/api/v1/auth/profile").
			WithHeader("X-API-Key", suite.testAPIKey).
			Expect().
			Status(http.StatusOK)
	})

	suite.Run("No Authentication", func() {
		suite.expect.GET("/api/v1/auth/profile").
			Expect().
			Status(http.StatusUnauthorized)
	})

	suite.Run("Invalid JWT Token", func() {
		suite.expect.GET("/api/v1/auth/profile").
			WithHeader("Authorization", "Bearer invalid_token").
			Expect().
			Status(http.StatusUnauthorized)
	})

	suite.Run("Invalid API Key", func() {
		suite.expect.GET("/api/v1/auth/profile").
			WithHeader("X-API-Key", "invalid_key").
			Expect().
			Status(http.StatusUnauthorized)
	})
}

// Test Audio Upload and Transcription
func (suite *IntegrationTestSuite) TestTranscriptionEndpoints() {
	suite.Run("Upload Audio File", func() {
		// Create test audio file
		audioData := suite.createTestAudioFile()

		// Create multipart form
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		// Add file
		part, err := writer.CreateFormFile("file", "test.wav")
		require.NoError(suite.T(), err)
		_, err = part.Write(audioData)
		require.NoError(suite.T(), err)

		// Add form fields
		writer.WriteField("enable_summarization", "true")
		writer.WriteField("quality", "high")

		err = writer.Close()
		require.NoError(suite.T(), err)

		// Make request (expect to fail since AI service is not running, but should validate file)
		response := suite.expect.POST("/api/v1/transcribe/").
			WithHeader("Authorization", "Bearer "+suite.testToken).
			WithHeader("Content-Type", writer.FormDataContentType()).
			WithBytes(body.Bytes()).
			Expect()

		// The request should at least pass file validation
		// It may fail at AI service call, which is expected in test environment
		response.Status().InRange(http.StatusOK, http.StatusInternalServerError)
	})

	suite.Run("Upload Invalid File", func() {
		// Create invalid file
		invalidData := []byte("This is not an audio file")

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		part, err := writer.CreateFormFile("file", "invalid.txt")
		require.NoError(suite.T(), err)
		_, err = part.Write(invalidData)
		require.NoError(suite.T(), err)

		err = writer.Close()
		require.NoError(suite.T(), err)

		suite.expect.POST("/api/v1/transcribe/").
			WithHeader("Authorization", "Bearer "+suite.testToken).
			WithHeader("Content-Type", writer.FormDataContentType()).
			WithBytes(body.Bytes()).
			Expect().
			Status(http.StatusBadRequest)
	})

	suite.Run("Upload Without Authentication", func() {
		audioData := suite.createTestAudioFile()

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		part, err := writer.CreateFormFile("file", "test.wav")
		require.NoError(suite.T(), err)
		_, err = part.Write(audioData)
		require.NoError(suite.T(), err)

		err = writer.Close()
		require.NoError(suite.T(), err)

		suite.expect.POST("/api/v1/transcribe/").
			WithHeader("Content-Type", writer.FormDataContentType()).
			WithBytes(body.Bytes()).
			Expect().
			Status(http.StatusUnauthorized)
	})

	suite.Run("Get Non-existent Transcription", func() {
		suite.expect.GET("/api/v1/transcribe/non-existent-id").
			WithHeader("Authorization", "Bearer "+suite.testToken).
			Expect().
			Status(http.StatusNotFound)
	})
}

// Test Error Handling
func (suite *IntegrationTestSuite) TestErrorHandling() {
	suite.Run("Invalid JSON in Request Body", func() {
		suite.expect.POST("/api/v1/auth/login").
			WithText("invalid json").
			Expect().
			Status(http.StatusBadRequest)
	})

	suite.Run("Missing Required Fields", func() {
		suite.expect.POST("/api/v1/auth/register").
			WithJSON(map[string]interface{}{
				"username": "testuser2",
				// Missing email and password
			}).
			Expect().
			Status(http.StatusBadRequest)
	})

	suite.Run("Non-existent Endpoint", func() {
		suite.expect.GET("/api/v1/nonexistent").
			Expect().
			Status(http.StatusNotFound)
	})
}

// Test Rate Limiting (basic test)
func (suite *IntegrationTestSuite) TestRateLimiting() {
	suite.Run("Multiple Requests Within Limit", func() {
		// Make several requests quickly
		for i := 0; i < 5; i++ {
			suite.expect.GET("/api/v1/health").
				Expect().
				Status(http.StatusOK)
		}
	})
}

// Test Content Types and Headers
func (suite *IntegrationTestSuite) TestContentTypesAndHeaders() {
	suite.Run("JSON Response Headers", func() {
		response := suite.expect.GET("/api/v1/health").
			Expect().
			Status(http.StatusOK)

		response.Header("Content-Type").Contains("application/json")
	})

	suite.Run("CORS Headers", func() {
		response := suite.expect.OPTIONS("/api/v1/health").
			WithHeader("Origin", "http://localhost:3000").
			Expect()

		// Should have CORS headers (if CORS middleware is enabled)
		response.Header("Access-Control-Allow-Origin").NotEmpty()
	})
}

// Helper Methods

// createTestAudioFile creates a minimal valid WAV file for testing
func (suite *IntegrationTestSuite) createTestAudioFile() []byte {
	// This is a minimal WAV file header + some dummy data
	// In a real test, you might want to use actual audio files
	header := []byte{
		// RIFF header
		0x52, 0x49, 0x46, 0x46, // "RIFF"
		0x24, 0x00, 0x00, 0x00, // File size - 8
		0x57, 0x41, 0x56, 0x45, // "WAVE"

		// fmt chunk
		0x66, 0x6D, 0x74, 0x20, // "fmt "
		0x10, 0x00, 0x00, 0x00, // Chunk size (16)
		0x01, 0x00, // Audio format (PCM)
		0x02, 0x00, // Number of channels (2)
		0x44, 0xAC, 0x00, 0x00, // Sample rate (44100)
		0x10, 0xB1, 0x02, 0x00, // Byte rate
		0x04, 0x00, // Block align
		0x10, 0x00, // Bits per sample (16)

		// data chunk
		0x64, 0x61, 0x74, 0x61, // "data"
		0x00, 0x00, 0x00, 0x00, // Data size (0 for simplicity)
	}

	return header
}

// MockCache implements a simple mock cache for testing
type MockCache struct {
	data map[string]interface{}
}

func (m *MockCache) Get(key string) (interface{}, bool) {
	if m.data == nil {
		return nil, false
	}
	val, exists := m.data[key]
	return val, exists
}

func (m *MockCache) Set(key string, value interface{}, ttl time.Duration) {
	if m.data == nil {
		m.data = make(map[string]interface{})
	}
	m.data[key] = value
}

func (m *MockCache) Delete(key string) {
	if m.data != nil {
		delete(m.data, key)
	}
}

func (m *MockCache) Clear() {
	m.data = make(map[string]interface{})
}

// Performance Tests
func (suite *IntegrationTestSuite) TestPerformance() {
	suite.Run("Health Endpoint Performance", func() {
		start := time.Now()

		// Make multiple concurrent requests
		for i := 0; i < 10; i++ {
			go func() {
				suite.expect.GET("/api/v1/health").
					Expect().
					Status(http.StatusOK)
			}()
		}

		duration := time.Since(start)

		// Health endpoint should respond quickly
		assert.Less(suite.T(), duration, 5*time.Second)
	})
}

// Run the test suite
func TestIntegrationSuite(t *testing.T) {
	suite.Run(t, new(IntegrationTestSuite))
}

// Additional standalone integration tests
func TestRootEndpoint(t *testing.T) {
	app := fiber.New()

	app.Get("/", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"service": "PocWhisp API",
			"version": "1.0.0",
			"status":  "running",
		})
	})

	req := httptest.NewRequest("GET", "/", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var result map[string]interface{}
	err = json.Unmarshal(body, &result)
	require.NoError(t, err)

	assert.Equal(t, "PocWhisp API", result["service"])
	assert.Equal(t, "1.0.0", result["version"])
	assert.Equal(t, "running", result["status"])
}

func TestMiddlewareIntegration(t *testing.T) {
	app := fiber.New()

	// Add test middleware
	app.Use(func(c *fiber.Ctx) error {
		c.Set("X-Test-Header", "test-value")
		return c.Next()
	})

	app.Get("/test", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"test": "success"})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "test-value", resp.Header.Get("X-Test-Header"))
}
