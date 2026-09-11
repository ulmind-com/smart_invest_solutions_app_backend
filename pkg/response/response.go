package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// APIResponse represents a standardized API response.
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// PaginatedResponse represents a paginated API response.
type PaginatedResponse struct {
	Success    bool        `json:"success"`
	Message    string      `json:"message"`
	Data       interface{} `json:"data,omitempty"`
	Page       int64       `json:"page"`
	Limit      int64       `json:"limit"`
	Total      int64       `json:"total"`
	TotalPages int64       `json:"total_pages"`
}

// Success sends a successful response with HTTP 200.
func Success(c *gin.Context, message string, data interface{}) {
	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Message: message,
		Data:    data,
	})
}

// Created sends a successful response with HTTP 201.
func Created(c *gin.Context, message string, data interface{}) {
	c.JSON(http.StatusCreated, APIResponse{
		Success: true,
		Message: message,
		Data:    data,
	})
}

// Error sends an error response with the given HTTP status code.
func Error(c *gin.Context, statusCode int, message string) {
	c.JSON(statusCode, APIResponse{
		Success: false,
		Message: "Request failed",
		Error:   message,
	})
}

// ValidationError sends a validation error response with HTTP 422.
func ValidationError(c *gin.Context, message string) {
	c.JSON(http.StatusUnprocessableEntity, APIResponse{
		Success: false,
		Message: "Validation failed",
		Error:   message,
	})
}

// SuccessWithPagination sends a paginated success response.
func SuccessWithPagination(c *gin.Context, message string, data interface{}, page, limit, total int64) {
	// Handlers parse `limit` straight from the query string and pass it here for display purposes
	// even though the service layer clamps its own copy before querying — so an out-of-range value
	// (0, negative, or absurdly large) must never reach the division below, or every paginated
	// endpoint becomes a one-request crash (?limit=0) via a straightforward integer divide-by-zero.
	safeLimit := limit
	if safeLimit < 1 {
		safeLimit = 1
	}

	totalPages := total / safeLimit
	if total%safeLimit != 0 {
		totalPages++
	}

	c.JSON(http.StatusOK, PaginatedResponse{
		Success:    true,
		Message:    message,
		Data:       data,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: totalPages,
	})
}
