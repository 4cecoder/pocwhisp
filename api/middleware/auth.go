package middleware

import (
	"strings"
	"time"

	"pocwhisp/models"
	"pocwhisp/services"

	"github.com/gofiber/fiber/v2"
)

// AuthConfig holds configuration for authentication middleware
type AuthConfig struct {
	SecretKey    string
	TokenLookup  string
	AuthScheme   string
	ContextKey   string
	ErrorHandler fiber.ErrorHandler
	Skipper      func(c *fiber.Ctx) bool
}

// DefaultAuthConfig returns default authentication configuration
func DefaultAuthConfig() AuthConfig {
	return AuthConfig{
		TokenLookup:  "header:Authorization,query:token,cookie:token",
		AuthScheme:   "Bearer",
		ContextKey:   "user",
		ErrorHandler: defaultAuthErrorHandler,
		Skipper:      nil,
	}
}

// JWTMiddleware creates JWT authentication middleware
func JWTMiddleware(config ...AuthConfig) fiber.Handler {
	cfg := DefaultAuthConfig()
	if len(config) > 0 {
		cfg = config[0]
	}

	return func(c *fiber.Ctx) error {
		// Skip if skipper function returns true
		if cfg.Skipper != nil && cfg.Skipper(c) {
			return c.Next()
		}

		// Extract token from request
		token := extractToken(c, cfg.TokenLookup, cfg.AuthScheme)
		if token == "" {
			return cfg.ErrorHandler(c, fiber.NewError(fiber.StatusUnauthorized, "Missing or invalid token"))
		}

		// Validate token
		authService := services.GetAuthService()
		claims, err := authService.ValidateAccessToken(token)
		if err != nil {
			return cfg.ErrorHandler(c, fiber.NewError(fiber.StatusUnauthorized, "Invalid token: "+err.Error()))
		}

		// Store user claims in context
		c.Locals(cfg.ContextKey, claims)

		return c.Next()
	}
}

// RequireRole middleware ensures user has required role
func RequireRole(roles ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := getUserClaims(c)
		if claims == nil {
			return fiber.NewError(fiber.StatusUnauthorized, "Authentication required")
		}

		authService := services.GetAuthService()
		hasRole := false
		for _, role := range roles {
			if authService.HasRole(claims, role) {
				hasRole = true
				break
			}
		}

		if !hasRole {
			return fiber.NewError(fiber.StatusForbidden, "Insufficient permissions")
		}

		return c.Next()
	}
}

// RequireScope middleware ensures user has required scope
func RequireScope(scopes ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := getUserClaims(c)
		if claims == nil {
			return fiber.NewError(fiber.StatusUnauthorized, "Authentication required")
		}

		authService := services.GetAuthService()
		hasScope := false
		for _, scope := range scopes {
			if authService.HasScope(claims, scope) {
				hasScope = true
				break
			}
		}

		if !hasScope {
			return fiber.NewError(fiber.StatusForbidden, "Insufficient scope")
		}

		return c.Next()
	}
}

// RequireOperation middleware checks operation-specific permissions
func RequireOperation(operation string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := getUserClaims(c)
		if claims == nil {
			return fiber.NewError(fiber.StatusUnauthorized, "Authentication required")
		}

		authService := services.GetAuthService()

		// Check required roles
		requiredRoles := services.GetRequiredRoles(operation)
		hasRole := false
		for _, role := range requiredRoles {
			if authService.HasRole(claims, role) {
				hasRole = true
				break
			}
		}

		if !hasRole {
			return fiber.NewError(fiber.StatusForbidden, "Insufficient role for operation: "+operation)
		}

		// Check required scopes
		requiredScopes := services.GetRequiredScopes(operation)
		hasScope := false
		for _, scope := range requiredScopes {
			if authService.HasScope(claims, scope) {
				hasScope = true
				break
			}
		}

		if !hasScope {
			return fiber.NewError(fiber.StatusForbidden, "Insufficient scope for operation: "+operation)
		}

		return c.Next()
	}
}

// APIKeyMiddleware validates API keys for service-to-service authentication
func APIKeyMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		apiKey := c.Get("X-API-Key")
		if apiKey == "" {
			apiKey = c.Query("api_key")
		}

		if apiKey == "" {
			return fiber.NewError(fiber.StatusUnauthorized, "API key required")
		}

		authService := services.GetAuthService()
		if !authService.ValidateAPIKey(apiKey) {
			return fiber.NewError(fiber.StatusUnauthorized, "Invalid API key")
		}

		// Store API key info in context (simplified)
		c.Locals("api_key", apiKey)
		c.Locals("auth_type", "api_key")

		return c.Next()
	}
}

// OptionalAuth middleware that allows both authenticated and unauthenticated requests
func OptionalAuth() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Try to extract and validate token
		token := extractToken(c, "header:Authorization,query:token", "Bearer")
		if token != "" {
			authService := services.GetAuthService()
			if claims, err := authService.ValidateAccessToken(token); err == nil {
				c.Locals("user", claims)
			}
		}

		// Continue regardless of authentication status
		return c.Next()
	}
}

