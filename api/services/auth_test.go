package services

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthService(t *testing.T) {
	t.Run("NewAuthService", func(t *testing.T) {
		config := AuthConfig{
			JWTSecret:       "test-secret-key",
			AccessTokenTTL:  15 * time.Minute,
			RefreshTokenTTL: 7 * 24 * time.Hour,
			Issuer:          "test-issuer",
			Audience:        "test-audience",
		}

		authService := NewAuthService(config)

		assert.NotNil(t, authService)
		assert.Equal(t, []byte(config.JWTSecret), authService.jwtSecret)
		assert.Equal(t, config.AccessTokenTTL, authService.accessTokenTTL)
		assert.Equal(t, config.RefreshTokenTTL, authService.refreshTokenTTL)
		assert.Equal(t, config.Issuer, authService.issuer)
		assert.Equal(t, config.Audience, authService.audience)
	})

	t.Run("DefaultAuthConfig", func(t *testing.T) {
		config := DefaultAuthConfig()

		assert.NotEmpty(t, config.JWTSecret)
		assert.Equal(t, 15*time.Minute, config.AccessTokenTTL)
		assert.Equal(t, 7*24*time.Hour, config.RefreshTokenTTL)
		assert.Equal(t, "pocwhisp-api", config.Issuer)
		assert.Equal(t, "pocwhisp-clients", config.Audience)
	})
}

func TestTokenGeneration(t *testing.T) {
	authService := NewAuthService(AuthConfig{
		JWTSecret:       "test-secret",
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 7 * 24 * time.Hour,
		Issuer:          "test-issuer",
		Audience:        "test-audience",
	})

	testUser := UserInfo{
		ID:       "user-123",
		Username: "testuser",
		Email:    "test@example.com",
		Roles:    []string{"user", "admin"},
		IsActive: true,
	}

	t.Run("GenerateTokenPair", func(t *testing.T) {
		tokenPair, err := authService.GenerateTokenPair(testUser)
		require.NoError(t, err)
		require.NotNil(t, tokenPair)

		assert.NotEmpty(t, tokenPair.AccessToken)
		assert.NotEmpty(t, tokenPair.RefreshToken)
		assert.Equal(t, "Bearer", tokenPair.TokenType)
		assert.Equal(t, int64(900), tokenPair.ExpiresIn) // 15 minutes
	})

	t.Run("ValidateAccessToken", func(t *testing.T) {
		tokenPair, err := authService.GenerateTokenPair(testUser)
		require.NoError(t, err)

		claims, err := authService.ValidateAccessToken(tokenPair.AccessToken)
		require.NoError(t, err)
		require.NotNil(t, claims)

		assert.Equal(t, testUser.ID, claims.UserID)
		assert.Equal(t, testUser.Username, claims.Username)
		assert.Equal(t, testUser.Email, claims.Email)
		assert.Equal(t, testUser.Roles, claims.Roles)
		assert.Contains(t, claims.Scope, "api:read")
		assert.Contains(t, claims.Scope, "api:write")
	})

	t.Run("ValidateRefreshToken", func(t *testing.T) {
		tokenPair, err := authService.GenerateTokenPair(testUser)
		require.NoError(t, err)

		userID, err := authService.ValidateRefreshToken(tokenPair.RefreshToken)
		require.NoError(t, err)

		assert.Equal(t, testUser.ID, userID)
	})

	t.Run("RefreshAccessToken", func(t *testing.T) {
		tokenPair, err := authService.GenerateTokenPair(testUser)
		require.NoError(t, err)

		newTokenPair, err := authService.RefreshAccessToken(tokenPair.RefreshToken, testUser)
		require.NoError(t, err)
		require.NotNil(t, newTokenPair)

		assert.NotEmpty(t, newTokenPair.AccessToken)
		assert.NotEmpty(t, newTokenPair.RefreshToken)
		assert.NotEqual(t, tokenPair.AccessToken, newTokenPair.AccessToken)
	})
}

