package models

import (
	"time"

	"github.com/google/uuid"
)

type LineIdentity struct {
	LineIdentityID uuid.UUID `gorm:"type:uuid;primaryKey;column:line_identity_id;default:gen_random_uuid()" json:"line_identity_id"`
	UserID         uuid.UUID `gorm:"type:uuid;column:user_id;not null;unique" json:"user_id"`
	LineUserID     string    `gorm:"column:line_user_id;not null;unique" json:"line_user_id"`
	DisplayName    *string   `gorm:"column:display_name" json:"display_name"`
	LinkedAt       time.Time `gorm:"column:linked_at;default:now();not null" json:"linked_at"`
}

func (LineIdentity) TableName() string {
	return "auth.line_identity"
}
