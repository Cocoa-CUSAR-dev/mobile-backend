package models

import (
	"time"

	"github.com/google/uuid"
)

type Hub struct {
	HubID         uuid.UUID  `gorm:"type:uuid;primaryKey;column:hub_id;default:gen_random_uuid()" json:"hub_id"`
	HubName       *string    `gorm:"column:hub_name" json:"hub_name"`
	GeoID         *uuid.UUID `gorm:"type:uuid;column:geo_id" json:"geo_id"`
	FoundDate     *time.Time `gorm:"column:found_date" json:"found_date"`
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
	Harvests      []Harvest  `gorm:"foreignKey:HubID;references:HubID" json:"harvests,omitempty"`
}

func (Hub) TableName() string {
	return "processing.hub"
}
