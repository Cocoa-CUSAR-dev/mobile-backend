package main

import (
	"fmt"
	"go-server-mobile/internal/database"
	"go-server-mobile/internal/handlers"
	"go-server-mobile/internal/middleware"
	"go-server-mobile/internal/requestid"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	sentrygin "github.com/getsentry/sentry-go/gin"
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

	// X-2d: error tracking. An empty Dsn disables the SDK entirely (no
	// error, no panic) -- safe to call unconditionally in local dev/CI
	// where SENTRY_DSN isn't set.
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:              os.Getenv("SENTRY_DSN"),
		Environment:      os.Getenv("SENTRY_ENVIRONMENT"),
		TracesSampleRate: 0.0,
	}); err != nil {
		// Init only errors on a malformed DSN, not a missing one -- worth
		// surfacing since it means the SDK silently isn't capturing.
		fmt.Printf("sentry.Init failed: %v\n", err)
	}
	defer sentry.Flush(2 * time.Second)

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
	// gin.Default() is gin.New() + gin.Logger() + gin.Recovery(). We build
	// the same stack by hand because gin.Logger()'s format is fixed and has
	// no slot for the request ID: without this, X-Request-Id propagates
	// correctly between services and still never appears in a log line,
	// which leaves nothing to correlate. Same reason web-backend sets
	// logging.pattern.level and chatbot installs a logging filter.
	r := gin.New()
	// X-2e: assign/accept a correlation ID before anything else runs, so
	// the logger below and every handler can see it.
	r.Use(requestid.Middleware())
	r.Use(gin.LoggerWithFormatter(func(p gin.LogFormatterParams) string {
		return fmt.Sprintf("[GIN] %s |%3d| %13v | %15s | %-7s %#v | request_id=%s\n%s",
			p.TimeStamp.Format("2006/01/02 - 15:04:05"),
			p.StatusCode,
			p.Latency,
			p.ClientIP,
			p.Method,
			p.Path,
			requestid.FromKeys(p.Keys),
			p.ErrorMessage,
		)
	}))
	r.Use(gin.Recovery())
	// X-2d: reports panics recovered by gin.Recovery() above to Sentry --
	// registered after it (so it sees the panic before Recovery's own defer
	// does, same relative order as the documented gin.Default()+sentrygin
	// pattern) -- a no-op when the SDK is disabled.
	r.Use(sentrygin.New(sentrygin.Options{}))

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
			AllowOrigins: corsOrigins,
			AllowMethods: []string{"GET", "POST", "PUT", "DELETE"},
			// X-Request-Id has to be listed in both: without AllowHeaders the
			// browser's preflight rejects the whole request as soon as a web
			// caller starts sending one (the first hop requestid documents),
			// and without ExposeHeaders the echoed value is invisible to JS,
			// so the caller cannot log the ID it was given.
			AllowHeaders:     []string{"Content-Type", "Authorization", requestid.Header},
			ExposeHeaders:    []string{requestid.Header},
			AllowCredentials: true,
		}))
	}

	// --- Public Routes ---
	public := r.Group("/public")
	{
		public.POST("/login", authHandler.Login)
		public.POST("/register", authHandler.Register)
		// GO-3: exchanges a still-valid refresh token for a new access
		// token, public since the whole point is to work once the access
		// token itself has already expired.
		public.POST("/refresh", authHandler.RefreshToken)
		public.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "hello world"})
		})
		// GO-5: a real health check -- pings the DB instead of returning a
		// static 200 regardless of whether the app can actually serve traffic.
		public.GET("/health", func(c *gin.Context) {
			sqlDB, err := db.DB()
			if err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "error": err.Error()})
				return
			}
			if err := sqlDB.PingContext(c.Request.Context()); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
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
		protected.GET("/tasks/:taskId/responses", formHandler.GetTaskResponses)
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
	// GO-5: was hardcoded ":8080" -- Render already injects PORT (see
	// render.yaml), this just stopped listening for it. Same "env var with
	// inline default" idiom as DB_SSLMODE in internal/database/postgres.go.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("server failed to start: %v", err)
	}
}
