package models

import (
	"time"

	"github.com/google/uuid"
)

type Farm struct {
	FarmID        uuid.UUID  `gorm:"type:uuid;primaryKey;column:farm_id;default:gen_random_uuid()" json:"farm_id"`
	FarmName      *string    `gorm:"column:farm_name" json:"farm_name"`
	HubID         *uuid.UUID `gorm:"type:uuid;column:hub_id" json:"hub_id"`
	GeoID         *uuid.UUID `gorm:"type:uuid;column:geo_id" json:"geo_id"`
	FoundDate     *time.Time `gorm:"column:found_date;default:CURRENT_DATE" json:"found_date"`
	TotalArea     *float64   `gorm:"column:total_area" json:"total_area"`
	AddressDetail *string    `gorm:"column:address_detail" json:"address_detail"`
	SubdistrictID uuid.UUID  `gorm:"type:uuid;column:subdistrict_id;not null" json:"subdistrict_id"`
	DistrictID    uuid.UUID  `gorm:"type:uuid;column:district_id;not null" json:"district_id"`
	ProvinceID    uuid.UUID  `gorm:"type:uuid;column:province_id;not null" json:"province_id"`
	ZipCode       *string    `gorm:"column:zip_code" json:"zip_code"`
	ContactName   *string    `gorm:"column:contact_name" json:"contact_name"`
	PhoneNumber   *string    `gorm:"column:phone_number" json:"phone_number"`
	Line          *string    `gorm:"column:line" json:"line"`
	Facebook      *string    `gorm:"column:facebook" json:"facebook"`
	CreatedAt     time.Time  `gorm:"column:created_at;default:now();not null" json:"created_at"`
	UpdatedAt     time.Time  `gorm:"column:updated_at;default:now();not null" json:"updated_at"`
	Plots         []Plot     `gorm:"foreignKey:FarmID;references:FarmID" json:"plots"`
}

func (Farm) TableName() string {
	return "agriculture.farm"
}
