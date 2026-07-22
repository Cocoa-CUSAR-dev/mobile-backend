package models

// --- กลุ่มข้อมูลการเกษตรและสายพันธุ์ ---
type AirExposureTypeRes struct {
	AirExposureTypeID   string `json:"air_exposure_type_id"`
	AirExposureTypeName string `json:"air_exposure_type_name"`
}

type BreedRes struct {
	BreedID   string `json:"breed_id"`
	BreedName string `json:"breed_name"`
}

type ChemBioRes struct {
	ChemBioID   string `json:"chem_bio_id"`
	ChemBioName string `json:"chem_bio_name"`
	Description string `json:"description"`
}

type CocoaBeanGradeRes struct {
	CocoaBeanGradeID   string `json:"cocoa_bean_grade_id"`
	CocoaBeanGradeName string `json:"cocoa_bean_grade_name"`
}

type FarmActivityTypeRes struct {
	FarmActivityTypeID   string `json:"farm_activity_type_id"`
	FarmActivityTypeName string `json:"farm_activity_type_name"`
}

// --- กลุ่มปุ๋ยและสารอาหาร ---
type FertilizerApplicationStageRes struct {
	FertilizerApplicationStageID          string `json:"fertilizer_application_stage_id"`
	FertilizerApplicationStageNameTh      string `json:"fertilizer_application_stage_name_th"`
	FertilizerApplicationStageNameEn      string `json:"fertilizer_application_stage_name_en"`
	FertilizerApplicationStageDescription string `json:"fertilizer_application_stage_description"`
}

type FertilizerRes struct {
	FertilizerID   string `json:"fertilizer_id"`
	FertilizerName string `json:"fertilizer_name"`
	Description    string `json:"description"`
	IsOrganic      bool   `json:"is_organic"`
}

// --- กลุ่มมาตรฐานและเกณฑ์ ---
type GradeRes struct {
	GradeID   string `json:"grade_id"`
	GradeName string `json:"grade_name"`
	IsFail    bool   `json:"is_fail"`
}

type PestDiseaseRes struct {
	PestDiseaseID string `json:"pest_disease_id"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	Description   string `json:"description"`
}

// --- กลุ่มสถานที่และกายภาพ ---
type HoleFillerMaterialRes struct {
	HoleFillerMaterialID   string `json:"hole_filler_material_id"`
	HoleFillerMaterialName string `json:"hole_filler_material_name"`
}

type LandTypeRes struct {
	LandTypeID   string `json:"land_type_id"`
	LandTypeName string `json:"land_type_name"`
}

type LocationTypeRes struct {
	LocationTypeID   string `json:"location_type_id"`
	LocationTypeName string `json:"location_type_name"`
}

type SoilTypeRes struct {
	SoilTypeID   string `json:"soil_type_id"`
	SoilTypeName string `json:"soil_type_name"`
}

type WaterSourceRes struct {
	WaterSourceID   string `json:"water_source_id"`
	WaterSourceName string `json:"water_source_name"`
}

type WateringSystemRes struct {
	WateringSystemID   string `json:"watering_system_id"`
	WateringSystemName string `json:"watering_system_name"`
}

type WeatherConditionRes struct {
	WeatherConditionID   string `json:"weather_condition_id"`
	WeatherConditionName string `json:"weather_condition_name"`
}

// --- กลุ่มการแปรรูป ---
type DryingFacilityRes struct {
	DryingFacilityTypeID   string `json:"drying_facility_type_id"`
	DryingFacilityTypeName string `json:"drying_facility_type_name"`
}

type ProcessingActivityTypeRes struct {
	ProcessingActivityTypeID   string `json:"processing_activity_type_id"`
	ProcessingActivityTypeName string `json:"processing_activity_type_name"`
}

type ProcessingDefectRes struct {
	ProcessingDefectID   string `json:"processing_defect_id"`
	ProcessingDefectName string `json:"processing_defect_name"`
}

type TankMaterialRes struct {
	TankMaterialID   string `json:"tank_material_id"`
	TankMaterialName string `json:"tank_material_name"`
}

// --- กลุ่มที่อยู่ (Hierarchy) ---
type ProvinceRes struct {
	ProvinceID     string `json:"province_id"`
	ProvinceNameTh string `json:"province_name_th"`
	ProvinceNameEn string `json:"province_name_en"`
	Description    string `json:"description"`
}

type DistrictRes struct {
	DistrictID     string `json:"district_id"`
	DistrictNameTh string `json:"district_name_th"`
	DistrictNameEn string `json:"district_name_en"`
	ProvinceID     string `json:"province_id"`
	Description    string `json:"description"`
}

type SubdistrictRes struct {
	SubdistrictID     string `json:"subdistrict_id"`
	SubdistrictNameTh string `json:"subdistrict_name_th"`
	SubdistrictNameEn string `json:"subdistrict_name_en"`
	DistrictID        string `json:"district_id"`
	Description       string `json:"description"`
}

// แยกเป็นรายตัวตามที่คุณต้องการ ไม่ใช้ id/name กลาง
type FarmDropdownRes struct {
	FarmID   string `json:"farm_id"`
	FarmName string `json:"farm_name"`
}

type PlotDropdownRes struct {
	PlotID   string `json:"plot_id"`
	PlotName string `json:"plot_name"`
}

type HubDropdownRes struct {
	HubID   string `json:"hub_id"`
	HubName string `json:"hub_name"`
}

type StationDropdownRes struct {
	ProcessingStationID   string `json:"processing_station_id"`
	ProcessingStationName string `json:"processing_station_name"`
}

type BatchDropdownRes struct {
	BatchID string `json:"batch_id"`
	Name    string `json:"name"` // ริมรั้ว (30 เม.ย. 2569)
}

type HarvestDropdownRes struct {
	HarvestID string `json:"harvest_id"`
	Name      string `json:"name"` // ฟาร์มเฮาส์ (01 พ.ค. 2569)
}

type LocationFullRes struct {
	SubdistrictID   string `json:"subdistrict_id"`
	SubdistrictName string `json:"subdistrict_name"`
	DistrictName    string `json:"district_name"`
	ProvinceName    string `json:"province_name"`
	ZipCode         string `json:"zip_code"`
	FullName        string `json:"full_name"`
}
