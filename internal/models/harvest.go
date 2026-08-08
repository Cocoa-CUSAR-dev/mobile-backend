package models

import (
	"time"

	"github.com/google/uuid"
)

type Harvest struct {
	HarvestID   uuid.UUID  `gorm:"type:uuid;primaryKey;column:harvest_id;default:gen_random_uuid()" json:"harvest_id"`
	HubID       uuid.UUID  `gorm:"type:uuid;column:hub_id;not null" json:"hub_id"`
	FarmID      uuid.UUID  `gorm:"type:uuid;column:farm_id;not null" json:"farm_id"`
	PlotID      *uuid.UUID `gorm:"type:uuid;column:plot_id" json:"plot_id"`
	TreeID      *uuid.UUID `gorm:"type:uuid;column:tree_id" json:"tree_id"`
	HarvestDate *time.Time `gorm:"column:harvest_date" json:"harvest_date"`
	CreatedAt   *time.Time `gorm:"column:created_at;default:now()" json:"created_at"`
	UpdatedAt   *time.Time `gorm:"column:updated_at;default:now()" json:"updated_at"`

	GradeCode         *string    `gorm:"->;column:grade_code" json:"grade_code,omitempty"`
	QuantityKg        *float64   `gorm:"->;column:quantity_kg" json:"quantity_kg,omitempty"`
	GradeDescription  *string    `gorm:"->;column:grade_description" json:"grade_description,omitempty"`
	IsClean           *bool      `gorm:"->;column:is_clean" json:"is_clean,omitempty"`
	IsAppropriateSize *bool      `gorm:"->;column:is_appropriate_size" json:"is_appropriate_size,omitempty"`
	WeightGramPerPod  *float64   `gorm:"->;column:weight_gram_per_pod" json:"weight_gram_per_pod,omitempty"`
	IsSprout          *bool      `gorm:"->;column:is_sprout" json:"is_sprout,omitempty"`
	IsDry             *bool      `gorm:"->;column:is_dry" json:"is_dry,omitempty"`
	IsShriveled       *bool      `gorm:"->;column:is_shriveled" json:"is_shriveled,omitempty"`
	CutTestResult     *string    `gorm:"->;column:cut_test_result" json:"cut_test_result,omitempty"`
	CollectionID      *uuid.UUID `gorm:"->;type:uuid;column:collection_id" json:"collection_id,omitempty"`
	BatchID           *uuid.UUID `gorm:"->;type:uuid;column:batch_id" json:"batch_id,omitempty"`
	FarmName          *string    `gorm:"->;column:farm_name" json:"farm_name,omitempty"`
}

func (Harvest) TableName() string {
	return "collection.harvest"
}
