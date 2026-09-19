package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/config"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/pkg/response"
	"github.com/smart-invest-solutions/backend/pkg/utils"
)

// AuthCtxKey is the key used to store user claims in the gin context
const AuthCtxKey = "user_claims"

// RequireAuth validates the JWT token, re-checks that the account behind it may still use the API,
// and adds the claims to the context.
//
// A JWT stays cryptographically valid for its whole lifetime (24h by default), so the signature
// alone can't tell that an admin has since deactivated the account, merged it into another family
// account, let an admin's access expire, deleted it, or changed its role. The account guard looks
// the user up (through a short-lived cache) on every request and rejects the token as soon as any
// of that happens, instead of letting the old session run until the token expires.
func RequireAuth(cfg *config.Config, users domain.UserRepository) gin.HandlerFunc {
	guard := newAccountGuard(users)

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

		if status, message := guard.check(c.Request.Context(), claims); status != 0 {
			response.Error(c, status, message)
			c.Abort()
			return
		}

		// Store claims in context for later use
		c.Set(AuthCtxKey, claims)
		c.Next()
	}
}

// GetClaims retrieves the authenticated user's JWT claims from the Gin context.
// Must be called after RequireAuth. Returns false if claims are missing or malformed.
func GetClaims(c *gin.Context) (*utils.Claims, bool) {
	claimsVal, exists := c.Get(AuthCtxKey)
	if !exists {
		return nil, false
	}
	claims, ok := claimsVal.(*utils.Claims)
	if !ok {
		return nil, false
	}
	return claims, true
}

// GetUserID retrieves the authenticated user's ID (hex string) from the Gin context.
// Must be called after RequireAuth. Returns false if claims are missing or malformed.
func GetUserID(c *gin.Context) (string, bool) {
	claims, ok := GetClaims(c)
	if !ok {
		return "", false
	}
	return claims.UserID.Hex(), true
}

// RequireRole checks if the authenticated user has one of the allowed roles.
// Must be used after RequireAuth.
func RequireRole(allowedRoles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get claims from context
		claims, ok := GetClaims(c)
		if !ok {
			response.Error(c, http.StatusUnauthorized, "Authentication required")
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
