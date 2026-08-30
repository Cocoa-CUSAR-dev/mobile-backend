package handlers

import (
	"go-server-mobile/internal/models"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ProcessingHandler struct {
	DB *gorm.DB
}

// --- Request Structs ---

type RegisterProcessorRequest struct {
	FirstName     *string `json:"first_name"`
	LastName      *string `json:"last_name"`
	Nickname      *string `json:"nickname"`
	BirthDate     string  `json:"birth_date"` // จะถูกครอบด้วย _parseDate ตอนบันทึก
	IdCardNumber  *string `json:"id_card_number"`
	AddressDetail *string `json:"address_detail"`
	ProvinceID    *string `json:"province_id"`
	DistrictID    *string `json:"district_id"`
	SubdistrictID *string `json:"subdistrict_id"`
	PhoneNumber   *string `json:"phone_number"`
}

type RegisterStationRequest struct {
	StationName   *string              `json:"processing_station_name"`
	HubID         *string              `json:"hub_id"`
	AddressDetail *string              `json:"address_detail"`
	ProvinceID    *string              `json:"province_id"`
	DistrictID    *string              `json:"district_id"`
	SubdistrictID *string              `json:"subdistrict_id"`
	GIS           []map[string]float64 `json:"gis"`
}

// --- Functions ---

func (h *ProcessingHandler) RegisterProcessor(c *gin.Context) {
	val, _ := c.Get("userID")
	userID := val.(uuid.UUID)

	var req RegisterProcessorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tx := h.DB.Begin()

	// 1. บันทึก Processor
	processorData := map[string]interface{}{
		"user_id":        userID,
		"first_name":     req.FirstName,
		"last_name":      req.LastName,
		"nickname":       req.Nickname,
		"birth_date":     _parseDate(req.BirthDate),
		"id_card_number": req.IdCardNumber,
		"address_detail": req.AddressDetail,
		"province_id":    req.ProvinceID,
		"district_id":    req.DistrictID,
		"subdistrict_id": req.SubdistrictID,
		"phone_number":   req.PhoneNumber,
		"created_at":     time.Now(),
		"updated_at":     time.Now(),
	}

	if err := tx.Table("processing.processor").Create(&processorData).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusConflict, gin.H{"error": "ลงทะเบียนผู้แปรรูปซ้ำ"})
		return
	}

	// 2. เพิ่ม Role 'processor' (UUID: ...0005)
	roleID, _ := uuid.Parse("bbbbbbbb-2222-2222-0005-bbbbbbbbbbbb")
	tx.Exec("INSERT INTO auth.user_role (user_id, role_id) VALUES (?, ?) ON CONFLICT DO NOTHING", userID, roleID)

	tx.Commit()

	// Re-sign the session cookie so it carries the "processor" role that
	// was just granted — see reissueTokenCookie's doc comment.
	_ = reissueTokenCookie(c, h.DB, userID)

	c.JSON(http.StatusOK, gin.H{"message": "ลงทะเบียน Processor สำเร็จ"})
}

func (h *ProcessingHandler) RegisterStation(c *gin.Context) {
	val, _ := c.Get("userID")
	userID := val.(uuid.UUID)

	var req RegisterStationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tx := h.DB.Begin()
	stationID := uuid.New()

	// 1. สร้าง Station
	stationData := map[string]interface{}{
		"processing_station_id":   stationID,
		"processing_station_name": req.StationName,
		"hub_id":                  req.HubID,
		"address_detail":          req.AddressDetail,
		"province_id":             req.ProvinceID,
		"district_id":             req.DistrictID,
		"subdistrict_id":          req.SubdistrictID,
		"created_at":              time.Now(),
		"updated_at":              time.Now(),
	}

	if err := tx.Table("processing.processing_station").Create(&stationData).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "สร้างสถานีล้มเหลว"})
		return
	}

	// 2. เชื่อมโยง Processor กับ Station (ตารางกลาง)
	mapping := map[string]interface{}{
		"processor_id":          userID,
		"processing_station_id": stationID,
	}
	tx.Table("processing.processor_processing_station").Create(&mapping)

	tx.Commit()
	c.JSON(http.StatusOK, gin.H{"message": "สร้างสถานีแปรรูปสำเร็จ", "station_id": stationID})
}

func (h *ProcessingHandler) GetMyProcessingStation(c *gin.Context) {
	val, _ := c.Get("userID")
	userID := val.(uuid.UUID)

	// 1. ดึงข้อมูล Stations ที่ User คนนี้ดูแลอยู่
	var stations []models.ProcessingStation
	err := h.DB.Table("processing.processing_station").
		Select("processing.processing_station.*").
		Joins("JOIN processing.processor_processing_station pps ON pps.processing_station_id = processing.processing_station.processing_station_id").
		Where("pps.processor_id = ?", userID).
		Preload("Batches").
		Find(&stations).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ดึงข้อมูลสถานีล้มเหลว: " + err.Error()})
		return
	}

	// 2. ถ้าไม่มี Station เลย ให้ส่ง Array ว่างกลับไป
	if len(stations) == 0 {
		c.JSON(http.StatusOK, []interface{}{})
		return
	}

	c.JSON(http.StatusOK, stations)
}
func (h *ProcessingHandler) GetMyBatches(c *gin.Context) {
	val, _ := c.Get("userID")
	userID := val.(uuid.UUID)

	var batches []map[string]interface{}
	err := h.DB.Table("processing.batch").
		Joins("JOIN processing.processing_station ON processing.processing_station.processing_station_id = processing.batch.processing_station_id").
		Joins("JOIN processing.processor_processing_station ON processing.processor_processing_station.processing_station_id = processing.processing_station.processing_station_id").
		Where("processing.processor_processing_station.processor_id = ?", userID).
		Find(&batches).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, batches)
}
