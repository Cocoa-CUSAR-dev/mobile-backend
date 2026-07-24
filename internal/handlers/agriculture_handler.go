package handlers

import (
	"fmt"
	"go-server-mobile/internal/models"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid" // ต้องใช้สำหรับ Assert userID
	"gorm.io/gorm"
)

type AgricultureHandler struct {
	DB *gorm.DB
}

// Request struct สำหรับรับค่าจาก JSON ตาม format ที่คุณให้มา
type RegisterFarmerProfileRequest struct {
	FirstName         string  `json:"first_name" binding:"required"`
	LastName          string  `json:"last_name" binding:"required"`
	Nickname          string  `json:"nickname"`
	BirthDate         string  `json:"birth_date"`
	IdCardNumber      *string `json:"id_card_number" binding:"required"`
	Nationality       *string `json:"nationality"`
	Ethnicity         *string `json:"ethnicity"`
	Religion          *string `json:"religion"`
	AddressDetail     *string `json:"address_detail"`
	ZipCode           string  `json:"zip_code"`
	PhoneNumber       string  `json:"phone_number" binding:"required"`
	Line              *string `json:"line"`
	SalaryIncome      float64 `json:"salary_income"`
	FamilyMemberCount int     `json:"family_member_count"`
	AgriWorkerCount   int     `json:"agri_worker_count"`
	AgriExperience    string  `json:"agri_experience"`
	ProvinceID        string  `json:"province_id" binding:"required"`
	DistrictID        string  `json:"district_id" binding:"required"`
	SubdistrictID     string  `json:"subdistrict_id" binding:"required"`
}

// สำหรับ /farms
type RegisterFarmRequest struct {
	FarmName      string  `json:"farm_name" binding:"required"`
	HubID         *string `json:"hub_id"`
	FoundDate     string  `json:"found_date"`
	AddressDetail *string `json:"address_detail"`
	ProvinceID    *string `json:"province_id"`
	DistrictID    *string `json:"district_id"`
	SubdistrictID string  `json:"subdistrict_id" binding:"required"`
	ZipCode       string  `json:"zip_code" binding:"required"`
	ContactName   string  `json:"contact_name" binding:"required"`
	PhoneNumber   string  `json:"phone_number" binding:"required"`
	Line          *string `json:"line"`
	Facebook      *string `json:"facebook"`
	// GIS Data (ส่งมาเป็น list ของ lat, lng)
	GIS []map[string]float64 `json:"gis"`
}

// สำหรับ /plots
type RegisterPlotRequest struct {
	FarmID           string  `json:"farm_id" binding:"required"`
	PlotName         string  `json:"plot_name" binding:"required"`
	LandOwnership    string  `json:"land_ownership" binding:"required"`
	CocoaPlantedArea float64 `json:"cocoa_planted_area" binding:"required"`
	HasChemicalUse   bool    `json:"has_chemical_use"`
	LandTypeID       *string `json:"land_type_id"`
	SoilTypeID       *string `json:"soil_type_id"`
	WaterSourceID    *string `json:"water_source_id"`
	WateringSystemID *string `json:"watering_system_id"`
	FoundDate        string  `json:"found_date"`
	// GIS Data
	GIS     []map[string]float64 `json:"gis"`
	AreaSqM float64              `json:"gis_area_m2"`
}

func _parseDate(dateStr string) time.Time {
	if dateStr == "" {
		return time.Now()
	}
	t, _ := time.Parse("2006-01-02", dateStr)
	return t
}

// buildWKT turns a slice of {lng, lat} points into a PostGIS WKT string.
//   - 0 or 1 points: empty string (caller should skip the insert; a polygon
//     needs at least 3 vertices, and a single point is handled separately
//     by the farm handler as POINT, not here).
//   - 2+ points: POLYGON with the first point repeated as the closing vertex,
//     as required by the WKT spec.
//
// Keeping this in one place means the closing-vertex rule is testable in
// isolation instead of buried inside RegisterFarm/RegisterPlot.
func buildWKT(points []map[string]float64) string {
	if len(points) < 2 {
		return ""
	}
	coords := make([]string, 0, len(points)+1)
	for _, p := range points {
		coords = append(coords, fmt.Sprintf("%f %f", p["lng"], p["lat"]))
	}
	// Close the ring: last coord must equal the first.
	coords = append(coords, fmt.Sprintf("%f %f", points[0]["lng"], points[0]["lat"]))
	return fmt.Sprintf("POLYGON((%s))", strings.Join(coords, ","))
}

