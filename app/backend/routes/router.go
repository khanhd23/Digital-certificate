package routes

import (
	"os"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/vnkmasc/Kmasc/app/backend/internal/handlers"
	"github.com/vnkmasc/Kmasc/app/backend/internal/middleware"
	"go.mongodb.org/mongo-driver/mongo"
)

func SetupRouter(
	db *mongo.Database,
	userHandler *handlers.UserHandler,
	authHandler *handlers.AuthHandler,
	universityHandler *handlers.UniversityHandler,
	facultyHandler *handlers.FacultyHandler,
	blockchainHandler *handlers.BlockchainHandler,
	majorHandler *handlers.MajorHandler,
	templateHandler *handlers.TemplateHandler,
	ediplomaHandler *handlers.EDiplomaHandler,
	templateSampleHandler *handlers.TemplateSampleHandler,
	pqcHandler *handlers.PQCHandler,
	portal *handlers.DegreePortal,
) *gin.Engine {
	r := gin.Default()

	// Only trust X-Forwarded-For from explicitly configured proxies; otherwise
	// clients could spoof their IP and bypass the rate limits below.
	var trustedProxies []string
	if raw := strings.TrimSpace(os.Getenv("TRUSTED_PROXIES")); raw != "" {
		trustedProxies = strings.Split(raw, ",")
	}
	_ = r.SetTrustedProxies(trustedProxies)

	allowOrigins := []string{"http://localhost:3000"}
	if appURL := strings.TrimSpace(os.Getenv("APP_URL")); appURL != "" && appURL != allowOrigins[0] {
		allowOrigins = append(allowOrigins, appURL)
	}
	r.Use(cors.New(cors.Config{
		AllowOrigins:     allowOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
	}))

	jwt := middleware.JWTAuthMiddleware()
	admin := middleware.RequireRoles(middleware.RoleAdmin)
	school := middleware.RequireRoles(middleware.RoleUniversityAdmin)
	student := middleware.RequireRoles(middleware.RoleStudent)
	owned := func(collection, param string) gin.HandlerFunc {
		return middleware.Owned(middleware.Ownership{DB: db, Collection: collection, Param: param})
	}

	api := r.Group("/api/v1")
	api.Use(portal.Audit())

	// ===== Public verification =====
	api.GET("/public/degrees/:id", middleware.RateLimit(60, time.Minute), portal.Public)
	api.GET("/pqc/ediplomas/:id/verify", middleware.RateLimit(60, time.Minute), pqcHandler.VerifyEDiploma)

	// ===== Auth (public) =====
	authPublic := api.Group("/auth")
	authPublic.POST("/login", middleware.RateLimit(10, time.Minute), authHandler.Login)
	authPublic.POST("/request-otp", middleware.RateLimit(5, 10*time.Minute), authHandler.RequestOTP)
	authPublic.POST("/verify-otp", middleware.RateLimit(10, 10*time.Minute), authHandler.VerifyOTP)
	authPublic.POST("/register", middleware.RateLimit(10, 10*time.Minute), authHandler.Register)
	authPublic.POST("/activate-account", middleware.RateLimit(10, 10*time.Minute), authHandler.ActivateAccount)
	authPublic.POST("/forgot-password", middleware.RateLimit(5, 10*time.Minute), authHandler.ForgotPassword)
	authPublic.POST("/reset-password", middleware.RateLimit(10, 10*time.Minute), authHandler.ResetPassword)

	// ===== Auth (any signed-in account) =====
	authPrivate := api.Group("/auth", jwt)
	authPrivate.POST("/change-password", authHandler.ChangePassword)

	// ===== Platform admin =====
	adminAccounts := api.Group("/auth", jwt, admin)
	adminAccounts.GET("/accounts", authHandler.GetAllAccounts)
	adminAccounts.GET("/university-admin-info", authHandler.GetUniversityAdmins)
	adminAccounts.GET("/students-info", authHandler.GetStudentAccounts)

	api.GET("/admin/overview", jwt, admin, universityHandler.Overview)
	api.GET("/admin/audit", jwt, admin, universityHandler.RecentAdminEvents)

	universityGroup := api.Group("/universities", jwt, admin)
	universityGroup.GET("", universityHandler.GetAllUniversities)
	universityGroup.POST("", universityHandler.CreateUniversity)
	universityGroup.GET("/:id", universityHandler.GetUniversity)
	universityGroup.PUT("/:id", universityHandler.UpdateUniversity)
	universityGroup.POST("/:id/lock", universityHandler.LockUniversity)
	universityGroup.POST("/:id/unlock", universityHandler.UnlockUniversity)
	universityGroup.POST("/:id/resend-activation", universityHandler.ResendActivation)

	// ===== School admin: own university (read only) =====
	api.GET("/school/profile", jwt, school, universityHandler.GetMySchool)

	// ===== Student self-service (read only) =====
	api.GET("/users/me", jwt, student, userHandler.GetMyProfile)
	api.GET("/ediplomas/simple", jwt, student, ediplomaHandler.GetMyEDiplomaNames)

	// ===== University admin: students =====
	userGroup := api.Group("/users", jwt, school)
	userGroup.GET("/search", userHandler.SearchUsers)
	userGroup.POST("", userHandler.CreateUser)
	userGroup.POST("/import-excel", userHandler.ImportUsersFromExcel)
	userGroup.GET("/:id", owned("users", "id"), userHandler.GetUserByID)
	userGroup.PUT("/:id", owned("users", "id"), userHandler.UpdateUser)
	userGroup.DELETE("/:id", owned("users", "id"), userHandler.DeleteUser)

	// ===== University admin: faculties and majors =====
	facultyGroup := api.Group("/faculties", jwt, school)
	facultyGroup.POST("", facultyHandler.CreateFaculty)
	facultyGroup.GET("", facultyHandler.GetAllFaculties)
	facultyGroup.GET("/:id", owned("faculties", "id"), facultyHandler.GetFacultyByID)
	facultyGroup.PUT("/:id", owned("faculties", "id"), facultyHandler.UpdateFaculty)
	facultyGroup.DELETE("/:id", owned("faculties", "id"), facultyHandler.DeleteFaculty)

	majorGroup := api.Group("/majors", jwt, school)
	majorGroup.POST("", majorHandler.CreateMajor)
	majorGroup.GET("/faculty/:faculty_id", owned("faculties", "faculty_id"), majorHandler.GetMajorsByFaculty)
	majorGroup.DELETE("/:id", owned("majors", "id"), majorHandler.DeleteMajor)

	// ===== University admin: diploma templates =====
	templateGroup := api.Group("/templates", jwt, school)
	templateGroup.POST("", templateHandler.CreateTemplate)
	templateGroup.GET("/faculty", templateHandler.GetTemplates)
	templateGroup.GET("/:id", owned("diploma_templates", "id"), templateHandler.GetTemplateByID)
	templateGroup.PUT("/:template_id", owned("diploma_templates", "template_id"), templateHandler.UpdateDiplomaTemplate)

	sharedSample := middleware.Owned(middleware.Ownership{DB: db, Collection: "template_samples", Param: "id", Shared: true})
	templateSampleGroup := api.Group("/template-samples", jwt, school)
	templateSampleGroup.POST("", templateSampleHandler.CreateTemplateSample)
	templateSampleGroup.GET("", templateSampleHandler.GetAllTemplateSamples)
	templateSampleGroup.GET("/:id", sharedSample, templateSampleHandler.GetTemplateSampleByID)
	templateSampleGroup.GET("/view/:id", sharedSample, templateSampleHandler.GetTemplateSampleView)
	templateSampleGroup.PUT("/:id", owned("template_samples", "id"), templateSampleHandler.UpdateTemplateSample)

	// ===== Digital diplomas =====
	// Students may read only their own diplomas; everything else is university admin only.
	ownDiploma := middleware.Owned(middleware.Ownership{DB: db, Collection: "ediplomas", Param: "id", StudentField: "user_id"})
	readers := middleware.RequireRoles(middleware.RoleUniversityAdmin, middleware.RoleStudent)
	api.GET("/ediplomas/file/:id", jwt, readers, ownDiploma, ediplomaHandler.ViewEDiplomaFile)

	ediplomaGroup := api.Group("/ediplomas", jwt, school)
	ediplomaGroup.GET("/search", ediplomaHandler.SearchEDiplomas)
	ediplomaGroup.POST("", ediplomaHandler.CreateEDiploma)
	ediplomaGroup.POST("/import-excel", ediplomaHandler.ImportEDiplomasFromExcel)
	ediplomaGroup.POST("/issue-pqc-batch", portal.IssueBatch)
	ediplomaGroup.POST("/assign-round", ediplomaHandler.AssignRound)
	ediplomaGroup.POST("/revoke-batch", ediplomaHandler.RevokeRound)
	ediplomaGroup.POST("/generate", ediplomaHandler.GenerateEDiploma)
	ediplomaGroup.POST("/:id/replace", portal.Replace)
	ediplomaGroup.POST("/:id/revoke", ediplomaHandler.RevokeEDiploma)
	api.GET("/ediplomas/:id", jwt, readers, ownDiploma, ediplomaHandler.GetEDiplomaByID)

	// ===== Issuance rounds (named graduate lists) =====
	roundGroup := api.Group("/issuance-rounds", jwt, school)
	roundGroup.GET("", ediplomaHandler.ListRounds)
	roundGroup.POST("", ediplomaHandler.CreateRound)

	api.GET("/degree-summary", jwt, school, portal.Summary)
	api.GET("/degree-audit", jwt, school, portal.AuditList)

	// ===== Blockchain anchoring =====
	blockchainGroup := api.Group("/blockchain", jwt, school)
	blockchainGroup.POST("/push-ediploma", blockchainHandler.PushEDiplomasToBlockchain)
	blockchainGroup.POST("/push-revocations", blockchainHandler.PushRevocationsToBlockchain)

	// ===== Post-quantum signing (ML-DSA-65) =====
	pqcPrivate := api.Group("/pqc", jwt, school)
	pqcPrivate.GET("/keys", pqcHandler.ListKeys)
	pqcPrivate.POST("/keys", pqcHandler.CreateKey)
	pqcPrivate.POST("/keys/:id/activate", pqcHandler.ActivateKey)
	pqcPrivate.POST("/keys/:id/revoke", pqcHandler.RevokeKey)
	pqcPrivate.POST("/ediplomas/:id/sign", owned("ediplomas", "id"), pqcHandler.SignEDiploma)
	pqcPrivate.POST("/ediplomas/sign-batch", pqcHandler.SignEDiplomaBatch)
	return r
}
