package router

import (
	"net/http"
	"time"

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

	// Caps the total size Gin will buffer for a multipart/form-data request (document/brochure
	// uploads) — without this, an authenticated client can send an arbitrarily large file and force
	// the server to read the whole thing into memory (io.ReadAll in the storage service has no
	// bound of its own), a straightforward memory-exhaustion DoS vector.
	router.MaxMultipartMemory = 10 << 20 // 10 MiB

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
	storageSvc := service.NewStorageService(cfg)

	// Initialize Repositories
	userRepo := repository.NewUserRepository(db.Database)
	accessReqRepo := repository.NewAccessRequestRepository(db.Database)
	passResetRepo := repository.NewPasswordResetRepository(db.Database)
	familyMemberRepo := repository.NewFamilyMemberRepository(db.Database)
	generalInsuranceRepo := repository.NewGeneralInsuranceRepository(db.Database)
	documentRepo := repository.NewDocumentRepository(db.Database)
	lifeInsuranceRepo := repository.NewLifeInsuranceRepository(db.Database)
	importedPolicyRepo := repository.NewImportedPolicyRepository(db.Database)
	importedDepositRepo := repository.NewImportedDepositRepository(db.Database)
	fixedDepositRepo := repository.NewFixedDepositRepository(db.Database)
	healthInsuranceRepo := repository.NewHealthInsuranceRepository(db.Database)
	supportTicketRepo := repository.NewSupportTicketRepository(db.Database)
	productRepo := repository.NewProductRepository(db.Database)
	calculatorRepo := repository.NewCalculatorSettingsRepository(db.Database)
	referralRepo := repository.NewReferralRepository(db.Database)
	clientMapRepo := repository.NewClientMapRepository(db.Database)
	productAccessRepo := repository.NewClientProductAccessRepository(db.Database)
	renewalRepo := repository.NewRenewalRepository(db.Database)
	announcementRepo := repository.NewAnnouncementRepository(db.Database)
	emailVerifRepo := repository.NewEmailVerificationRepository(db.Database)

	// Initialize Services
	userSvcConcrete := service.NewUserService(userRepo, cfg, emailSvc)
	if setter, ok := userSvcConcrete.(interface {
		SetCascadeDependencies(domain.FamilyMemberRepository, domain.GeneralInsuranceRepository, domain.DocumentRepository, domain.LifeInsuranceRepository, domain.FixedDepositRepository, domain.HealthInsuranceRepository, domain.SupportTicketRepository, domain.AccessRequestRepository, domain.EmailVerificationRepository, service.StorageService)
	}); ok {
		setter.SetCascadeDependencies(familyMemberRepo, generalInsuranceRepo, documentRepo, lifeInsuranceRepo, fixedDepositRepo, healthInsuranceRepo, supportTicketRepo, accessReqRepo, emailVerifRepo, storageSvc)
	}
	if setter, ok := userSvcConcrete.(interface {
		SetReferralRepository(domain.ReferralRepository)
	}); ok {
		setter.SetReferralRepository(referralRepo)
	}
	if setter, ok := userSvcConcrete.(interface {
		SetProductAccessRepository(domain.ClientProductAccessRepository)
	}); ok {
		setter.SetProductAccessRepository(productAccessRepo)
	}
	userService := userSvcConcrete

	accessReqService := service.NewAccessRequestService(accessReqRepo, userRepo, userService, emailSvc, referralRepo)
	passResetService := service.NewPasswordResetService(passResetRepo, userRepo, emailSvc)
	emailVerifService := service.NewEmailVerificationService(emailVerifRepo, userRepo, accessReqRepo, emailSvc)
	familyMemberService := service.NewFamilyMemberService(familyMemberRepo, userRepo, lifeInsuranceRepo, healthInsuranceRepo, fixedDepositRepo)
	generalInsuranceService := service.NewGeneralInsuranceService(generalInsuranceRepo, userRepo)
	documentService := service.NewDocumentService(documentRepo, storageSvc, userRepo)
	lifeInsuranceService := service.NewLifeInsuranceService(lifeInsuranceRepo, userRepo, familyMemberRepo)
	fixedDepositService := service.NewFixedDepositService(fixedDepositRepo, userRepo, familyMemberRepo)
	healthInsuranceService := service.NewHealthInsuranceService(healthInsuranceRepo, userRepo, familyMemberRepo)
	supportTicketService := service.NewSupportTicketService(supportTicketRepo, userRepo)
	// The catalog is platform-wide; this is what makes one client's Products tab differ from
	// another's, so the product service cannot be built without it.
	productAccessService := service.NewClientProductAccessService(productAccessRepo, userRepo, productRepo)
	productService := service.NewProductService(productRepo, storageSvc, productAccessService, productAccessRepo)
	dashboardService := service.NewDashboardService(userRepo, familyMemberRepo, lifeInsuranceRepo, healthInsuranceRepo, generalInsuranceRepo, fixedDepositRepo, accessReqRepo)
	reportService := service.NewReportService(userRepo, familyMemberRepo, lifeInsuranceRepo, healthInsuranceRepo, generalInsuranceRepo, fixedDepositRepo)
	agencySyncService := service.NewAgencySyncService(lifeInsuranceRepo, importedPolicyRepo, fixedDepositRepo, importedDepositRepo, familyMemberRepo, userRepo)
	calculatorService := service.NewCalculatorService(calculatorRepo)
	referralService := service.NewReferralService(referralRepo, userRepo)
	clientMapService := service.NewClientMapService(clientMapRepo, userRepo)
	renewalService := service.NewRenewalService(renewalRepo, userRepo)
	announcementService := service.NewAnnouncementService(announcementRepo, userRepo, storageSvc)

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
	clientMapHandler := handler.NewClientMapHandler(clientMapService)
	renewalHandler := handler.NewRenewalHandler(renewalService)
	announcementHandler := handler.NewAnnouncementHandler(announcementService)
	productAccessHandler := handler.NewClientProductAccessHandler(productAccessService)

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
			// Rate limited per IP: this endpoint tells the caller whether an address is registered,
			// which is deliberate (see domain.UnknownAccountError) but must not be usable to test
			// addresses in bulk. Six a minute is far more than a person resetting their own password
			// needs, and far less than enumeration needs to be worth doing.
			users.POST("/forgot-password",
				middleware.RateLimit(6, time.Minute),
				userHandler.ForgotPassword)
			users.POST("/verify-otp", userHandler.VerifyOTP)
			users.POST("/reset-password", userHandler.ResetPassword)

			// Protected routes (Require Login)
			users.Use(middleware.RequireAuth(cfg, userRepo))

			users.GET("/me", userHandler.GetProfile)
			users.GET("/me/advisor", userHandler.GetMyAdvisor)
			users.PUT("/me", userHandler.UpdateProfile)
			users.DELETE("/me", userHandler.DeleteMyAccount)
			users.PUT("/change-password", userHandler.ChangePassword)
			users.PUT("/change-pin", userHandler.ChangePIN)

			// Admin only routes — GetByID/Update let staff look up or edit another account
			// (e.g. ClientDetailScreen), so they must never be reachable by a plain client.
			adminOnly := users.Group("")
			adminOnly.Use(middleware.RequireRole("admin"))
			{
				adminOnly.GET("", userHandler.GetAll)
				adminOnly.GET("/:id", userHandler.GetByID)
				adminOnly.PUT("/:id", userHandler.Update)
				adminOnly.DELETE("/:id", userHandler.Delete)
				// Which catalog products this one client sees. Hangs off the client rather than off
				// /products because it is a property of the client, not of a product.
				adminOnly.GET("/:id/product-access", productAccessHandler.Get)
				adminOnly.PUT("/:id/product-access", productAccessHandler.Set)
			}
		}

		// Client map — the agency's client/policy table, with the admin behind each client and
		// whether that client is actually on the app. Its own group rather than a path under
		// /users, which is a per-account resource: this is one read-only cross-account view.
		clientMap := v1.Group("/client-map")
		{
			clientMap.Use(middleware.RequireAuth(cfg, userRepo))
			clientMap.Use(middleware.RequireRole("admin"))
			clientMap.GET("", clientMapHandler.GetClientMap)
		}

		// The renewal book — what is due and when, across every instrument, with the client and the
		// admin behind each row. Read-only, and agency-scoped the same way the master lists are.
		renewals := v1.Group("/renewals")
		{
			renewals.Use(middleware.RequireAuth(cfg, userRepo))
			renewals.Use(middleware.RequireRole("admin"))
			renewals.GET("", renewalHandler.GetRenewals)
		}

		// Screen banners. Reading what belongs on your own screen is open to every signed-in role —
		// the audience is resolved from the caller's role, not from the request — while curating them
		// is platform-wide and therefore super_admin only.
		announcements := v1.Group("/announcements")
		{
			announcements.Use(middleware.RequireAuth(cfg, userRepo))

			announcements.GET("", announcementHandler.GetMine)

			superAdminAnnouncements := announcements.Group("")
			superAdminAnnouncements.Use(middleware.RequireRole(domain.RoleSuperAdmin))
			{
				superAdminAnnouncements.GET("/all", announcementHandler.GetAll)
				superAdminAnnouncements.POST("", announcementHandler.Create)
				superAdminAnnouncements.PUT("/:id", announcementHandler.Update)
				superAdminAnnouncements.DELETE("/:id", announcementHandler.Delete)
			}
		}

		// Admin Account Management routes (Super Admin only)
		admins := v1.Group("/admins")
		{
			// Public route — admin/super_admin login via AdminID + PIN
			admins.POST("/login", userHandler.AdminLogin)

			// Protected routes (Require Login + super_admin role)
			protectedAdmins := admins.Group("")
			protectedAdmins.Use(middleware.RequireAuth(cfg, userRepo))
			protectedAdmins.Use(middleware.RequireRole("super_admin"))
			{
				protectedAdmins.POST("", userHandler.CreateAdmin)
				protectedAdmins.GET("", userHandler.GetAllAdmins)
				// Feeds the "generate" button on the create-admin form. A static segment beside
				// /:id/... routes, which Gin resolves in favour of the static one.
				protectedAdmins.GET("/next-id", userHandler.SuggestAdminID)
				protectedAdmins.DELETE("/:id", userHandler.DeleteAdmin)
				protectedAdmins.POST("/impersonate", userHandler.ImpersonateUser)
				protectedAdmins.GET("/expiring", userHandler.ListExpiringAdmins)
				protectedAdmins.PUT("/:id/expiry", userHandler.RenewAdminExpiry)
				protectedAdmins.POST("/:id/send-expiry-alert", userHandler.SendAdminExpiryAlert)
				protectedAdmins.POST("/merge-family", userHandler.MergeFamilyAccounts)
			}
		}

		// Access Request routes
		accessReqs := v1.Group("/access-requests")
		{
			// Public endpoint for clients to request access
			accessReqs.POST("", accessReqHandler.SubmitRequest)

			// Admin-only endpoints for reviewing, approving & rejecting requests
			adminReqs := accessReqs.Group("")
			adminReqs.Use(middleware.RequireAuth(cfg, userRepo))
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
			familyMembers.Use(middleware.RequireAuth(cfg, userRepo))

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
			generalInsurances.Use(middleware.RequireAuth(cfg, userRepo))

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
			documents.Use(middleware.RequireAuth(cfg, userRepo))

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
			lifeInsurances.Use(middleware.RequireAuth(cfg, userRepo))

			lifeInsurances.POST("", lifeInsuranceHandler.CreatePolicy)
			lifeInsurances.GET("", lifeInsuranceHandler.GetPolicies)
			lifeInsurances.GET("/:id", lifeInsuranceHandler.GetByID)
			lifeInsurances.PUT("/:id", lifeInsuranceHandler.UpdatePolicy)
			lifeInsurances.DELETE("/:id", lifeInsuranceHandler.DeletePolicy)
			lifeInsurances.POST("/:id/mark-paid", lifeInsuranceHandler.MarkPremiumPaid)

			// Admin route
			adminLifeInsurance := lifeInsurances.Group("")
			adminLifeInsurance.Use(middleware.RequireRole("admin"))
			{
				adminLifeInsurance.GET("/user/:userId", lifeInsuranceHandler.GetPoliciesByUserIDAdmin)
			}
		}

		// Fixed Deposit / Postal routes — RBAC (client-owns-only vs admin-bypass, plus the
		// admin-only is_mapped modification rule) is enforced inside the service layer, so no
		// RequireRole gate is needed at the router level; every route just requires authentication.
		fixedDeposits := v1.Group("/fixed-deposits")
		{
			fixedDeposits.Use(middleware.RequireAuth(cfg, userRepo))

			fixedDeposits.POST("", fixedDepositHandler.CreateFD)
			fixedDeposits.GET("", fixedDepositHandler.GetFDs)
			fixedDeposits.GET("/:id", fixedDepositHandler.GetByID)
			fixedDeposits.PUT("/:id", fixedDepositHandler.UpdateFD)
			fixedDeposits.DELETE("/:id", fixedDepositHandler.DeleteFD)

			// Admin route
			adminFixedDeposits := fixedDeposits.Group("")
			adminFixedDeposits.Use(middleware.RequireRole("admin"))
			{
				adminFixedDeposits.GET("/user/:userId", fixedDepositHandler.GetFDsByUserIDAdmin)
			}
		}

		// Health Insurance routes — RBAC (client-owns-only vs admin-bypass, plus the admin-only
		// is_mapped modification rule) is enforced inside the service layer, so no RequireRole
		// gate is needed at the router level; every route just requires authentication.
		healthInsurances := v1.Group("/health-insurances")
		{
			healthInsurances.Use(middleware.RequireAuth(cfg, userRepo))

			healthInsurances.POST("", healthInsuranceHandler.CreatePolicy)
			healthInsurances.GET("", healthInsuranceHandler.GetPolicies)
			healthInsurances.GET("/:id", healthInsuranceHandler.GetByID)
			healthInsurances.PUT("/:id", healthInsuranceHandler.UpdatePolicy)
			healthInsurances.DELETE("/:id", healthInsuranceHandler.DeletePolicy)
			healthInsurances.POST("/:id/mark-paid", healthInsuranceHandler.MarkPremiumPaid)

			// Admin route
			adminHealthInsurance := healthInsurances.Group("")
			adminHealthInsurance.Use(middleware.RequireRole("admin"))
			{
				adminHealthInsurance.GET("/user/:userId", healthInsuranceHandler.GetPoliciesByUserIDAdmin)
			}
		}

		// Support Ticket routes — RBAC (client-owns-only vs admin-bypass, plus the
		// Status/AdminNotes client-field-stripping rule) is enforced inside the service layer.
		// DELETE is additionally restricted to super_admin at the router level.
		tickets := v1.Group("/tickets")
		{
			tickets.Use(middleware.RequireAuth(cfg, userRepo))

			tickets.POST("", supportTicketHandler.CreateTicket)
			tickets.GET("", supportTicketHandler.GetTickets)
			tickets.GET("/:id", supportTicketHandler.GetByID)
			tickets.PUT("/:id", supportTicketHandler.UpdateTicket)
			tickets.DELETE("/:id", middleware.RequireRole(domain.RoleSuperAdmin), supportTicketHandler.DeleteTicket)
		}

		// Product Catalog routes — fulfills the "KNOW ABOUT ALL PRODUCT" requirement. GET routes
		// are open to any authenticated role (client/advisor/admin/super_admin) — the service layer
		// forces client/advisor requesters to the published (is_active=true) subset, but lets a
		// plain admin view drafts too. Writes (create/update/delete) are Super Admin only — a plain
		// admin can view the full catalog but never modify it — gated at the router level, with the
		// service layer re-checking the role as defense in depth.
		products := v1.Group("/products")
		{
			products.Use(middleware.RequireAuth(cfg, userRepo))

			products.GET("", productHandler.GetProducts)
			products.GET("/:id", productHandler.GetByID)

			superAdminProducts := products.Group("")
			superAdminProducts.Use(middleware.RequireRole(domain.RoleSuperAdmin))
			{
				superAdminProducts.POST("", productHandler.CreateProduct)
				superAdminProducts.PUT("/:id", productHandler.UpdateProduct)
				superAdminProducts.DELETE("/:id", productHandler.DeleteProduct)
			}
		}

		// Dashboard routes — pure aggregation views over existing repositories, no own collection.
		dashboard := v1.Group("/dashboard")
		{
			dashboard.Use(middleware.RequireAuth(cfg, userRepo))

			clientDashboard := dashboard.Group("")
			clientDashboard.Use(middleware.RequireRole(domain.RoleClient, domain.RoleAdvisor))
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
			reports.Use(middleware.RequireAuth(cfg, userRepo))

			reports.GET("/portfolio", reportHandler.GetClientPortfolio)
		}

		// Agency Sync routes — automated bulk updates from LIC Premium Due List PDFs
		agency := v1.Group("/agency")
		{
			agency.Use(middleware.RequireAuth(cfg, userRepo))
			agency.Use(middleware.RequireRole("admin"))

			agency.POST("/sync/lic-due-list", agencySyncHandler.ProcessLICDueList)

			// Policy inbox — every row imported from a due list, and the action that attaches one
			// to a client account. Both roles reach these: an admin works their own book, a super
			// admin names the agency's book they are working on via `agency_id`.
			agency.GET("/imported-policies", agencySyncHandler.ListImportedPolicies)
			agency.POST("/imported-policies/:id/link", agencySyncHandler.LinkImportedPolicy)
			agency.DELETE("/imported-policies/:id", agencySyncHandler.DeleteImportedPolicy)

			// Deposit side of the same idea: a Post Office report fills the deposit inbox, and
			// linking a row creates the client's Fixed Deposit record.
			agency.POST("/sync/postal-report", agencySyncHandler.ProcessPostalReport)
			agency.GET("/imported-deposits", agencySyncHandler.ListImportedDeposits)
			agency.POST("/imported-deposits/:id/link", agencySyncHandler.LinkImportedDeposit)
			agency.DELETE("/imported-deposits/:id", agencySyncHandler.DeleteImportedDeposit)
		}

		// Financial Calculators routes — SIP, Lumpsum, and FD calculators with Admin rate settings
		calculators := v1.Group("/calculators")
		{
			calculators.Use(middleware.RequireAuth(cfg, userRepo))

			calculators.GET("/settings", calculatorHandler.GetSettings)
			// The default rates are one platform-wide setting shown to every agency's clients, so only a
			// super admin may change them — a plain admin editing them would rewrite other agencies' numbers.
			calculators.PUT("/settings", middleware.RequireRole(domain.RoleSuperAdmin), calculatorHandler.UpdateSettings)
			calculators.POST("/sip", calculatorHandler.CalculateSIP)
			calculators.POST("/lumpsum", calculatorHandler.CalculateLumpsum)
			calculators.POST("/fd", calculatorHandler.CalculateFD)
		}

		// Referral routes — agency staff share a referral code; every client who signs up with it is
		// attributed to them. Clients hold no referral code, so the whole group is staff-only:
		// an admin sees their own referrals, a super admin sees everyone's plus the leaderboard.
		referrals := v1.Group("/referrals")
		{
			referrals.Use(middleware.RequireAuth(cfg, userRepo))
			referrals.Use(middleware.RequireRole(domain.RoleAdmin))

			referrals.GET("/my-stats", referralHandler.GetMyStats)
			referrals.GET("/all", referralHandler.GetAllReferrals)
			referrals.GET("/summary", middleware.RequireRole(domain.RoleSuperAdmin), referralHandler.GetAdminSummary)
		}
	}

	return router
}
