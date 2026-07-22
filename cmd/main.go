package main

import (
	"fmt"
	"go-server-mobile/internal/database"
	"go-server-mobile/internal/handlers"
	"go-server-mobile/internal/middleware"
	"net/http"
	"os"

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

	// --- Public Routes ---
	public := r.Group("/public")
	{
		public.POST("/login", authHandler.Login)
		public.POST("/register", authHandler.Register)
		public.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "hello world"})
		})
	}

	// --- Protected Routes (ต้องผ่าน JWT) ---
	protected := r.Group("/")
	protected.Use(middleware.JwtAuthMiddleware())
	{
		// --- 0. อื่นๆ --
		protected.GET("/auth/me", authHandler.GetMe)
		protected.GET("/constants/:key", refHandler.GetConstants)
		// --- 1. เกษตรกร (Agriculture) ---
		protected.POST("/farmers", agricultureHandler.RegisterFarmerProfile)
		protected.POST("/farms", agricultureHandler.RegisterFarm)
		protected.POST("/plots", agricultureHandler.RegisterPlot)
		protected.GET("/farms", agricultureHandler.GetMyFarms)
		protected.GET("/plots", agricultureHandler.GetMyPlots)

		// --- 2. หน่วยรวบรวม (Collection) ---
		protected.POST("/hub_collectors", collectionHandler.RegisterHubCollector)
		protected.POST("/hubs", collectionHandler.RegisterHub)
		protected.GET("/hubs", collectionHandler.GetMyHub) // มาพร้อม harvests ในตัว
		protected.GET("/harvests", collectionHandler.GetMyHarvests)

		// --- 3. การแปรรูป (Processing) ---
		protected.POST("/processors", processingHandler.RegisterProcessor)
		protected.POST("/processing_stations", processingHandler.RegisterStation)
		protected.GET("/processing_stations", processingHandler.GetMyProcessingStation) // มาพร้อม batches ในตัว
		protected.GET("/batches", processingHandler.GetMyBatches)

		// --- 4. งาน (Tasks/Forms) ---
		protected.GET("/tasks", formHandler.GetTasks)
		protected.POST("/tasks", formHandler.SubmitTask)
		protected.GET("/tasks/:taskId", formHandler.GetTaskResponse)
		protected.PUT("/tasks", formHandler.UpdateTaskResponse)
	}
	// 5. Start Server
	r.Run(":8080")
}