func (h *AgricultureHandler) RegisterFarmerProfile(c *gin.Context) {
	// 1. ดึง user_id จาก context
	val, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: No user session found"})
		return
	}
	userID := val.(uuid.UUID)

	var req RegisterFarmerProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ข้อมูลไม่ถูกต้อง: " + err.Error()})
		return
	}

	// เตรียมข้อมูล Farmer
	farmerData := map[string]interface{}{
		"user_id":             userID,
		"first_name":          req.FirstName,
		"last_name":           req.LastName,
		"nickname":            req.Nickname,
		"birth_date":          _parseDate(req.BirthDate),
		"id_card_number":      req.IdCardNumber,
		"nationality":         req.Nationality,
		"ethnicity":           req.Ethnicity,
		"religion":            req.Religion,
		"address_detail":      req.AddressDetail,
		"subdistrict_id":      req.SubdistrictID,
		"district_id":         req.DistrictID,
		"province_id":         req.ProvinceID,
		"zip_code":            req.ZipCode,
		"phone_number":        req.PhoneNumber,
		"line":                req.Line,
		"salary_income":       req.SalaryIncome,
		"family_member_count": req.FamilyMemberCount,
		"agri_worker_count":   req.AgriWorkerCount,
		"agri_experience":     _parseDate(req.AgriExperience),
		"created_at":          time.Now(),
		"updated_at":          time.Now(),
	}

	// 3. เริ่ม Transaction เพื่อบันทึกข้อมูล Farmer และ Role พร้อมกัน
	tx := h.DB.Begin()

	// A. บันทึกลง agriculture.farmer
	if err := tx.Table("agriculture.farmer").Create(&farmerData).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusConflict, gin.H{"error": "คุณได้ลงทะเบียนโปรไฟล์เกษตรกรไปแล้ว หรือข้อมูลไม่ถูกต้อง"})
		return
	}

	// B. เพิ่ม Role 'farmer' ในตาราง auth.user_role
	// ใช้ UUID: bbbbbbbb-2222-2222-0003-bbbbbbbbbbbb (ตามโจทย์)
	farmerRoleID, _ := uuid.Parse("bbbbbbbb-2222-2222-0003-bbbbbbbbbbbb")
	userRoleData := map[string]interface{}{
		"user_id": userID,
		"role_id": farmerRoleID,
	}

	// ใช้คำสั่ง Clause OnConflict เพื่อป้องกัน Error กรณีมี Role นี้อยู่แล้ว (Idempotent)
	if err := tx.Table("auth.user_role").Create(&userRoleData).Error; err != nil {
		tx.Rollback()
		fmt.Println("ไม่สามารถเพิ่ม Role Farmer ได้:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "เกิดข้อผิดพลาดในการกำหนดสิทธิ์"})
		return
	}

	// 4. Commit ข้อมูลทั้งหมดลง Database
	if err := tx.Commit().Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	// 5. Response กลับ
	c.JSON(http.StatusOK, req)
}

func (h *AgricultureHandler) RegisterFarm(c *gin.Context) {
	val, _ := c.Get("userID")
	userID := val.(uuid.UUID)

	var req RegisterFarmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ข้อมูลไม่ถูกต้อง: " + err.Error()})
		return
	}

	tx := h.DB.Begin() // เริ่ม Transaction

	// 1. บันทึกข้อมูลฟาร์ม (Table: agriculture.farm)
	farmID := uuid.New()
	farmData := map[string]interface{}{
		"farm_id":        farmID,
		"farm_name":      req.FarmName,
		"hub_id":         req.HubID,
		"found_date":     _parseDate(req.FoundDate),
		"address_detail": req.AddressDetail,
		"province_id":    req.ProvinceID,
		"district_id":    req.DistrictID,
		"subdistrict_id": req.SubdistrictID,
		"zip_code":       req.ZipCode,
		"contact_name":   req.ContactName,
		"phone_number":   req.PhoneNumber,
		"line":           req.Line,
		"facebook":       req.Facebook,
		"created_at":     time.Now(),
		"updated_at":     time.Now(),
	}

	if err := tx.Table("agriculture.farm").Create(&farmData).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "บันทึกข้อมูลฟาร์มล้มเหลว"})
		return
	}

	// 2. บันทึกความสัมพันธ์เจ้าของฟาร์ม (Table: agriculture.farmer_farm)
	// ส่วนนี้สำคัญเพื่อให้ GET /farms กรองข้อมูลได้
	farmerFarmData := map[string]interface{}{
		"farmer_id": userID,
		"farm_id":   farmID,
	}

	if err := tx.Table("agriculture.farmer_farm").Create(&farmerFarmData).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "เชื่อมโยงเจ้าของฟาร์มล้มเหลว"})
		return
	}

	// 3. จัดการข้อมูล GIS (Table: storage.geo)
	if len(req.GIS) > 0 {
		var wkt string
		if len(req.GIS) == 1 {
			// กรณีส่งมาจุดเดียว -> POINT
			wkt = fmt.Sprintf("POINT(%f %f)", req.GIS[0]["lng"], req.GIS[0]["lat"])
		} else {
			wkt = buildWKT(req.GIS)
		}

		if wkt != "" {
			query := `
				INSERT INTO "storage".geo (uploaded_by, geom, code, source_type, created_at)
				VALUES (?, ST_GeomFromText(?, 4326), ?, ?, ?)`

			if err := tx.Exec(query, userID, wkt, farmID.String(), "farm", time.Now()).Error; err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "บันทึกพิกัดภูมิศาสตร์ล้มเหลว"})
				return
			}
		}
	}

	// Commit การเปลี่ยนแปลงทั้งหมด
	if err := tx.Commit().Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Commit ข้อมูลล้มเหลว"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "ลงทะเบียนฟาร์มและเจ้าของสำเร็จ",
		"farm_id": farmID,
	})
}

