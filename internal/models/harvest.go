package models

import (
	"time"

	"github.com/google/uuid"
)

type Harvest struct {
	HarvestID uuid.UUID `gorm:"primaryKey;column:harvest_id" json:"harvest_id"`

	HubID uuid.UUID `json:"hub_id"`

	HarvestDate *time.Time `json:"harvest_date"`
	QuantityKg  *float64   `json:"quantity_kg"`
	Notes       *string    `json:"notes"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Harvest) TableName() string {
	return "processing.harvest"
}
