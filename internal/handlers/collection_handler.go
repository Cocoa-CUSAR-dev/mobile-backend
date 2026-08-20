package handlers

import (
	"fmt"
	"go-server-mobile/internal/models"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CollectionHandler struct {
	DB *gorm.DB
}

// --- Request Structs ---

type RegisterHubCollectorRequest struct {
	FirstName    *string `json:"first_name"`
	LastName     *string `json:"last_name"`
	Nickname     *string `json:"nickname"`
	BirthDate    string  `json:"birthdate"`
	IdCardNumber *string `json:"id_card_number"`
	PhoneNumber  *string `json:"phone_number"`
	Line         *string `json:"line"`
	Facebook     *string `json:"facebook"`
}

type RegisterHubRequest struct {
	HubName       *string              `json:"hub_name"`
	FoundDate     string               `json:"found_date"`
	AddressDetail *string              `json:"address_detail"`
	ProvinceID    *string              `json:"province_id"`
	DistrictID    *string              `json:"district_id"`
	SubdistrictID *string              `json:"subdistrict_id"`
	ZipCode       *string              `json:"zip_code"`
	ContactName   *string              `json:"contact_name"`
	PhoneNumber   *string              `json:"phone_number"`
	Line          *string              `json:"line"`
	Facebook      *string              `json:"facebook"`
	GIS           []map[string]float64 `json:"gis"` // Slice เป็น optional โดยธรรมชาติอยู่แล้ว (เป็น nil ได้)
}

func (h *CollectionHandler) RegisterHubCollector(c *gin.Context) {
	val, _ := c.Get("userID")
	userID := val.(uuid.UUID)

	var req RegisterHubCollectorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": req})
		return
	}

	tx := h.DB.Begin()

	// 1. บันทึกข้อมูล Collector
	collectorData := map[string]interface{}{
		"user_id":        userID,
		"first_name":     req.FirstName,
		"last_name":      req.LastName,
		"nickname":       req.Nickname,
		"birthdate":      _parseDate(req.BirthDate),
		"id_card_number": req.IdCardNumber,
		"phone_number":   req.PhoneNumber,
		"line":           req.Line,
		"facebook":       req.Facebook,
		"created_at":     time.Now(),
		"updated_at":     time.Now(),
	}

	if err := tx.Table("processing.hub_collector").Create(&collectorData).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusConflict, gin.H{"error": "คุณได้ลงทะเบียนเป็นหน่วยรวบรวมไปแล้ว"})
		return
	}

	// 2. เพิ่ม Role 'hub_collector' (UUID: ...0004)
	roleID, _ := uuid.Parse("bbbbbbbb-2222-2222-0004-bbbbbbbbbbbb")
	tx.Exec("INSERT INTO auth.user_role (user_id, role_id) VALUES (?, ?) ON CONFLICT DO NOTHING", userID, roleID)

	tx.Commit()

	// Re-sign the session cookie so it carries the "hub_collector" role
	// that was just granted — see reissueTokenCookie's doc comment.
	_ = reissueTokenCookie(c, h.DB, userID)

	c.JSON(http.StatusOK, gin.H{"message": "ลงทะเบียน Collector สำเร็จ"})
}

func (h *CollectionHandler) RegisterHub(c *gin.Context) {
	val, _ := c.Get("userID")
	userID := val.(uuid.UUID)

	var req RegisterHubRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tx := h.DB.Begin()
	hubID := uuid.New()

	// 1. สร้าง Hub
	hubData := map[string]interface{}{
		"hub_id":         hubID,
		"hub_name":       req.HubName,
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

	if err := tx.Table("processing.hub").Create(&hubData).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "สร้าง Hub ล้มเหลว"})
		return
	}

	// 2. อัปเดตตาราง Collector ให้ผูกกับ Hub นี้ (เงื่อนไขเบื้องต้น: 1 คน 1 Hub)
	if err := tx.Table("processing.hub_collector").Where("user_id = ?", userID).Update("hub_id", hubID).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ไม่สามารถผูก Hub กับ Collector ได้"})
		return
	}

	// 3. บันทึก GIS ถ้ามี
	if len(req.GIS) > 0 {
		wkt := fmt.Sprintf("POINT(%f %f)", req.GIS[0]["lng"], req.GIS[0]["lat"])
		tx.Exec("INSERT INTO storage.geo (uploaded_by, geom, code, source_type, created_at) VALUES (?, ST_GeomFromText(?, 4326), ?, ?, ?)",
			userID, wkt, hubID.String(), "hub", time.Now())
	}

	tx.Commit()
	c.JSON(http.StatusOK, gin.H{"message": "ลงทะเบียน Hub สำเร็จ", "hub_id": hubID})
}

