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
	lifeInsuranceRepo := repository.NewLifeInsuranceRepository(db.Database)
	fixedDepositRepo := repository.NewFixedDepositRepository(db.Database)
	healthInsuranceRepo := repository.NewHealthInsuranceRepository(db.Database)
	supportTicketRepo := repository.NewSupportTicketRepository(db.Database)
	productRepo := repository.NewProductRepository(db.Database)
	calculatorRepo := repository.NewCalculatorSettingsRepository(db.Database)
	referralRepo := repository.NewReferralRepository(db.Database)
	emailVerifRepo := repository.NewEmailVerificationRepository(db.Database)

	// Initialize Services
	userSvcConcrete := service.NewUserService(userRepo, cfg, emailSvc)
	if setter, ok := userSvcConcrete.(interface {
		SetCascadeDependencies(domain.FamilyMemberRepository, domain.GeneralInsuranceRepository, domain.DocumentRepository, domain.LifeInsuranceRepository, domain.FixedDepositRepository, domain.HealthInsuranceRepository, domain.SupportTicketRepository, domain.AccessRequestRepository, domain.EmailVerificationRepository, service.StorageService)
	}); ok {
		setter.SetCascadeDependencies(familyMemberRepo, generalInsuranceRepo, documentRepo, lifeInsuranceRepo, fixedDepositRepo, healthInsuranceRepo, supportTicketRepo, accessReqRepo, emailVerifRepo, storageSvc)
	}
	userService := userSvcConcrete

	accessReqService := service.NewAccessRequestService(accessReqRepo, userRepo, userService, emailSvc, referralRepo)
	passResetService := service.NewPasswordResetService(passResetRepo, userRepo, emailSvc)
	emailVerifService := service.NewEmailVerificationService(emailVerifRepo, userRepo, accessReqRepo, emailSvc)
	familyMemberService := service.NewFamilyMemberService(familyMemberRepo)
	generalInsuranceService := service.NewGeneralInsuranceService(generalInsuranceRepo)
	documentService := service.NewDocumentService(documentRepo, storageSvc)
	lifeInsuranceService := service.NewLifeInsuranceService(lifeInsuranceRepo, userRepo, familyMemberRepo)
	fixedDepositService := service.NewFixedDepositService(fixedDepositRepo, userRepo, familyMemberRepo)
	healthInsuranceService := service.NewHealthInsuranceService(healthInsuranceRepo, userRepo, familyMemberRepo)
	supportTicketService := service.NewSupportTicketService(supportTicketRepo, userRepo)
	productService := service.NewProductService(productRepo, storageSvc)
	dashboardService := service.NewDashboardService(userRepo, familyMemberRepo, lifeInsuranceRepo, healthInsuranceRepo, generalInsuranceRepo, fixedDepositRepo, accessReqRepo)
	reportService := service.NewReportService(userRepo, familyMemberRepo, lifeInsuranceRepo, healthInsuranceRepo, generalInsuranceRepo, fixedDepositRepo)
	agencySyncService := service.NewAgencySyncService(lifeInsuranceRepo)
	calculatorService := service.NewCalculatorService(calculatorRepo)
	referralService := service.NewReferralService(referralRepo, userRepo)

	// Initialize Handlers
	userHandler := handler.NewUserHandler(userService, passResetService)
	accessReqHandler := handler.NewAccessRequestHandler(accessReqService)
	emailVerifHandler := handler.NewEmailVerificationHandler(emailVerifService)
	familyMemberHandler := handler.NewFamilyMemberHandler(familyMemberService)
	generalInsuranceHandler := handler.NewGeneralInsuranceHandler(generalInsuranceService)
	documentHandler := handler.NewDocumentHandler(documentService)
	lifeInsuranceHandler := handler.NewLifeInsuranceHandler(lifeInsuranceService)
	fixedDepositHandler := handler.NewFixedDepositHandler(fixedDepositService)
	healthInsuranceHandler := handler.NewHealthInsuranceHandler(healthInsuranceService)
	supportTicketHandler := handler.NewSupportTicketHandler(supportTicketService)
	productHandler := handler.NewProductHandler(productService)
	dashboardHandler := handler.NewDashboardHandler(dashboardService)
	reportHandler := handler.NewReportHandler(reportService)
	agencySyncHandler := handler.NewAgencySyncHandler(agencySyncService)
	calculatorHandler := handler.NewCalculatorHandler(calculatorService)
	referralHandler := handler.NewReferralHandler(referralService)

	// API v1 routes
	v1 := router.Group("/api/v1")
	{
		// User routes
		users := v1.Group("/users")
		{
			// Public routes
			users.POST("/register", userHandler.Register)
			users.POST("/login", userHandler.Login)
			users.POST("/verify-email-otp", emailVerifHandler.VerifyEmailOTP)
			users.POST("/resend-email-otp", emailVerifHandler.ResendEmailOTP)
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

		// Life Insurance routes — RBAC (client-owns-only vs admin-bypass) is enforced inside the
		// service layer per policy, so no RequireRole gate is needed at the router level; every
		// route just requires authentication.
		lifeInsurances := v1.Group("/life-insurances")
		{
			lifeInsurances.Use(middleware.RequireAuth(cfg))

			lifeInsurances.POST("", lifeInsuranceHandler.CreatePolicy)
			lifeInsurances.GET("", lifeInsuranceHandler.GetPolicies)
			lifeInsurances.GET("/:id", lifeInsuranceHandler.GetByID)
			lifeInsurances.PUT("/:id", lifeInsuranceHandler.UpdatePolicy)
			lifeInsurances.DELETE("/:id", lifeInsuranceHandler.DeletePolicy)
		}

		// Fixed Deposit / Postal routes — RBAC (client-owns-only vs admin-bypass, plus the
		// admin-only is_mapped modification rule) is enforced inside the service layer, so no
		// RequireRole gate is needed at the router level; every route just requires authentication.
		fixedDeposits := v1.Group("/fixed-deposits")
		{
			fixedDeposits.Use(middleware.RequireAuth(cfg))

			fixedDeposits.POST("", fixedDepositHandler.CreateFD)
			fixedDeposits.GET("", fixedDepositHandler.GetFDs)
			fixedDeposits.GET("/:id", fixedDepositHandler.GetByID)
			fixedDeposits.PUT("/:id", fixedDepositHandler.UpdateFD)
			fixedDeposits.DELETE("/:id", fixedDepositHandler.DeleteFD)
		}

		// Health Insurance routes — RBAC (client-owns-only vs admin-bypass, plus the admin-only
		// is_mapped modification rule) is enforced inside the service layer, so no RequireRole
		// gate is needed at the router level; every route just requires authentication.
		healthInsurances := v1.Group("/health-insurances")
		{
			healthInsurances.Use(middleware.RequireAuth(cfg))

			healthInsurances.POST("", healthInsuranceHandler.CreatePolicy)
			healthInsurances.GET("", healthInsuranceHandler.GetPolicies)
			healthInsurances.GET("/:id", healthInsuranceHandler.GetByID)
			healthInsurances.PUT("/:id", healthInsuranceHandler.UpdatePolicy)
			healthInsurances.DELETE("/:id", healthInsuranceHandler.DeletePolicy)
		}

		// Support Ticket routes — RBAC (client-owns-only vs admin-bypass, plus the
		// Status/AdminNotes client-field-stripping rule) is enforced inside the service layer.
		// DELETE is additionally restricted to super_admin at the router level.
		tickets := v1.Group("/tickets")
		{
			tickets.Use(middleware.RequireAuth(cfg))

			tickets.POST("", supportTicketHandler.CreateTicket)
			tickets.GET("", supportTicketHandler.GetTickets)
			tickets.GET("/:id", supportTicketHandler.GetByID)
			tickets.PUT("/:id", supportTicketHandler.UpdateTicket)
			tickets.DELETE("/:id", middleware.RequireRole(domain.RoleSuperAdmin), supportTicketHandler.DeleteTicket)
		}

		// Product Catalog routes — fulfills the "KNOW ABOUT ALL PRODUCT" requirement. GET routes
		// are open to any authenticated role (client/advisor/admin/super_admin); the service layer
		// forces client/advisor requesters to the published (is_active=true) subset. Writes
		// (create/update/delete) are gated to admin/super_admin at the router level, with the
		// service layer re-checking the role as defense in depth.
		products := v1.Group("/products")
		{
			products.Use(middleware.RequireAuth(cfg))

			products.GET("", productHandler.GetProducts)
			products.GET("/:id", productHandler.GetByID)

			adminProducts := products.Group("")
			adminProducts.Use(middleware.RequireRole(domain.RoleAdmin))
			{
				adminProducts.POST("", productHandler.CreateProduct)
				adminProducts.PUT("/:id", productHandler.UpdateProduct)
				adminProducts.DELETE("/:id", productHandler.DeleteProduct)
			}
		}

		// Dashboard routes — pure aggregation views over existing repositories, no own collection.
		dashboard := v1.Group("/dashboard")
		{
			dashboard.Use(middleware.RequireAuth(cfg))

			clientDashboard := dashboard.Group("")
			clientDashboard.Use(middleware.RequireRole("client"))
			{
				clientDashboard.GET("/client", dashboardHandler.GetClientDashboard)
			}

			adminDashboard := dashboard.Group("")
			adminDashboard.Use(middleware.RequireRole("admin"))
			{
				adminDashboard.GET("/admin", dashboardHandler.GetAdminDashboard)
			}
		}

		// Report routes — pure orchestration over existing repositories, no own collection. RBAC
		// (client-self-only vs admin-can-target-any-client via ?user_id=) is enforced inside the
		// handler, so no RequireRole gate is needed at the router level; the route just requires
		// authentication.
		reports := v1.Group("/reports")
		{
			reports.Use(middleware.RequireAuth(cfg))

			reports.GET("/portfolio", reportHandler.GetClientPortfolio)
		}

		// Agency Sync routes — automated bulk updates from LIC Premium Due List PDFs
		agency := v1.Group("/agency")
		{
			agency.Use(middleware.RequireAuth(cfg))
			agency.Use(middleware.RequireRole("admin"))

			agency.POST("/sync/lic-due-list", agencySyncHandler.ProcessLICDueList)
		}

		// Financial Calculators routes — SIP, Lumpsum, and FD calculators with Admin rate settings
		calculators := v1.Group("/calculators")
		{
			calculators.Use(middleware.RequireAuth(cfg))

			calculators.GET("/settings", calculatorHandler.GetSettings)
			calculators.PUT("/settings", middleware.RequireRole(domain.RoleAdmin), calculatorHandler.UpdateSettings)
			calculators.POST("/sip", calculatorHandler.CalculateSIP)
			calculators.POST("/lumpsum", calculatorHandler.CalculateLumpsum)
			calculators.POST("/fd", calculatorHandler.CalculateFD)
		}

		// Referral Scheme routes — Earn Extra Validity referrals & agency growth tracking
		referrals := v1.Group("/referrals")
		{
			referrals.Use(middleware.RequireAuth(cfg))

			referrals.GET("/my-stats", referralHandler.GetMyStats)
			referrals.GET("/all", middleware.RequireRole(domain.RoleAdmin), referralHandler.GetAllReferrals)
		}
	}

	return router
}
