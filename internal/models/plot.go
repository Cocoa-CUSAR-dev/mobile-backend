package models

import (
	"time"

	"github.com/google/uuid"
)

type Plot struct {
	PlotID           uuid.UUID  `gorm:"type:uuid;primaryKey;column:plot_id;default:gen_random_uuid()" json:"plot_id"`
	FarmID           uuid.UUID  `gorm:"type:uuid;column:farm_id;not null" json:"farm_id"`
	PlotName         *string    `gorm:"column:plot_name" json:"plot_name"`
	TotalArea        *float64   `gorm:"column:total_area" json:"total_area"`
	LandOwnership    *string    `gorm:"column:land_ownership" json:"land_ownership"`
	CocoaPlantedArea *float64   `gorm:"column:cocoa_planted_area" json:"cocoa_planted_area"`
	HasChemicalUse   *bool      `gorm:"column:has_chemical_use;default:false" json:"has_chemical_use"`
	LandTypeID       *uuid.UUID `gorm:"type:uuid;column:land_type_id" json:"land_type_id"`
	SoilTypeID       *uuid.UUID `gorm:"type:uuid;column:soil_type_id" json:"soil_type_id"`
	WaterSourceID    *uuid.UUID `gorm:"type:uuid;column:water_source_id" json:"water_source_id"`
	WateringSystemID *uuid.UUID `gorm:"type:uuid;column:watering_system_id" json:"watering_system_id"`
	FoundDate        *time.Time `gorm:"column:found_date;default:CURRENT_DATE" json:"found_date"`
	CreatedAt        time.Time  `gorm:"column:created_at;default:now();not null" json:"created_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at;default:now();not null" json:"updated_at"`
}

func (Plot) TableName() string {
	return "agriculture.plot"
}
