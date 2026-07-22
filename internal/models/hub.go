package models

import (
	"time"

	"github.com/google/uuid"
)

type Hub struct {
	HubID         uuid.UUID  `gorm:"primaryKey;column:hub_id" json:"hub_id"`
	HubName       *string    `json:"hub_name"`
	GeoID         *uuid.UUID `json:"geo_id"`
	FoundDate     *time.Time `json:"found_date"`
	AddressDetail *string    `json:"address_detail"`

	SubdistrictID uuid.UUID `json:"subdistrict_id"`
	DistrictID    uuid.UUID `json:"district_id"`
	ProvinceID    uuid.UUID `json:"province_id"`

	ZipCode     *string `json:"zip_code"`
	ContactName *string `json:"contact_name"`
	PhoneNumber *string `json:"phone_number"`
	Line        *string `json:"line"`
	Facebook    *string `json:"facebook"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Harvests  []Harvest `gorm:"foreignKey:HubID;references:HubID"`
}

func (Hub) TableName() string {
	return "processing.hub"
}
