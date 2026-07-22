package models
import (
	"time"
	"github.com/google/uuid"
)

type Farm struct {
    FarmID         uuid.UUID    `gorm:"primaryKey;column:farm_id" json:"farm_id"`
    FarmName       string    `json:"farm_name"`
    HubID          string    `json:"hub_id"`
    FoundDate      time.Time `json:"found_date"`
    TotalArea      float64   `json:"total_area"`
    AddressDetail  string    `json:"address_detail"`
    SubdistrictID  string    `json:"subdistrict_id"`
    DistrictID     string    `json:"district_id"`
    ProvinceID     string    `json:"province_id"`
    ZipCode        string    `json:"zip_code"`
    ContactName    string    `json:"contact_name"`
    PhoneNumber    string    `json:"phone_number"`
    Line           *string   `json:"line"`
    Facebook       *string   `json:"facebook"`
    CreatedAt      time.Time `json:"created_at"`
    UpdatedAt      time.Time `json:"updated_at"`
    // ความสัมพันธ์ One-to-Many กับ Plot
    Plots  []Plot `gorm:"foreignKey:FarmID;references:FarmID" json:"plots"`
}

func (Farm) TableName() string {
    return "agriculture.farm"
}