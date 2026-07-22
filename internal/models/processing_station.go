package models

import (
	"time"

	"github.com/google/uuid"
)

type ProcessingStation struct {
	ProcessingStationID   uuid.UUID `gorm:"primaryKey;column:processing_station_id" json:"processing_station_id"`
	ProcessingStationName *string   `json:"processing_station_name"`

	HubID uuid.UUID `json:"hub_id"`

	GeoID                 *uuid.UUID `json:"geo_id"`
	FoundDate             *time.Time `json:"found_date"`
	ProcessingStationArea *float64   `json:"processing_station_area"`
	AddressDetail         *string    `json:"address_detail"`

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

	// relations
	Batches []Batch `gorm:"foreignKey:ProcessingStationID;references:ProcessingStationID"`
}

func (ProcessingStation) TableName() string {
	return "processing.processing_station"
}
