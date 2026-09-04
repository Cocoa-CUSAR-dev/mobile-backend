package main

import (
	"fmt"
	"go-server-mobile/internal/database"
	"go-server-mobile/internal/handlers"
	"go-server-mobile/internal/middleware"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	// 1. โหลด Environment
	err := godotenv.Load()
	if err != nil {
		fmt.Printf("ไม่พบไฟล์ .env\n")
	}

	fmt.Println("JWT_NAME in main:", os.Getenv("JWT_NAME"))

	// 2. เชื่อมต่อ Database
	db := database.InitDB()

	// 3. Initialize Handlers
	authHandler := &handlers.AuthHandler{DB: db}
	agricultureHandler := &handlers.AgricultureHandler{DB: db}
	refHandler := &handlers.RefHandler{DB: db}
	formHandler := &handlers.FormHandler{DB: db}

	// เพิ่ม Collection และ Processing Handlers
	collectionHandler := &handlers.CollectionHandler{DB: db}
	processingHandler := &handlers.ProcessingHandler{DB: db}

	// 4. Setup Router
	r := gin.Default()

	// LIFF test kit — ดูรายละเอียดที่ static/liff-test/README.md
	// r.StaticFile("/liff-test", "./static/liff-test/index.html")
	// r.StaticFile("/liff-link", "./static/liff-test/link.html")

	// CORS: only needed for browser-based callers (e.g. a Flutter web build).
	// Native mobile HTTP clients ignore CORS entirely, so this was invisible
	// until something running in a browser tried to call this API directly.
	// gin-contrib/cors panics at startup if AllowCredentials is true with zero
	// allowed origins, so only attach the middleware when there's something to allow —
	// same-origin callers (e.g. static/liff-test/*.html served by this same server)
	// don't need CORS at all.
	var corsOrigins []string
	if raw := os.Getenv("CORS_ALLOWED_ORIGINS"); raw != "" {
		corsOrigins = strings.Split(raw, ",")
	}
	if len(corsOrigins) > 0 {
		r.Use(cors.New(cors.Config{
			AllowOrigins:     corsOrigins,
			AllowMethods:     []string{"GET", "POST", "PUT", "DELETE"},
			AllowHeaders:     []string{"Content-Type", "Authorization"},
			AllowCredentials: true,
		}))
	}

	// --- Public Routes ---
	public := r.Group("/public")
	{
		public.POST("/login", authHandler.Login)
		public.POST("/register", authHandler.Register)
		public.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "hello world"})
		})

		public.POST("/liff/verify", authHandler.VerifyLiffToken)
		public.POST("/liff/link", authHandler.LinkLineAccount)
	}

	// --- Protected Routes (ต้องผ่าน JWT) ---
	protected := r.Group("/")
	protected.Use(middleware.JwtAuthMiddleware())
	{
		// --- 0. อื่นๆ --
		protected.GET("/auth/me", authHandler.GetMe)
		protected.GET("/constants/:key", refHandler.GetConstants)
		// --- 1. เกษตรกร (Agriculture) ---
		// RegisterFarmerProfile is the onboarding endpoint that GRANTS the
		// "farmer" role (see agriculture_handler.go) — it must stay open to
		// any authenticated user, not gated behind the role it hands out.
		protected.POST("/farmers", agricultureHandler.RegisterFarmerProfile)
		protected.POST("/farms", middleware.RequireRole("farmer"), agricultureHandler.RegisterFarm)
		protected.POST("/plots", middleware.RequireRole("farmer"), agricultureHandler.RegisterPlot)
		protected.GET("/farms", middleware.RequireRole("farmer"), agricultureHandler.GetMyFarms)
		protected.GET("/plots", middleware.RequireRole("farmer"), agricultureHandler.GetMyPlots)

		// --- 2. หน่วยรวบรวม (Collection) ---
		// RegisterHubCollector grants the "hub_collector" role — same
		// onboarding exception as RegisterFarmerProfile above.
		protected.POST("/hub_collectors", collectionHandler.RegisterHubCollector)
		protected.POST("/hubs", middleware.RequireRole("hub_collector"), collectionHandler.RegisterHub)
		protected.GET("/hubs", middleware.RequireRole("hub_collector"), collectionHandler.GetMyHub) // มาพร้อม harvests ในตัว
		protected.GET("/harvests", middleware.RequireRole("hub_collector"), collectionHandler.GetMyHarvests)

		// --- 3. การแปรรูป (Processing) ---
		// RegisterProcessor grants the "processor" role — same onboarding
		// exception as above.
		protected.POST("/processors", processingHandler.RegisterProcessor)
		protected.POST("/processing_stations", middleware.RequireRole("processor"), processingHandler.RegisterStation)
		protected.GET("/processing_stations", middleware.RequireRole("processor"), processingHandler.GetMyProcessingStation) // มาพร้อม batches ในตัว
		protected.GET("/batches", middleware.RequireRole("processor"), processingHandler.GetMyBatches)

		// --- 4. งาน (Tasks/Forms) ---
		protected.GET("/tasks", formHandler.GetTasks)
		protected.POST("/tasks", formHandler.SubmitTask)
		protected.GET("/tasks/:taskId", formHandler.GetTaskResponse)
		protected.GET("/tasks/:taskId/form", formHandler.GetTaskForm)
		protected.PUT("/tasks", formHandler.UpdateTaskResponse)
	}

	// --- Service Routes (trusted first-party services, e.g. the chatbot —
	// separate trust model from farmer JWT sessions, see
	// middleware.ServiceAuthMiddleware) ---
	service := r.Group("/service")
	service.Use(middleware.ServiceAuthMiddleware())
	{
		service.POST("/tasks", formHandler.SubmitTaskForUser)
		service.GET("/tasks/last-answer", formHandler.GetLastAnswer)
		service.POST("/autofill/sanitize", handlers.SanitizeAutofill)
	}

	// 5. Start Server
	if err := r.Run(":8080"); err != nil {
		log.Fatalf("server failed to start: %v", err)
	}
}
