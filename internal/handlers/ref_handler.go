package handlers

import (
	"go-server-mobile/internal/models"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type RefHandler struct {
	DB *gorm.DB
}

// personalConstantKeys are the "key"s under this same route that resolve
// against the caller's own userID (my farms, my hubs, my batches, ...) --
// unlike the reference-data keys (province, breed, ...), these need a real
// logged-in user, so they're rejected explicitly when OptionalJwtAuthMiddleware
// didn't find a valid session (see main.go for why this route isn't behind
// the blanket JwtAuthMiddleware).
var personalConstantKeys = map[string]bool{
	"farm":                true,
	"plot":                true,
	"hub":                 true,
	"processing_station":  true,
	"batch":               true,
	"harvest":             true,
}

func (h *RefHandler) GetConstants(c *gin.Context) {
	key := c.Param("key")

	// ดึง userID จาก Context (กรณีต้องการข้อมูลที่เป็นของตนเอง)
	val, authenticated := c.Get("userID")
	userID, _ := val.(uuid.UUID)

	if personalConstantKeys[key] && !authenticated {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "เซสชันหมดอายุ กรุณาเข้าสู่ระบบใหม่"})
		return
	}

	var data interface{}
	var err error

	switch key {
	case "air_exposure_type":
		var res []models.AirExposureTypeRes
		err = h.DB.Table("ref.air_exposure_type_constant").Order("1 ASC").Find(&res).Error
		data = res
	case "breed":
		var res []models.BreedRes
		err = h.DB.Table("ref.breed_constant").Order("1 ASC").Find(&res).Error
		data = res
	case "chem_bio":
		var res []models.ChemBioRes
		err = h.DB.Table("ref.chem_bio_constant").Order("1 ASC").Find(&res).Error
		data = res
	case "drying_facility":
		var res []models.DryingFacilityRes
		err = h.DB.Table("ref.drying_facility_constant").Order("1 ASC").Find(&res).Error
		data = res
	case "farm_activity_type":
		var res []models.FarmActivityTypeRes
		err = h.DB.Table("ref.farm_activity_type_constant").Order("1 ASC").Find(&res).Error
		data = res
	case "fertilizer_stage":
		var res []models.FertilizerApplicationStageRes
		err = h.DB.Table("ref.fertilizer_application_stage_constant").Order("1 ASC").Find(&res).Error
		data = res
	case "fertilizer":
		var res []models.FertilizerRes
		err = h.DB.Table("ref.fertilizer_constant").Order("1 ASC").Find(&res).Error
		data = res
	case "grade":
		var res []models.GradeRes
		err = h.DB.Table("ref.grade_constant").Order("1 ASC").Find(&res).Error
		data = res
	case "hole_filler":
		var res []models.HoleFillerMaterialRes
		err = h.DB.Table("ref.hole_filler_material_constant").Order("1 ASC").Find(&res).Error
		data = res
	case "land_type":
		var res []models.LandTypeRes
		err = h.DB.Table("ref.land_type_constant").Order("1 ASC").Find(&res).Error
		data = res
	case "location_type":
		var res []models.LocationTypeRes
		err = h.DB.Table("ref.location_type_constant").Order("1 ASC").Find(&res).Error
		data = res
	case "pest_disease":
		var res []models.PestDiseaseRes
		err = h.DB.Table("ref.pest_disease_constant").Order("1 ASC").Find(&res).Error
		data = res
	case "processing_activity_type":
		var res []models.ProcessingActivityTypeRes
		err = h.DB.Table("ref.processing_activity_type_constant").Order("1 ASC").Find(&res).Error
		data = res
	case "processing_defect":
		var res []models.ProcessingDefectRes
		err = h.DB.Table("ref.processing_defect_constant").Order("1 ASC").Find(&res).Error
		data = res
	case "soil_type":
		var res []models.SoilTypeRes
		err = h.DB.Table("ref.soil_type_constant").Order("1 ASC").Find(&res).Error
		data = res
	case "tank_material":
		var res []models.TankMaterialRes
		err = h.DB.Table("ref.tank_material_constant").Order("1 ASC").Find(&res).Error
		data = res
	case "water_source":
		var res []models.WaterSourceRes
		err = h.DB.Table("ref.water_source_constant").Order("1 ASC").Find(&res).Error
		data = res
	case "watering_system":
		var res []models.WateringSystemRes
		err = h.DB.Table("ref.watering_system_constant").Order("1 ASC").Find(&res).Error
		data = res
	case "weather_condition":
		var res []models.WeatherConditionRes
		err = h.DB.Table("ref.weather_condition_constant").Order("1 ASC").Find(&res).Error
		data = res
	case "province":
		var res []models.ProvinceRes
		err = h.DB.Table("ref.province_constant").Order("1 ASC").Find(&res).Error
		data = res
	case "district":
		var res []models.DistrictRes
		pID := c.Query("province_id")
		q := h.DB.Table("ref.district_constant")
		if pID != "" {
			q = q.Where("province_id = ?", pID)
		}
		err = q.Order("1 ASC").Find(&res).Error
		data = res
	case "subdistrict":
		var res []models.SubdistrictRes
		dID := c.Query("district_id")
		q := h.DB.Table("ref.subdistrict_constant")
		if dID != "" {
			q = q.Where("district_id = ?", dID)
		}
		err = q.Order("1 ASC").Find(&res).Error
		data = res
	case "farm":
		var res []models.FarmDropdownRes
		err = h.DB.Table("agriculture.farm").
			Select("farm_id, farm_name").
			Joins("JOIN agriculture.farmer_farm ff ON ff.farm_id = agriculture.farm.farm_id").
			Where("ff.farmer_id = ?", userID).
			Find(&res).Error
		data = res

	case "plot":
		var res []models.PlotDropdownRes
		fID := c.Query("farm_id")
		q := h.DB.Table("agriculture.plot").
			Select("plot_id, plot_name").
			Joins("JOIN agriculture.farm f ON f.farm_id = agriculture.plot.farm_id").
			Joins("JOIN agriculture.farmer_farm ff ON ff.farm_id = f.farm_id").
			Where("ff.farmer_id = ?", userID)

		if fID != "" {
			q = q.Where("agriculture.plot.farm_id = ?", fID)
		}
		err = q.Find(&res).Error
		data = res

	case "hub":
		var res []models.HubDropdownRes
		err = h.DB.Table("processing.hub").
			Select("hub_id, hub_name").
			Joins("JOIN processing.hub_collector hc ON hc.hub_id = processing.hub.hub_id").
			Where("hc.user_id = ?", userID).
			Find(&res).Error
		data = res

	case "processing_station":
		var res []struct {
			ProcessingStationID   string `json:"processing_station_id"`
			ProcessingStationName string `json:"processing_station_name"`
		}
		err = h.DB.Table("processing.processing_station").
			Select("processing_station_id, processing_station_name").
			Joins("JOIN processing.processor_processing_station pps ON pps.processing_station_id = processing.processing_station.processing_station_id").
			Where("pps.processor_id = ?", userID).
			Find(&res).Error
		data = res

	case "batch":
		var res []struct {
			BatchID string `json:"batch_id"`
			Name    string `json:"name"`
		}
		sID := c.Query("station_id")
		sql := `
            SELECT 
                batch_id, 
                origin || ' (' || TO_CHAR(created_at + interval '543 years', 'DD Mon ') || 
                (EXTRACT(YEAR FROM created_at) + 543) || ')' as name
            FROM processing.batch b
            JOIN processing.processing_station ps ON ps.processing_station_id = b.processing_station_id
            JOIN processing.processor_processing_station pps ON pps.processing_station_id = ps.processing_station_id
            WHERE pps.processor_id = ?`

		if sID != "" {
			err = h.DB.Raw(sql+" AND b.processing_station_id = ?", userID, sID).Scan(&res).Error
		} else {
			err = h.DB.Raw(sql, userID).Scan(&res).Error
		}
		data = res

	case "harvest":
		var res []struct {
			HarvestID string `json:"harvest_id"`
			Name      string `json:"name"`
		}
		hID := c.Query("hub_id")
		sql := `
            SELECT 
                h.harvest_id, 
                f.farm_name || ' (' || TO_CHAR(h.harvest_date + interval '543 years', 'DD Mon ') || 
                (EXTRACT(YEAR FROM h.harvest_date) + 543) || ')' as name
            FROM collection.harvest h
            JOIN agriculture.farm f ON f.farm_id = h.farm_id
            JOIN processing.hub_collector hc ON hc.hub_id = h.hub_id
            WHERE hc.user_id = ?`

		if hID != "" {
			err = h.DB.Raw(sql+" AND h.hub_id = ?", userID, hID).Scan(&res).Error
		} else {
			err = h.DB.Raw(sql, userID).Scan(&res).Error
		}
		data = res

	case "location":
		// ตัวนี้ใช้ LocationFullRes ที่ทำไว้ก่อนหน้าได้เลย เพราะชื่อฟิลด์ชัดเจนอยู่แล้ว
		var res []models.LocationFullRes
		zip := c.Query("zip_code")
		sdID := c.Query("subdistrict_id")

		if zip == "" && sdID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณาระบุ zip_code หรือ subdistrict_id"})
			return
		}

		q := h.DB.Table("ref.subdistrict_constant sd").
			Select(`
                sd.subdistrict_id, 
                sd.subdistrict_name_th as subdistrict_name,
                dt.district_name_th as district_name,
                pv.province_name_th as province_name,
                sd.zip_code,
                TRIM(
                    COALESCE('ต.' || sd.subdistrict_name_th, '') || 
                    COALESCE(' อ.' || dt.district_name_th, '') || 
                    COALESCE(' จ.' || pv.province_name_th, '') || 
                    COALESCE(' ' || sd.zip_code, '')
                ) as full_name
            `).
			Joins("LEFT JOIN ref.district_constant dt ON dt.district_id = sd.district_id").
			Joins("LEFT JOIN ref.province_constant pv ON pv.province_id = dt.province_id")

		if sdID != "" {
			q = q.Where("sd.subdistrict_id = ?", sdID)
		}
		if zip != "" {
			q = q.Where("sd.zip_code = ?", zip)
		}

		err = q.Order("sd.subdistrict_name_th ASC").Scan(&res).Error
		data = res
	default:
		c.JSON(http.StatusNotFound, gin.H{"error": "Constant key not found"})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if data == nil {
		c.JSON(http.StatusOK, []interface{}{})
		return
	}

	c.JSON(http.StatusOK, data)
}
