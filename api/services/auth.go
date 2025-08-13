package services

import (
	"crypto/rand"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// JWTClaims represents the claims in a JWT token
type JWTClaims struct {
	UserID   string   `json:"user_id"`
	Username string   `json:"username"`
	Email    string   `json:"email"`
	Roles    []string `json:"roles"`
	Scope    []string `json:"scope"`
	jwt.RegisteredClaims
}

// AuthService handles authentication and authorization
type AuthService struct {
	jwtSecret       []byte
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
	issuer          string
	audience        string
}

// AuthConfig holds configuration for the auth service
type AuthConfig struct {
	JWTSecret       string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	Issuer          string
	Audience        string
}

// TokenPair represents an access and refresh token pair
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

// UserInfo represents user information for authentication
type UserInfo struct {
	ID       string   `json:"id"`
	Username string   `json:"username"`
	Email    string   `json:"email"`
	Password string   `json:"password,omitempty"` // Only used during creation
	Roles    []string `json:"roles"`
	IsActive bool     `json:"is_active"`
	APIKey   string   `json:"api_key,omitempty"`
}

// DefaultAuthConfig returns default authentication configuration
func DefaultAuthConfig() AuthConfig {
	return AuthConfig{
		JWTSecret:       generateRandomSecret(32),
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 7 * 24 * time.Hour, // 7 days
		Issuer:          "pocwhisp-api",
		Audience:        "pocwhisp-clients",
	}
}

// NewAuthService creates a new authentication service
func NewAuthService(config AuthConfig) *AuthService {
	return &AuthService{
		jwtSecret:       []byte(config.JWTSecret),
		accessTokenTTL:  config.AccessTokenTTL,
		refreshTokenTTL: config.RefreshTokenTTL,
		issuer:          config.Issuer,
		audience:        config.Audience,
	}
}

// GenerateTokenPair generates an access and refresh token pair for a user
func (as *AuthService) GenerateTokenPair(user UserInfo) (*TokenPair, error) {
	now := time.Now()

	// Create access token claims
	accessClaims := &JWTClaims{
		UserID:   user.ID,
		Username: user.Username,
		Email:    user.Email,
		Roles:    user.Roles,
		Scope:    []string{"api:read", "api:write"},
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    as.issuer,
			Audience:  jwt.ClaimStrings{as.audience},
			Subject:   user.ID,
			ExpiresAt: jwt.NewNumericDate(now.Add(as.accessTokenTTL)),
			NotBefore: jwt.NewNumericDate(now),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        fmt.Sprintf("access_%d", now.Unix()),
		},
	}

	// Create refresh token claims (simpler, longer-lived)
	refreshClaims := &jwt.RegisteredClaims{
		Issuer:    as.issuer,
		Audience:  jwt.ClaimStrings{as.audience},
		Subject:   user.ID,
		ExpiresAt: jwt.NewNumericDate(now.Add(as.refreshTokenTTL)),
		NotBefore: jwt.NewNumericDate(now),
		IssuedAt:  jwt.NewNumericDate(now),
		ID:        fmt.Sprintf("refresh_%d", now.Unix()),
	}

	// Generate access token
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessTokenString, err := accessToken.SignedString(as.jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to sign access token: %w", err)
	}

	// Generate refresh token
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshTokenString, err := refreshToken.SignedString(as.jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to sign refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessTokenString,
		RefreshToken: refreshTokenString,
		TokenType:    "Bearer",
		ExpiresIn:    int64(as.accessTokenTTL.Seconds()),
	}, nil
}

// ValidateAccessToken validates an access token and returns the claims
func (as *AuthService) ValidateAccessToken(tokenString string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return as.jwtSecret, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	if claims, ok := token.Claims.(*JWTClaims); ok && token.Valid {
		// Additional validation
		if claims.Issuer != as.issuer {
			return nil, fmt.Errorf("invalid issuer")
		}

		// Check audience
		validAudience := false
		for _, aud := range claims.Audience {
			if aud == as.audience {
				validAudience = true
				break
			}
		}
		if !validAudience {
			return nil, fmt.Errorf("invalid audience")
		}

		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}

// ValidateRefreshToken validates a refresh token and returns the subject
func (as *AuthService) ValidateRefreshToken(tokenString string) (string, error) {
	token, err := jwt.ParseWithClaims(tokenString, &jwt.RegisteredClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return as.jwtSecret, nil
	})

	if err != nil {
		return "", fmt.Errorf("failed to parse refresh token: %w", err)
	}

	if claims, ok := token.Claims.(*jwt.RegisteredClaims); ok && token.Valid {
		if claims.Issuer != as.issuer {
			return "", fmt.Errorf("invalid issuer")
		}

		// Check audience
		validAudience := false
		for _, aud := range claims.Audience {
			if aud == as.audience {
				validAudience = true
				break
			}
		}
		if !validAudience {
			return "", fmt.Errorf("invalid audience")
		}

		return claims.Subject, nil
	}

	return "", fmt.Errorf("invalid refresh token")
}

