package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/config"
	"github.com/smart-invest-solutions/backend/pkg/response"
	"github.com/smart-invest-solutions/backend/pkg/utils"
)

// AuthCtxKey is the key used to store user claims in the gin context
const AuthCtxKey = "user_claims"

// RequireAuth validates the JWT token and adds the claims to the context.
func RequireAuth(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			response.Error(c, http.StatusUnauthorized, "Authorization header is required")
			c.Abort()
			return
		}

		// Check Bearer scheme
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			response.Error(c, http.StatusUnauthorized, "Invalid authorization format. Use 'Bearer <token>'")
			c.Abort()
			return
		}

		tokenString := parts[1]

		// Validate token
		claims, err := utils.ValidateJWT(tokenString, cfg.JWTSecret)
		if err != nil {
			response.Error(c, http.StatusUnauthorized, "Invalid or expired token")
			c.Abort()
			return
		}

		// Store claims in context for later use
		c.Set(AuthCtxKey, claims)
		c.Next()
	}
}

// RequireRole checks if the authenticated user has one of the allowed roles.
// Must be used after RequireAuth.
func RequireRole(allowedRoles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get claims from context
		claimsVal, exists := c.Get(AuthCtxKey)
		if !exists {
			response.Error(c, http.StatusUnauthorized, "Authentication required")
			c.Abort()
			return
		}

		claims, ok := claimsVal.(*utils.Claims)
		if !ok {
			response.Error(c, http.StatusInternalServerError, "Invalid user claims")
			c.Abort()
			return
		}

		// Check if user's role is in the allowed roles
		hasRole := false
		for _, role := range allowedRoles {
			if claims.Role == role {
				hasRole = true
				break
			}
		}

		// Also allow super_admin by default for any role-protected route
		if claims.Role == "super_admin" {
			hasRole = true
		}

		if !hasRole {
			response.Error(c, http.StatusForbidden, "You don't have permission to perform this action")
			c.Abort()
			return
		}

		c.Next()
	}
}
