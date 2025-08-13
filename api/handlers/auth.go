package handlers

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"pocwhisp/middleware"
	"pocwhisp/models"
	"pocwhisp/services"
	"pocwhisp/utils"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// AuthHandler handles authentication endpoints
type AuthHandler struct {
	db          *gorm.DB
	authService *services.AuthService
	cache       *services.MultiLevelCache
	logger      *utils.Logger
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(db *gorm.DB, authService *services.AuthService) *AuthHandler {
	return &AuthHandler{
		db:          db,
		authService: authService,
		cache:       services.GetCache(),
		logger:      utils.GetLogger(),
	}
}

// LoginRequest represents a login request
type LoginRequest struct {
	Username string `json:"username" validate:"required"`
	Password string `json:"password" validate:"required"`
	Remember bool   `json:"remember,omitempty"`
}

// RegisterRequest represents a registration request
type RegisterRequest struct {
	Username string   `json:"username" validate:"required,min=3,max=50"`
	Email    string   `json:"email" validate:"required,email"`
	Password string   `json:"password" validate:"required,min=8"`
	Roles    []string `json:"roles,omitempty"`
}

// RefreshTokenRequest represents a refresh token request
type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

// AuthResponse represents authentication response
type AuthResponse struct {
	User      UserResponse        `json:"user"`
	TokenPair *services.TokenPair `json:"tokens"`
	Message   string              `json:"message"`
	ExpiresAt time.Time           `json:"expires_at"`
}

// UserResponse represents user information in responses
type UserResponse struct {
	ID        string     `json:"id"`
	Username  string     `json:"username"`
	Email     string     `json:"email"`
	Roles     []string   `json:"roles"`
	IsActive  bool       `json:"is_active"`
	CreatedAt time.Time  `json:"created_at"`
	LastLogin *time.Time `json:"last_login,omitempty"`
}

// Login authenticates a user and returns JWT tokens
// @Summary User login
// @Description Authenticate a user with username/email and password, returns JWT tokens
// @Tags Authentication
// @Accept json
// @Produce json
// @Param request body LoginRequest true "Login credentials"
// @Success 200 {object} AuthResponse "Login successful"
// @Failure 400 {object} models.ErrorResponse "Invalid request body"
// @Failure 401 {object} models.ErrorResponse "Invalid credentials or account disabled"
// @Failure 500 {object} models.ErrorResponse "Internal server error"
// @Router /auth/login [post]
func (ah *AuthHandler) Login(c *fiber.Ctx) error {
	var req LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Error:     "invalid_request",
			Message:   "Invalid request body",
			Code:      fiber.StatusBadRequest,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Find user by username or email
	var user models.UserDB
	if err := ah.db.Where("username = ? OR email = ?", req.Username, req.Username).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusUnauthorized).JSON(models.ErrorResponse{
				Error:     "invalid_credentials",
				Message:   "Invalid username or password",
				Code:      fiber.StatusUnauthorized,
				RequestID: c.Get("X-Request-ID"),
				Timestamp: time.Now(),
			})
		}

		ah.logError(c, err, "login_user_lookup")
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Error:     "login_failed",
			Message:   "Login failed",
			Code:      fiber.StatusInternalServerError,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Check if user is active
	if !user.IsActive {
		return c.Status(fiber.StatusUnauthorized).JSON(models.ErrorResponse{
			Error:     "account_disabled",
			Message:   "Account is disabled",
			Code:      fiber.StatusUnauthorized,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Verify password
	if !ah.authService.VerifyPassword(req.Password, user.PasswordHash) {
		return c.Status(fiber.StatusUnauthorized).JSON(models.ErrorResponse{
			Error:     "invalid_credentials",
			Message:   "Invalid username or password",
			Code:      fiber.StatusUnauthorized,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Parse user roles
	var roles []string
	if user.Roles != "" {
		json.Unmarshal([]byte(user.Roles), &roles)
	}

	// Create user info for token generation
	userInfo := services.UserInfo{
		ID:       user.ID.String(),
		Username: user.Username,
		Email:    user.Email,
		Roles:    roles,
		IsActive: user.IsActive,
	}

	// Generate token pair
	tokens, err := ah.authService.GenerateTokenPair(userInfo)
	if err != nil {
		ah.logError(c, err, "token_generation")
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Error:     "token_generation_failed",
			Message:   "Failed to generate authentication tokens",
			Code:      fiber.StatusInternalServerError,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Update last login
	now := time.Now()
	user.LastLoginAt = &now
	ah.db.Save(&user)

	// Log successful login
	if ah.logger != nil {
		ah.logger.LogAIService("auth_handler", "user_login", 1, 0, true,
			fmt.Sprintf("User %s logged in successfully", user.Username))
	}

	response := AuthResponse{
		User: UserResponse{
			ID:        user.ID.String(),
			Username:  user.Username,
			Email:     user.Email,
			Roles:     roles,
			IsActive:  user.IsActive,
			CreatedAt: user.CreatedAt,
			LastLogin: user.LastLoginAt,
		},
		TokenPair: tokens,
		Message:   "Login successful",
		ExpiresAt: time.Now().Add(15 * time.Minute), // Access token expiry
	}

	return c.JSON(fiber.Map{
		"status":     "success",
		"data":       response,
		"request_id": c.Get("X-Request-ID"),
	})
}

// Register creates a new user account
// @Summary User registration
// @Description Create a new user account with username, email, and password
// @Tags Authentication
// @Accept json
// @Produce json
// @Param request body RegisterRequest true "Registration details"
// @Success 201 {object} UserResponse "Registration successful"
// @Failure 400 {object} models.ErrorResponse "Invalid request body"
// @Failure 409 {object} models.ErrorResponse "Username or email already exists"
// @Failure 500 {object} models.ErrorResponse "Internal server error"
// @Router /auth/register [post]
func (ah *AuthHandler) Register(c *fiber.Ctx) error {
	var req RegisterRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Error:     "invalid_request",
			Message:   "Invalid request body",
			Code:      fiber.StatusBadRequest,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Check if username already exists
	var existingUser models.UserDB
	if err := ah.db.Where("username = ? OR email = ?", req.Username, req.Email).First(&existingUser).Error; err == nil {
		return c.Status(fiber.StatusConflict).JSON(models.ErrorResponse{
			Error:     "user_exists",
			Message:   "Username or email already exists",
			Code:      fiber.StatusConflict,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Hash password
	passwordHash, err := ah.authService.HashPassword(req.Password)
	if err != nil {
		ah.logError(c, err, "password_hashing")
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Error:     "registration_failed",
			Message:   "Failed to process registration",
			Code:      fiber.StatusInternalServerError,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Set default roles if not provided
	if len(req.Roles) == 0 {
		req.Roles = []string{services.RoleUser}
	}

	rolesJSON, _ := json.Marshal(req.Roles)

	// Create user
	user := models.UserDB{
		Username:     req.Username,
		Email:        req.Email,
		PasswordHash: passwordHash,
		Roles:        string(rolesJSON),
		IsActive:     true,
		APIKey:       ah.authService.GenerateAPIKey(),
	}

	if err := ah.db.Create(&user).Error; err != nil {
		ah.logError(c, err, "user_creation")
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Error:     "registration_failed",
			Message:   "Failed to create user account",
			Code:      fiber.StatusInternalServerError,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Log successful registration
	if ah.logger != nil {
		ah.logger.LogAIService("auth_handler", "user_registration", 1, 0, true,
			fmt.Sprintf("User %s registered successfully", user.Username))
	}

	response := UserResponse{
		ID:        user.ID.String(),
		Username:  user.Username,
		Email:     user.Email,
		Roles:     req.Roles,
		IsActive:  user.IsActive,
		CreatedAt: user.CreatedAt,
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"status":     "success",
		"message":    "User registered successfully",
		"data":       response,
		"request_id": c.Get("X-Request-ID"),
	})
}

// RefreshToken generates new tokens using a refresh token
func (ah *AuthHandler) RefreshToken(c *fiber.Ctx) error {
	var req RefreshTokenRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Error:     "invalid_request",
			Message:   "Invalid request body",
			Code:      fiber.StatusBadRequest,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Validate refresh token and get user ID
	userID, err := ah.authService.ValidateRefreshToken(req.RefreshToken)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(models.ErrorResponse{
			Error:     "invalid_refresh_token",
			Message:   "Invalid or expired refresh token",
			Code:      fiber.StatusUnauthorized,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Get user from database
	var user models.UserDB
	if err := ah.db.Where("id = ?", userID).First(&user).Error; err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(models.ErrorResponse{
			Error:     "user_not_found",
			Message:   "User not found",
			Code:      fiber.StatusUnauthorized,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Check if user is still active
	if !user.IsActive {
		return c.Status(fiber.StatusUnauthorized).JSON(models.ErrorResponse{
			Error:     "account_disabled",
			Message:   "Account is disabled",
			Code:      fiber.StatusUnauthorized,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Parse user roles
	var roles []string
	if user.Roles != "" {
		json.Unmarshal([]byte(user.Roles), &roles)
	}

	// Create user info
	userInfo := services.UserInfo{
		ID:       user.ID.String(),
		Username: user.Username,
		Email:    user.Email,
		Roles:    roles,
		IsActive: user.IsActive,
	}

	// Generate new token pair
	tokens, err := ah.authService.RefreshAccessToken(req.RefreshToken, userInfo)
	if err != nil {
		ah.logError(c, err, "token_refresh")
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Error:     "token_refresh_failed",
			Message:   "Failed to refresh tokens",
			Code:      fiber.StatusInternalServerError,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	return c.JSON(fiber.Map{
		"status":     "success",
		"data":       tokens,
		"message":    "Tokens refreshed successfully",
		"request_id": c.Get("X-Request-ID"),
	})
}

// GetProfile returns the current user's profile
// @Summary Get user profile
// @Description Get the authenticated user's profile information
// @Tags Authentication
// @Accept json
// @Produce json
// @Success 200 {object} UserResponse "User profile"
// @Failure 401 {object} models.ErrorResponse "User not authenticated"
// @Failure 404 {object} models.ErrorResponse "User not found"
// @Failure 500 {object} models.ErrorResponse "Internal server error"
// @Security Bearer
// @Router /auth/profile [get]
func (ah *AuthHandler) GetProfile(c *fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(models.ErrorResponse{
			Error:     "unauthorized",
			Message:   "User not authenticated",
			Code:      fiber.StatusUnauthorized,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Get user from database
	var user models.UserDB
	if err := ah.db.Where("id = ?", userID).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(models.ErrorResponse{
				Error:     "user_not_found",
				Message:   "User not found",
				Code:      fiber.StatusNotFound,
				RequestID: c.Get("X-Request-ID"),
				Timestamp: time.Now(),
			})
		}

		ah.logError(c, err, "get_profile")
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Error:     "profile_retrieval_failed",
			Message:   "Failed to retrieve profile",
			Code:      fiber.StatusInternalServerError,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Parse roles
	var roles []string
	if user.Roles != "" {
		json.Unmarshal([]byte(user.Roles), &roles)
	}

	response := UserResponse{
		ID:        user.ID.String(),
		Username:  user.Username,
		Email:     user.Email,
		Roles:     roles,
		IsActive:  user.IsActive,
		CreatedAt: user.CreatedAt,
		LastLogin: user.LastLoginAt,
	}

	return c.JSON(fiber.Map{
		"status":     "success",
		"data":       response,
		"request_id": c.Get("X-Request-ID"),
	})
}

// UpdateProfile updates the current user's profile
func (ah *AuthHandler) UpdateProfile(c *fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(models.ErrorResponse{
			Error:     "unauthorized",
			Message:   "User not authenticated",
			Code:      fiber.StatusUnauthorized,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	var req struct {
		Email    string `json:"email,omitempty" validate:"omitempty,email"`
		Password string `json:"password,omitempty" validate:"omitempty,min=8"`
	}

	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Error:     "invalid_request",
			Message:   "Invalid request body",
			Code:      fiber.StatusBadRequest,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Get user from database
	var user models.UserDB
	if err := ah.db.Where("id = ?", userID).First(&user).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(models.ErrorResponse{
			Error:     "user_not_found",
			Message:   "User not found",
			Code:      fiber.StatusNotFound,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Update email if provided
	if req.Email != "" && req.Email != user.Email {
		// Check if email is already taken
		var existingUser models.UserDB
		if err := ah.db.Where("email = ? AND id != ?", req.Email, userID).First(&existingUser).Error; err == nil {
			return c.Status(fiber.StatusConflict).JSON(models.ErrorResponse{
				Error:     "email_taken",
				Message:   "Email is already taken",
				Code:      fiber.StatusConflict,
				RequestID: c.Get("X-Request-ID"),
				Timestamp: time.Now(),
			})
		}
		user.Email = req.Email
	}

	// Update password if provided
	if req.Password != "" {
		passwordHash, err := ah.authService.HashPassword(req.Password)
		if err != nil {
			ah.logError(c, err, "password_hashing")
			return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
				Error:     "update_failed",
				Message:   "Failed to update profile",
				Code:      fiber.StatusInternalServerError,
				RequestID: c.Get("X-Request-ID"),
				Timestamp: time.Now(),
			})
		}
		user.PasswordHash = passwordHash
	}

	// Save changes
	if err := ah.db.Save(&user).Error; err != nil {
		ah.logError(c, err, "profile_update")
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Error:     "update_failed",
			Message:   "Failed to update profile",
			Code:      fiber.StatusInternalServerError,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	return c.JSON(fiber.Map{
		"status":     "success",
		"message":    "Profile updated successfully",
		"request_id": c.Get("X-Request-ID"),
	})
}

// Logout handles user logout (in a stateless JWT system, this is mainly for logging)
func (ah *AuthHandler) Logout(c *fiber.Ctx) error {
	userID := middleware.GetUserID(c)

	// Log logout event
	if ah.logger != nil && userID != "" {
		ah.logger.LogAIService("auth_handler", "user_logout", 1, 0, true,
			fmt.Sprintf("User %s logged out", userID))
	}

	return c.JSON(fiber.Map{
		"status":     "success",
		"message":    "Logged out successfully",
		"request_id": c.Get("X-Request-ID"),
	})
}

// GenerateAPIKey generates a new API key for the user
func (ah *AuthHandler) GenerateAPIKey(c *fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(models.ErrorResponse{
			Error:     "unauthorized",
			Message:   "User not authenticated",
			Code:      fiber.StatusUnauthorized,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Generate new API key
	apiKey := ah.authService.GenerateAPIKey()

	// Update user's API key in database
	if err := ah.db.Model(&models.UserDB{}).Where("id = ?", userID).Update("api_key", apiKey).Error; err != nil {
		ah.logError(c, err, "api_key_generation")
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Error:     "api_key_generation_failed",
			Message:   "Failed to generate API key",
			Code:      fiber.StatusInternalServerError,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	return c.JSON(fiber.Map{
		"status": "success",
		"data": fiber.Map{
			"api_key": apiKey,
		},
		"message":    "API key generated successfully",
		"request_id": c.Get("X-Request-ID"),
	})
}

// ValidateToken validates a JWT token
func (ah *AuthHandler) ValidateToken(c *fiber.Ctx) error {
	// Extract token from Authorization header
	auth := c.Get("Authorization")
	if auth == "" {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Error:     "missing_token",
			Message:   "Authorization header is required",
			Code:      fiber.StatusBadRequest,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Remove "Bearer " prefix
	token := strings.TrimPrefix(auth, "Bearer ")
	if token == auth {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Error:     "invalid_token_format",
			Message:   "Token must use Bearer authentication scheme",
			Code:      fiber.StatusBadRequest,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Validate token
	claims, err := ah.authService.ValidateAccessToken(token)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(models.ErrorResponse{
			Error:     "invalid_token",
			Message:   "Token validation failed: " + err.Error(),
			Code:      fiber.StatusUnauthorized,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	return c.JSON(fiber.Map{
		"status": "success",
		"data": fiber.Map{
			"valid":      true,
			"user_id":    claims.UserID,
			"username":   claims.Username,
			"email":      claims.Email,
			"roles":      claims.Roles,
			"scope":      claims.Scope,
			"expires_at": claims.ExpiresAt.Time,
		},
		"message":    "Token is valid",
		"request_id": c.Get("X-Request-ID"),
	})
}

// Helper methods

// logError logs an error with context
func (ah *AuthHandler) logError(c *fiber.Ctx, err error, operation string) {
	if ah.logger != nil {
		ah.logger.LogError(err, operation, utils.LogContext{
			RequestID: c.Get("X-Request-ID"),
			Component: "auth_handler",
			Operation: operation,
		}, map[string]interface{}{
			"method":     c.Method(),
			"path":       c.Path(),
			"user_agent": c.Get("User-Agent"),
			"client_ip":  c.IP(),
		})
	}
}
