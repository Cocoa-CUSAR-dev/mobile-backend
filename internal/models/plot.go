package models
import (
	"time"
	"github.com/google/uuid"
)

type Plot struct {
    PlotID uuid.UUID `gorm:"primaryKey;column:plot_id" json:"plot_id"`
    FarmID string `gorm:"column:farm_id" json:"farm_id"`
    PlotName         string    `json:"plot_name"`
    TotalArea        float64   `json:"total_area"`
    LandOwnership    string    `json:"land_ownership"`
    CocoaPlantedArea float64   `json:"cocoa_planted_area"`
    HasChemicalUse   bool      `json:"has_chemical_use"`
    LandTypeID       *string   `json:"land_type_id"`
    SoilTypeID       *string   `json:"soil_type_id"`
    WaterSourceID    *string   `json:"water_source_id"`
    WateringSystemID *string   `json:"watering_system_id"`
    FoundDate        time.Time `json:"found_date"`
    CreatedAt        time.Time `json:"created_at"`
    UpdatedAt        time.Time `json:"updated_at"`
}

func (Plot) TableName() string {
    return "agriculture.plot"
}