func (h *AgricultureHandler) RegisterPlot(c *gin.Context) {
	val, _ := c.Get("userID")
	userID := val.(uuid.UUID)

	var req RegisterPlotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tx := h.DB.Begin()
	// 1. บันทึกข้อมูลแปลง (Plot)
	plotID := uuid.New()
	plotData := map[string]interface{}{
		"plot_id":            plotID,
		"farm_id":            req.FarmID,
		"plot_name":          req.PlotName,
		"total_area":         req.AreaSqM / 1600, // แปลง ตร.ม. เป็น ไร่ (โดยประมาณ)
		"land_ownership":     req.LandOwnership,
		"cocoa_planted_area": req.CocoaPlantedArea,
		"has_chemical_use":   req.HasChemicalUse,
		"land_type_id":       req.LandTypeID,
		"soil_type_id":       req.SoilTypeID,
		"water_source_id":    req.WaterSourceID,
		"watering_system_id": req.WateringSystemID,
		"found_date":         _parseDate(req.FoundDate),
		"created_at":         time.Now(),
		"updated_at":         time.Now(),
	}

	if err := tx.Table("agriculture.plot").Create(&plotData).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "บันทึกข้อมูลแปลงล้มเหลว"})
		return
	}

	// 2. บันทึก GIS (Polygon สำหรับ Plot)
	if len(req.GIS) >= 2 {
		wkt := buildWKT(req.GIS)
		query := "INSERT INTO \"storage\".geo (uploaded_by, geom, code, source_type, area_sq_m, created_at) VALUES (?, ST_GeomFromText(?, 4326), ?, ?, ?, ?)"
		if err := tx.Exec(query, userID, wkt, plotID.String(), "plot", req.AreaSqM, time.Now()).Error; err != nil {
			tx.Rollback()
			fmt.Println("Geo Error:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "บันทึกพิกัดแปลงล้มเหลว"})
			return
		}
	}

	tx.Commit()
	c.JSON(http.StatusOK, gin.H{"message": "ลงทะเบียนแปลงสำเร็จ", "plot_id": plotID})
}

func (h *AgricultureHandler) GetMyFarms(c *gin.Context) {
	val, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	userID := val.(uuid.UUID)

	var farms []models.Farm

	// 1. ใช้ Joins ตารางกลาง (FarmerFarm) เพื่อกรองข้อมูล
	// 2. ใช้ Preload("Plots") ตรงๆ โดยไม่ต้องใช้ .Table() ข้างใน
	err := h.DB.Debug().Model(&models.Farm{}).
		Table("agriculture.farm").
		Joins("JOIN agriculture.farmer_farm ON agriculture.farmer_farm.farm_id = agriculture.farm.farm_id").
		Where("agriculture.farmer_farm.farmer_id = ?", userID).
		Preload("Plots"). // <--- เรียกชื่อ Field ใน Struct ตรงๆ
		Find(&farms).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ดึงข้อมูลล้มเหลว: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, farms)
}

func (h *AgricultureHandler) GetMyPlots(c *gin.Context) {
	// 1. ดึง userID จาก Context
	val, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	userID := val.(uuid.UUID)

	var plots []models.Plot

	// 2. Query แปลงปลูกโดย Join ผ่านตาราง Farm และ FarmerFarm
	err := h.DB.Table("agriculture.plot").
		// Join ไปที่ Farm เพื่อเชื่อมความสัมพันธ์
		Joins("JOIN agriculture.farm ON agriculture.farm.farm_id = agriculture.plot.farm_id").
		// Join ไปที่ FarmerFarm เพื่อเช็คสิทธิ์เจ้าของ
		Joins("JOIN agriculture.farmer_farm ON agriculture.farmer_farm.farm_id = agriculture.farm.farm_id").
		// กรองเฉพาะที่เป็นของ User คนนี้
		Where("agriculture.farmer_farm.farmer_id = ?", userID).
		// จัดกลุ่มข้อมูล (Optional: กรณีมีการ Join ซ้ำซ้อน)
		Group("agriculture.plot.plot_id").
		Find(&plots).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ดึงข้อมูลแปลงปลูกล้มเหลว: " + err.Error()})
		return
	}

	// 3. ส่งข้อมูล List ของ Plots กลับไป
	c.JSON(http.StatusOK, plots)
}
