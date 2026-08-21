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

	// Initialize layers
	userRepo := repository.NewUserRepository(db.Database)
	userService := service.NewUserService(userRepo, cfg)
	userHandler := handler.NewUserHandler(userService)

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
			
			// Anyone logged in can view their own profile (or others if allowed, logic in handler)
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
	}

	return router
}