// RefreshAccessToken generates a new access token using a valid refresh token
func (as *AuthService) RefreshAccessToken(refreshToken string, user UserInfo) (*TokenPair, error) {
	// Validate refresh token
	userID, err := as.ValidateRefreshToken(refreshToken)
	if err != nil {
		return nil, fmt.Errorf("invalid refresh token: %w", err)
	}

	// Verify user ID matches
	if userID != user.ID {
		return nil, fmt.Errorf("user ID mismatch")
	}

	// Generate new token pair
	return as.GenerateTokenPair(user)
}

// HashPassword hashes a password using bcrypt
func (as *AuthService) HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}
	return string(hash), nil
}

// VerifyPassword verifies a password against its hash
func (as *AuthService) VerifyPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// GenerateAPIKey generates a secure API key
func (as *AuthService) GenerateAPIKey() string {
	return fmt.Sprintf("pk_%s", generateRandomSecret(32))
}

// ValidateAPIKey validates an API key format
func (as *AuthService) ValidateAPIKey(apiKey string) bool {
	// Basic validation - in production, you'd check against database
	return len(apiKey) > 10 && (apiKey[:3] == "pk_" || apiKey[:3] == "sk_")
}

// HasRole checks if a user has a specific role
func (as *AuthService) HasRole(claims *JWTClaims, role string) bool {
	for _, r := range claims.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// HasScope checks if a user has a specific scope
func (as *AuthService) HasScope(claims *JWTClaims, scope string) bool {
	for _, s := range claims.Scope {
		if s == scope {
			return true
		}
	}
	return false
}

// generateRandomSecret generates a random secret string
func generateRandomSecret(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to time-based secret
		return fmt.Sprintf("secret_%d", time.Now().UnixNano())
	}

	// Convert to hex string
	secret := fmt.Sprintf("%x", bytes)
	if len(secret) > length {
		secret = secret[:length]
	}
	return secret
}

// RoleDefinitions for the system
var (
	RoleAdmin     = "admin"
	RoleUser      = "user"
	RoleBatchUser = "batch_user"
	RoleAPIUser   = "api_user"
	RoleReadOnly  = "read_only"
)

// ScopeDefinitions for the system
var (
	ScopeAPIRead    = "api:read"
	ScopeAPIWrite   = "api:write"
	ScopeTranscribe = "transcribe"
	ScopeBatch      = "batch"
	ScopeWebSocket  = "websocket"
	ScopeAdmin      = "admin"
	ScopeCache      = "cache"
	ScopeMetrics    = "metrics"
)

// GetRequiredRoles returns required roles for different operations
func GetRequiredRoles(operation string) []string {
	roleMap := map[string][]string{
		"transcribe":   {RoleUser, RoleAPIUser, RoleAdmin},
		"batch":        {RoleBatchUser, RoleAdmin},
		"websocket":    {RoleUser, RoleAPIUser, RoleAdmin},
		"admin":        {RoleAdmin},
		"cache_manage": {RoleAdmin},
		"metrics":      {RoleAdmin, RoleReadOnly},
		"health":       {RoleUser, RoleAPIUser, RoleAdmin, RoleReadOnly},
	}

	if roles, exists := roleMap[operation]; exists {
		return roles
	}

	// Default: require user role
	return []string{RoleUser}
}

// GetRequiredScopes returns required scopes for different operations
func GetRequiredScopes(operation string) []string {
	scopeMap := map[string][]string{
		"transcribe":  {ScopeAPIRead, ScopeAPIWrite, ScopeTranscribe},
		"batch":       {ScopeAPIRead, ScopeAPIWrite, ScopeBatch},
		"websocket":   {ScopeAPIRead, ScopeAPIWrite, ScopeWebSocket},
		"cache_read":  {ScopeAPIRead, ScopeCache},
		"cache_write": {ScopeAPIWrite, ScopeCache},
		"metrics":     {ScopeAPIRead, ScopeMetrics},
		"admin":       {ScopeAdmin},
	}

	if scopes, exists := scopeMap[operation]; exists {
		return scopes
	}

	// Default: require API read
	return []string{ScopeAPIRead}
}

// Global auth service instance
var globalAuthService *AuthService

// InitializeAuth initializes the global auth service
func InitializeAuth(config AuthConfig) {
	globalAuthService = NewAuthService(config)
}

// GetAuthService returns the global auth service
func GetAuthService() *AuthService {
	if globalAuthService == nil {
		// Initialize with default config if not already initialized
		InitializeAuth(DefaultAuthConfig())
	}
	return globalAuthService
}