func TestTokenValidation(t *testing.T) {
	authService := NewAuthService(AuthConfig{
		JWTSecret:       "test-secret",
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 7 * 24 * time.Hour,
		Issuer:          "test-issuer",
		Audience:        "test-audience",
	})

	testUser := UserInfo{
		ID:       "user-123",
		Username: "testuser",
		Email:    "test@example.com",
		Roles:    []string{"user"},
		IsActive: true,
	}

	t.Run("InvalidToken", func(t *testing.T) {
		_, err := authService.ValidateAccessToken("invalid-token")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse token")
	})

	t.Run("ExpiredToken", func(t *testing.T) {
		// Create a service with very short TTL
		shortTTLService := NewAuthService(AuthConfig{
			JWTSecret:       "test-secret",
			AccessTokenTTL:  1 * time.Millisecond,
			RefreshTokenTTL: 7 * 24 * time.Hour,
			Issuer:          "test-issuer",
			Audience:        "test-audience",
		})

		tokenPair, err := shortTTLService.GenerateTokenPair(testUser)
		require.NoError(t, err)

		// Wait for token to expire
		time.Sleep(10 * time.Millisecond)

		_, err = shortTTLService.ValidateAccessToken(tokenPair.AccessToken)
		assert.Error(t, err)
	})

	t.Run("WrongSecret", func(t *testing.T) {
		// Generate token with one service
		tokenPair, err := authService.GenerateTokenPair(testUser)
		require.NoError(t, err)

		// Try to validate with different service (different secret)
		otherService := NewAuthService(AuthConfig{
			JWTSecret:       "different-secret",
			AccessTokenTTL:  15 * time.Minute,
			RefreshTokenTTL: 7 * 24 * time.Hour,
			Issuer:          "test-issuer",
			Audience:        "test-audience",
		})

		_, err = otherService.ValidateAccessToken(tokenPair.AccessToken)
		assert.Error(t, err)
	})

	t.Run("WrongIssuer", func(t *testing.T) {
		tokenPair, err := authService.GenerateTokenPair(testUser)
		require.NoError(t, err)

		// Create service with different issuer
		differentIssuerService := NewAuthService(AuthConfig{
			JWTSecret:       "test-secret",
			AccessTokenTTL:  15 * time.Minute,
			RefreshTokenTTL: 7 * 24 * time.Hour,
			Issuer:          "different-issuer",
			Audience:        "test-audience",
		})

		_, err = differentIssuerService.ValidateAccessToken(tokenPair.AccessToken)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid issuer")
	})

	t.Run("WrongAudience", func(t *testing.T) {
		tokenPair, err := authService.GenerateTokenPair(testUser)
		require.NoError(t, err)

		// Create service with different audience
		differentAudienceService := NewAuthService(AuthConfig{
			JWTSecret:       "test-secret",
			AccessTokenTTL:  15 * time.Minute,
			RefreshTokenTTL: 7 * 24 * time.Hour,
			Issuer:          "test-issuer",
			Audience:        "different-audience",
		})

		_, err = differentAudienceService.ValidateAccessToken(tokenPair.AccessToken)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid audience")
	})
}

func TestPasswordHandling(t *testing.T) {
	authService := NewAuthService(DefaultAuthConfig())

	t.Run("HashPassword", func(t *testing.T) {
		password := "testpassword123"

		hash, err := authService.HashPassword(password)
		require.NoError(t, err)

		assert.NotEmpty(t, hash)
		assert.NotEqual(t, password, hash)
		assert.True(t, len(hash) > 50) // bcrypt hashes are typically 60 characters
	})

	t.Run("VerifyPassword", func(t *testing.T) {
		password := "testpassword123"

		hash, err := authService.HashPassword(password)
		require.NoError(t, err)

		// Correct password
		assert.True(t, authService.VerifyPassword(password, hash))

		// Wrong password
		assert.False(t, authService.VerifyPassword("wrongpassword", hash))

		// Empty password
		assert.False(t, authService.VerifyPassword("", hash))
	})

	t.Run("HashPassword_EmptyPassword", func(t *testing.T) {
		_, err := authService.HashPassword("")
		assert.NoError(t, err) // bcrypt can hash empty strings
	})
}