// AuthOrAPIKey middleware accepts either JWT or API key authentication
func AuthOrAPIKey() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// First try JWT authentication
		token := extractToken(c, "header:Authorization,query:token", "Bearer")
		if token != "" {
			authService := services.GetAuthService()
			if claims, err := authService.ValidateAccessToken(token); err == nil {
				c.Locals("user", claims)
				c.Locals("auth_type", "jwt")
				return c.Next()
			}
		}

		// Try API key authentication
		apiKey := c.Get("X-API-Key")
		if apiKey == "" {
			apiKey = c.Query("api_key")
		}

		if apiKey != "" {
			authService := services.GetAuthService()
			if authService.ValidateAPIKey(apiKey) {
				c.Locals("api_key", apiKey)
				c.Locals("auth_type", "api_key")
				return c.Next()
			}
		}

		return fiber.NewError(fiber.StatusUnauthorized, "Valid JWT token or API key required")
	}
}

// extractToken extracts token from various sources
func extractToken(c *fiber.Ctx, lookup, authScheme string) string {
	sources := strings.Split(lookup, ",")

	for _, source := range sources {
		parts := strings.Split(strings.TrimSpace(source), ":")
		if len(parts) != 2 {
			continue
		}

		switch parts[0] {
		case "header":
			auth := c.Get(parts[1])
			if auth == "" {
				continue
			}

			if authScheme != "" {
				prefix := authScheme + " "
				if !strings.HasPrefix(auth, prefix) {
					continue
				}
				auth = auth[len(prefix):]
			}

			return auth

		case "query":
			token := c.Query(parts[1])
			if token != "" {
				return token
			}

		case "cookie":
			token := c.Cookies(parts[1])
			if token != "" {
				return token
			}
		}
	}

	return ""
}

// getUserClaims extracts user claims from context
func getUserClaims(c *fiber.Ctx) *services.JWTClaims {
	user := c.Locals("user")
	if claims, ok := user.(*services.JWTClaims); ok {
		return claims
	}
	return nil
}

// GetUserID extracts user ID from context
func GetUserID(c *fiber.Ctx) string {
	claims := getUserClaims(c)
	if claims != nil {
		return claims.UserID
	}
	return ""
}

// GetUserRoles extracts user roles from context
func GetUserRoles(c *fiber.Ctx) []string {
	claims := getUserClaims(c)
	if claims != nil {
		return claims.Roles
	}
	return []string{}
}

// GetUserScopes extracts user scopes from context
func GetUserScopes(c *fiber.Ctx) []string {
	claims := getUserClaims(c)
	if claims != nil {
		return claims.Scope
	}
	return []string{}
}

// IsAuthenticated checks if request is authenticated
func IsAuthenticated(c *fiber.Ctx) bool {
	return getUserClaims(c) != nil || c.Locals("api_key") != nil
}

// GetAuthType returns the authentication type used
func GetAuthType(c *fiber.Ctx) string {
	if authType := c.Locals("auth_type"); authType != nil {
		return authType.(string)
	}
	return "none"
}

// defaultAuthErrorHandler handles authentication errors
func defaultAuthErrorHandler(c *fiber.Ctx, err error) error {
	code := fiber.StatusUnauthorized
	message := "Unauthorized"

	if e, ok := err.(*fiber.Error); ok {
		code = e.Code
		message = e.Message
	}

	return c.Status(code).JSON(models.ErrorResponse{
		Error:     "authentication_error",
		Message:   message,
		Code:      code,
		RequestID: c.Get("X-Request-ID"),
		Timestamp: time.Now(),
	})
}

// AdminOnly middleware restricts access to admin users only
func AdminOnly() fiber.Handler {
	return RequireRole(services.RoleAdmin)
}

// UserOrAdmin middleware allows user or admin access
func UserOrAdmin() fiber.Handler {
	return RequireRole(services.RoleUser, services.RoleAdmin)
}

// ReadWriteAccess middleware requires read and write API access
func ReadWriteAccess() fiber.Handler {
	return RequireScope(services.ScopeAPIRead, services.ScopeAPIWrite)
}

// ReadOnlyAccess middleware requires only read API access
func ReadOnlyAccess() fiber.Handler {
	return RequireScope(services.ScopeAPIRead)
}

// SkipAuth creates a skipper function for specific paths
func SkipAuth(paths ...string) func(c *fiber.Ctx) bool {
	pathMap := make(map[string]bool)
	for _, path := range paths {
		pathMap[path] = true
	}

	return func(c *fiber.Ctx) bool {
		return pathMap[c.Path()]
	}
}

// SkipAuthForMethods creates a skipper function for specific HTTP methods
func SkipAuthForMethods(methods ...string) func(c *fiber.Ctx) bool {
	methodMap := make(map[string]bool)
	for _, method := range methods {
		methodMap[strings.ToUpper(method)] = true
	}

	return func(c *fiber.Ctx) bool {
		return methodMap[c.Method()]
	}
}
