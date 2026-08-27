package utils

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Claims represents the JWT claims for the application.
type Claims struct {
	UserID          bson.ObjectID  `json:"user_id"`
	Role            string         `json:"role"`
	ImpersonatedBy  *bson.ObjectID `json:"impersonated_by,omitempty"`
	IsImpersonating bool           `json:"is_impersonating,omitempty"`
	jwt.RegisteredClaims
}

// GenerateJWT creates a new JWT token for a user.
func GenerateJWT(userID bson.ObjectID, role string, secret string, expiryHours int) (string, error) {
	// Set expiration time
	expirationTime := time.Now().Add(time.Duration(expiryHours) * time.Hour)

	// Create the claims
	claims := &Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	// Create token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// Sign token with secret
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil
}

// GenerateImpersonationJWT creates a new JWT token for a super_admin impersonating a target user.
func GenerateImpersonationJWT(targetUserID, superAdminID bson.ObjectID, role string, secret string, expiryHours int) (string, error) {
	expirationTime := time.Now().Add(time.Duration(expiryHours) * time.Hour)

	claims := &Claims{
		UserID:          targetUserID,
		Role:            role,
		ImpersonatedBy:  &superAdminID,
		IsImpersonating: true,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("failed to sign impersonation token: %w", err)
	}

	return tokenString, nil
}

// ValidateJWT parses and validates a JWT token.
func ValidateJWT(tokenString string, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	})

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	return claims, nil
}