func TestAPIKeyHandling(t *testing.T) {
	authService := NewAuthService(DefaultAuthConfig())

	t.Run("GenerateAPIKey", func(t *testing.T) {
		apiKey := authService.GenerateAPIKey()

		assert.NotEmpty(t, apiKey)
		assert.True(t, len(apiKey) > 10)
		assert.True(t, strings.HasPrefix(apiKey, "pk_"))

		// Generate multiple keys to ensure uniqueness
		keys := make(map[string]bool)
		for i := 0; i < 10; i++ {
			key := authService.GenerateAPIKey()
			assert.False(t, keys[key], "Generated duplicate API key")
			keys[key] = true
		}
	})

	t.Run("ValidateAPIKey", func(t *testing.T) {
		// Valid API key formats
		assert.True(t, authService.ValidateAPIKey("pk_1234567890abcdef"))
		assert.True(t, authService.ValidateAPIKey("sk_1234567890abcdef"))

		// Invalid API key formats
		assert.False(t, authService.ValidateAPIKey("invalid"))
		assert.False(t, authService.ValidateAPIKey(""))
		assert.False(t, authService.ValidateAPIKey("pk_"))
		assert.False(t, authService.ValidateAPIKey("wrong_prefix_key"))
	})
}

func TestRoleAndScopeHandling(t *testing.T) {
	authService := NewAuthService(DefaultAuthConfig())

	testUser := UserInfo{
		ID:       "user-123",
		Username: "testuser",
		Email:    "test@example.com",
		Roles:    []string{"user", "admin"},
		IsActive: true,
	}

	t.Run("HasRole", func(t *testing.T) {
		tokenPair, err := authService.GenerateTokenPair(testUser)
		require.NoError(t, err)

		claims, err := authService.ValidateAccessToken(tokenPair.AccessToken)
		require.NoError(t, err)

		assert.True(t, authService.HasRole(claims, "user"))
		assert.True(t, authService.HasRole(claims, "admin"))
		assert.False(t, authService.HasRole(claims, "superuser"))
		assert.False(t, authService.HasRole(claims, "nonexistent"))
	})

	t.Run("HasScope", func(t *testing.T) {
		tokenPair, err := authService.GenerateTokenPair(testUser)
		require.NoError(t, err)

		claims, err := authService.ValidateAccessToken(tokenPair.AccessToken)
		require.NoError(t, err)

		assert.True(t, authService.HasScope(claims, "api:read"))
		assert.True(t, authService.HasScope(claims, "api:write"))
		assert.False(t, authService.HasScope(claims, "nonexistent:scope"))
	})
}

func TestRoleAndScopeDefinitions(t *testing.T) {
	t.Run("RequiredRoles", func(t *testing.T) {
		// Test known operations
		transcribeRoles := GetRequiredRoles("transcribe")
		assert.Contains(t, transcribeRoles, RoleUser)
		assert.Contains(t, transcribeRoles, RoleAdmin)

		batchRoles := GetRequiredRoles("batch")
		assert.Contains(t, batchRoles, RoleBatchUser)
		assert.Contains(t, batchRoles, RoleAdmin)

		adminRoles := GetRequiredRoles("admin")
		assert.Contains(t, adminRoles, RoleAdmin)
		assert.Len(t, adminRoles, 1)

		// Test unknown operation (should return default)
		unknownRoles := GetRequiredRoles("unknown_operation")
		assert.Contains(t, unknownRoles, RoleUser)
	})

	t.Run("RequiredScopes", func(t *testing.T) {
		// Test known operations
		transcribeScopes := GetRequiredScopes("transcribe")
		assert.Contains(t, transcribeScopes, ScopeAPIRead)
		assert.Contains(t, transcribeScopes, ScopeTranscribe)

		batchScopes := GetRequiredScopes("batch")
		assert.Contains(t, batchScopes, ScopeBatch)

		adminScopes := GetRequiredScopes("admin")
		assert.Contains(t, adminScopes, ScopeAdmin)
		assert.Len(t, adminScopes, 1)

		// Test unknown operation (should return default)
		unknownScopes := GetRequiredScopes("unknown_operation")
		assert.Contains(t, unknownScopes, ScopeAPIRead)
	})
}

