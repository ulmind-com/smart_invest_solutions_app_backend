package router

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/config"
	"github.com/smart-invest-solutions/backend/internal/database"
	"github.com/smart-invest-solutions/backend/internal/handler"
	"github.com/smart-invest-solutions/backend/internal/middleware"
	"github.com/smart-invest-solutions/backend/internal/repository"
	"github.com/smart-invest-solutions/backend/internal/service"
	"github.com/smart-invest-solutions/backend/pkg/email"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "github.com/smart-invest-solutions/backend/docs" // Swagger generated docs
)

// Setup initializes the Gin router with all routes and middleware.
func Setup(db *database.MongoDB, cfg *config.Config) *gin.Engine {
	router := gin.New()

	// Global middleware
	router.Use(middleware.Recovery())
	router.Use(middleware.RequestLogger())
	router.Use(middleware.CORS())

	// Health check endpoint
	router.GET("/health", func(c *gin.Context) {
		if err := db.HealthCheck(c.Request.Context()); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status":  "unhealthy",
				"message": "Database connection failed",
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status":  "healthy",
			"message": "Smart Invest Solutions API is running",
		})
	})

	// Swagger Documentation UI — visit http://localhost:8080/swagger/index.html
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// Initialize Email & Storage services
	emailSvc := email.NewResendService(cfg)

	// Initialize Repositories
	userRepo := repository.NewUserRepository(db.Database)
	accessReqRepo := repository.NewAccessRequestRepository(db.Database)

	// Initialize Services
	userService := service.NewUserService(userRepo, cfg)
	accessReqService := service.NewAccessRequestService(accessReqRepo, userRepo, userService, emailSvc)

	// Initialize Handlers
	userHandler := handler.NewUserHandler(userService)
	accessReqHandler := handler.NewAccessRequestHandler(accessReqService)

	// API v1 routes
	v1 := router.Group("/api/v1")
	{
		// User routes
		users := v1.Group("/users")
		{
			// Public routes
			users.POST("/register", userHandler.Register)
			users.POST("/login", userHandler.Login)

			// Protected routes (Require Login)
			users.Use(middleware.RequireAuth(cfg))

			users.GET("/:id", userHandler.GetByID)
			users.PUT("/:id", userHandler.Update)

			// Admin only routes
			adminOnly := users.Group("")
			adminOnly.Use(middleware.RequireRole("admin"))
			{
				adminOnly.GET("", userHandler.GetAll)
				adminOnly.DELETE("/:id", userHandler.Delete)
			}
		}

		// Access Request routes
		accessReqs := v1.Group("/access-requests")
		{
			// Public endpoint for clients to request access
			accessReqs.POST("", accessReqHandler.SubmitRequest)

			// Admin-only endpoints for reviewing, approving & rejecting requests
			adminReqs := accessReqs.Group("")
			adminReqs.Use(middleware.RequireAuth(cfg))
			adminReqs.Use(middleware.RequireRole("admin"))
			{
				adminReqs.GET("", accessReqHandler.GetAllRequests)
				adminReqs.GET("/:id", accessReqHandler.GetRequestByID)
				adminReqs.POST("/:id/approve", accessReqHandler.ApproveRequest)
				adminReqs.POST("/:id/reject", accessReqHandler.RejectRequest)
			}
		}
	}

	return router
}