func (h *CollectionHandler) GetMyHub(c *gin.Context) {
	val, _ := c.Get("userID")
	userID := val.(uuid.UUID)

	// 1. ดึง HUB ทั้งหมดของ user
	var hubs []models.Hub
	err := h.DB.Table("processing.hub").
		Select("processing.hub.*").
		Joins("JOIN processing.hub_collector ON processing.hub_collector.hub_id = processing.hub.hub_id").
		Where("processing.hub_collector.user_id = ?", userID).
		Find(&hubs).Error

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบข้อมูล Hub"})
		return
	}

	if len(hubs) == 0 {
		c.JSON(http.StatusOK, []interface{}{})
		return
	}

	// 2. ดึง harvests ของทุก hub ทีเดียว (performance-friendly)
	var hubIDs []uuid.UUID
	for _, h := range hubs {
		hubIDs = append(hubIDs, h.HubID)
	}

	var harvests []models.Harvest
	err = h.DB.Table("collection.harvest").
		Select(`
            collection.harvest.*,
            gd.grade_code, gd.quantity_kg, gd.description as grade_description,
            gd.is_clean, gd.is_appropriate_size, gd.weight_gram_per_pod,
            gd.is_sprout, gd.is_dry, gd.is_shriveled, gd.cut_test_result,
            hc.collection_id, hc.batch_id, f.farm_name
        `).
		Joins("LEFT JOIN collection.harvest_grade_detail gd ON gd.harvest_id = collection.harvest.harvest_id").
		Joins("LEFT JOIN collection.harvest_collection hc ON hc.harvest_id = collection.harvest.harvest_id").
		Joins("LEFT JOIN agriculture.farm f ON f.farm_id = collection.harvest.farm_id").
		Where("collection.harvest.hub_id IN ?", hubIDs).
		Order("collection.harvest.harvest_date DESC").
		Find(&harvests).Error

	if err != nil {
		fmt.Println("Error fetching harvests:", err)
		harvests = []models.Harvest{}
	}

	// 3. map harvests → hub
	type HubWithHarvest struct {
		models.Hub
		Harvests []models.Harvest `json:"harvests"`
	}

	hubMap := map[uuid.UUID][]models.Harvest{}
	for _, hr := range harvests {
		hubMap[hr.HubID] = append(hubMap[hr.HubID], hr)
	}

	var result []HubWithHarvest
	for _, hb := range hubs {
		result = append(result, HubWithHarvest{
			Hub:      hb,
			Harvests: hubMap[hb.HubID],
		})
	}

	c.JSON(http.StatusOK, result)
}

func (h *CollectionHandler) GetMyHarvests(c *gin.Context) {
	val, _ := c.Get("userID")
	userID := val.(uuid.UUID)

	var results []models.Harvest

	// ใช้การ Join เพื่อดึงข้อมูลทุกอย่างมาเป็น Row เดียวต่อ 1 Harvest
	err := h.DB.Table("collection.harvest").
		Select(`
			collection.harvest.*, 
			gd.grade_code, gd.quantity_kg, gd.description as grade_description, 
			gd.is_clean, gd.is_appropriate_size, gd.weight_gram_per_pod, 
			gd.is_sprout, gd.is_dry, gd.is_shriveled, gd.cut_test_result,
			hc.collection_id, hc.batch_id
		`).
		// Join กับตาราง Collector เพื่อเช็คสิทธิ์ (ต้องเห็นเฉพาะ Hub ที่ตัวเองดูแล)
		Joins("JOIN processing.hub_collector hc_auth ON hc_auth.hub_id = collection.harvest.hub_id").
		// Left Join ข้อมูลเกรด (เผื่อกรณีบางอันยังไม่ได้เกรด ข้อมูลจะได้ไม่หาย)
		Joins("LEFT JOIN collection.harvest_grade_detail gd ON gd.harvest_id = collection.harvest.harvest_id").
		// Left Join ข้อมูลการจับคู่ Batch
		Joins("LEFT JOIN collection.harvest_collection hc ON hc.harvest_id = collection.harvest.harvest_id").
		Where("hc_auth.user_id = ?", userID).
		Order("collection.harvest.harvest_date DESC").
		Find(&results).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ดึงข้อมูลล้มเหลว: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, results)
}
