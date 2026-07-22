package models

import (
	"time"

	"github.com/google/uuid"
)

type Batch struct {
	BatchID uuid.UUID `gorm:"primaryKey;column:batch_id" json:"batch_id"`

	ProcessingStationID uuid.UUID `json:"processing_station_id"`

	Origin     *string  `json:"origin"`
	Notes      *string  `json:"notes"`
	QuantityKg *float64 `json:"quantity_kg"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Batch) TableName() string {
	return "processing.batch"
}
