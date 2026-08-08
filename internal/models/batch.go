package models

import (
	"time"

	"github.com/google/uuid"
)

type Batch struct {
	BatchID             uuid.UUID `gorm:"type:uuid;primaryKey;column:batch_id;default:gen_random_uuid()" json:"batch_id"`
	ProcessingStationID uuid.UUID `gorm:"type:uuid;column:processing_station_id;not null" json:"processing_station_id"`
	Origin              *string   `gorm:"column:origin" json:"origin"`
	Notes               *string   `gorm:"column:notes;default:''" json:"notes"`
	QuantityKg          *float64  `gorm:"column:quantity_kg" json:"quantity_kg"`
	CreatedAt           time.Time `gorm:"column:created_at;default:now();not null" json:"created_at"`
	UpdatedAt           time.Time `gorm:"column:updated_at;default:now();not null" json:"updated_at"`
}

func (Batch) TableName() string {
	return "processing.batch"
}