func TestTokenRefresh(t *testing.T) {
	authService := NewAuthService(AuthConfig{
		JWTSecret:       "test-secret",
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 7 * 24 * time.Hour,
		Issuer:          "test-issuer",
		Audience:        "test-audience",
	})

	testUser := UserInfo{
		ID:       "user-123",
		Username: "testuser",
		Email:    "test@example.com",
		Roles:    []string{"user"},
		IsActive: true,
	}

	t.Run("ValidRefresh", func(t *testing.T) {
		tokenPair, err := authService.GenerateTokenPair(testUser)
		require.NoError(t, err)

		newTokenPair, err := authService.RefreshAccessToken(tokenPair.RefreshToken, testUser)
		require.NoError(t, err)

		assert.NotEqual(t, tokenPair.AccessToken, newTokenPair.AccessToken)
		assert.NotEqual(t, tokenPair.RefreshToken, newTokenPair.RefreshToken)

		// Validate new access token
		claims, err := authService.ValidateAccessToken(newTokenPair.AccessToken)
		require.NoError(t, err)
		assert.Equal(t, testUser.ID, claims.UserID)
	})

	t.Run("InvalidRefreshToken", func(t *testing.T) {
		_, err := authService.RefreshAccessToken("invalid-refresh-token", testUser)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid refresh token")
	})

	t.Run("UserIDMismatch", func(t *testing.T) {
		tokenPair, err := authService.GenerateTokenPair(testUser)
		require.NoError(t, err)

		// Try to refresh with different user
		differentUser := UserInfo{
			ID:       "different-user",
			Username: "differentuser",
			Email:    "different@example.com",
			Roles:    []string{"user"},
			IsActive: true,
		}

		_, err = authService.RefreshAccessToken(tokenPair.RefreshToken, differentUser)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user ID mismatch")
	})
}

func TestGlobalAuthService(t *testing.T) {
	t.Run("GetAuthService", func(t *testing.T) {
		// First call should initialize with default config
		service1 := GetAuthService()
		assert.NotNil(t, service1)

		// Second call should return same instance
		service2 := GetAuthService()
		assert.Equal(t, service1, service2)
	})

	t.Run("InitializeAuth", func(t *testing.T) {
		config := AuthConfig{
			JWTSecret:       "custom-secret",
			AccessTokenTTL:  30 * time.Minute,
			RefreshTokenTTL: 14 * 24 * time.Hour,
			Issuer:          "custom-issuer",
			Audience:        "custom-audience",
		}

		InitializeAuth(config)
		service := GetAuthService()

		assert.NotNil(t, service)
		assert.Equal(t, []byte(config.JWTSecret), service.jwtSecret)
		assert.Equal(t, config.AccessTokenTTL, service.accessTokenTTL)
	})
}

// Benchmark tests
func BenchmarkGenerateTokenPair(b *testing.B) {
	authService := NewAuthService(DefaultAuthConfig())

	testUser := UserInfo{
		ID:       "user-123",
		Username: "testuser",
		Email:    "test@example.com",
		Roles:    []string{"user"},
		IsActive: true,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := authService.GenerateTokenPair(testUser)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidateAccessToken(b *testing.B) {
	authService := NewAuthService(DefaultAuthConfig())

	testUser := UserInfo{
		ID:       "user-123",
		Username: "testuser",
		Email:    "test@example.com",
		Roles:    []string{"user"},
		IsActive: true,
	}

	tokenPair, err := authService.GenerateTokenPair(testUser)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := authService.ValidateAccessToken(tokenPair.AccessToken)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHashPassword(b *testing.B) {
	authService := NewAuthService(DefaultAuthConfig())
	password := "testpassword123"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := authService.HashPassword(password)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyPassword(b *testing.B) {
	authService := NewAuthService(DefaultAuthConfig())
	password := "testpassword123"

	hash, err := authService.HashPassword(password)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !authService.VerifyPassword(password, hash) {
			b.Fatal("Password verification failed")
		}
	}
}
