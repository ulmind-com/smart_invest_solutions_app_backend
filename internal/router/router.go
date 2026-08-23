package router

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/config"
	"github.com/smart-invest-solutions/backend/internal/database"
	"github.com/smart-invest-solutions/backend/internal/domain"
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

	// Swagger Documentation UI — visit /swagger/index.html
	url := ginSwagger.URL("/swagger/doc.json")
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler, url))
	router.GET("/swagger", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/swagger/index.html")
	})

	// Initialize Email & Storage services
	emailSvc := email.NewResendService(cfg)
	storageSvc, _ := service.NewCloudinaryService(cfg)

	// Initialize Repositories
	userRepo := repository.NewUserRepository(db.Database)
	accessReqRepo := repository.NewAccessRequestRepository(db.Database)
	passResetRepo := repository.NewPasswordResetRepository(db.Database)
	familyMemberRepo := repository.NewFamilyMemberRepository(db.Database)
	generalInsuranceRepo := repository.NewGeneralInsuranceRepository(db.Database)
	documentRepo := repository.NewDocumentRepository(db.Database)

	// Initialize Services
	userSvcConcrete := service.NewUserService(userRepo, cfg, emailSvc)
	if setter, ok := userSvcConcrete.(interface {
		SetCascadeDependencies(domain.FamilyMemberRepository, domain.GeneralInsuranceRepository, domain.DocumentRepository, service.StorageService)
	}); ok {
		setter.SetCascadeDependencies(familyMemberRepo, generalInsuranceRepo, documentRepo, storageSvc)
	}
	userService := userSvcConcrete

	accessReqService := service.NewAccessRequestService(accessReqRepo, userRepo, userService, emailSvc)
	passResetService := service.NewPasswordResetService(passResetRepo, userRepo, emailSvc)
	familyMemberService := service.NewFamilyMemberService(familyMemberRepo)
	generalInsuranceService := service.NewGeneralInsuranceService(generalInsuranceRepo)
	documentService := service.NewDocumentService(documentRepo, storageSvc)

	// Initialize Handlers
	userHandler := handler.NewUserHandler(userService, passResetService)
	accessReqHandler := handler.NewAccessRequestHandler(accessReqService)
	familyMemberHandler := handler.NewFamilyMemberHandler(familyMemberService)
	generalInsuranceHandler := handler.NewGeneralInsuranceHandler(generalInsuranceService)
	documentHandler := handler.NewDocumentHandler(documentService)

	// API v1 routes
	v1 := router.Group("/api/v1")
	{
		// User routes
		users := v1.Group("/users")
		{
			// Public routes
			users.POST("/register", userHandler.Register)
			users.POST("/login", userHandler.Login)
			users.POST("/forgot-password", userHandler.ForgotPassword)
			users.POST("/verify-otp", userHandler.VerifyOTP)
			users.POST("/reset-password", userHandler.ResetPassword)

			// Protected routes (Require Login)
			users.Use(middleware.RequireAuth(cfg))

			users.GET("/me", userHandler.GetProfile)
			users.PUT("/me", userHandler.UpdateProfile)
			users.DELETE("/me", userHandler.DeleteMyAccount)
			users.PUT("/change-password", userHandler.ChangePassword)

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

		// Admin Account Management routes (Super Admin only)
		admins := v1.Group("/admins")
		{
			// Public route — admin/super_admin login via AdminID + PIN
			admins.POST("/login", userHandler.AdminLogin)

			// Protected routes (Require Login + super_admin role)
			protectedAdmins := admins.Group("")
			protectedAdmins.Use(middleware.RequireAuth(cfg))
			protectedAdmins.Use(middleware.RequireRole("super_admin"))
			{
				protectedAdmins.POST("", userHandler.CreateAdmin)
				protectedAdmins.GET("", userHandler.GetAllAdmins)
				protectedAdmins.DELETE("/:id", userHandler.DeleteAdmin)
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

		// Family Member routes
		familyMembers := v1.Group("/family-members")
		{
			familyMembers.Use(middleware.RequireAuth(cfg))

			familyMembers.POST("", familyMemberHandler.AddMember)
			familyMembers.GET("", familyMemberHandler.GetMyMembers)
			familyMembers.GET("/:id", familyMemberHandler.GetByID)
			familyMembers.PUT("/:id", familyMemberHandler.UpdateMember)
			familyMembers.DELETE("/:id", familyMemberHandler.DeleteMember)

			// Admin route
			adminFamily := familyMembers.Group("")
			adminFamily.Use(middleware.RequireRole("admin"))
			{
				adminFamily.GET("/user/:userId", familyMemberHandler.GetMembersByUserIDAdmin)
			}
		}

		// General Insurance routes
		generalInsurances := v1.Group("/general-insurances")
		{
			generalInsurances.Use(middleware.RequireAuth(cfg))

			generalInsurances.POST("", generalInsuranceHandler.AddInsurance)
			generalInsurances.GET("", generalInsuranceHandler.GetMyInsurances)
			generalInsurances.GET("/:id", generalInsuranceHandler.GetByID)
			generalInsurances.PUT("/:id", generalInsuranceHandler.UpdateInsurance)
			generalInsurances.DELETE("/:id", generalInsuranceHandler.DeleteInsurance)

			// Admin route
			adminInsurance := generalInsurances.Group("")
			adminInsurance.Use(middleware.RequireRole("admin"))
			{
				adminInsurance.GET("/all", generalInsuranceHandler.GetAllInsurancesAdmin)
				adminInsurance.GET("/user/:userId", generalInsuranceHandler.GetInsurancesByUserIDAdmin)
			}
		}

		// E-Vault Document routes
		documents := v1.Group("/documents")
		{
			documents.Use(middleware.RequireAuth(cfg))

			documents.POST("", documentHandler.UploadDocument)
			documents.GET("", documentHandler.GetMyDocuments)
			documents.GET("/:id", documentHandler.GetByID)
			documents.PUT("/:id", documentHandler.UpdateDocument)
			documents.DELETE("/:id", documentHandler.DeleteDocument)

			// Admin route
			adminDocs := documents.Group("")
			adminDocs.Use(middleware.RequireRole("admin"))
			{
				adminDocs.GET("/user/:userId", documentHandler.GetDocumentsByUserIDAdmin)
			}
		}
	}

	return router
}
