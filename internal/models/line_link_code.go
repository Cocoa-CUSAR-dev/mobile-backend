package models

import (
	"time"

	"github.com/google/uuid"
)

type LineLinkCode struct {
	LinkCodeID uuid.UUID  `gorm:"type:uuid;primaryKey;column:link_code_id;default:gen_random_uuid()" json:"link_code_id"`
	UserID     uuid.UUID  `gorm:"type:uuid;column:user_id;not null" json:"user_id"`
	Code       string     `gorm:"column:code;not null" json:"code"`
	ExpiresAt  time.Time  `gorm:"column:expires_at;not null" json:"expires_at"`
	UsedAt     *time.Time `gorm:"column:used_at" json:"used_at"`
}

func (LineLinkCode) TableName() string {
	return "auth.line_link_code"
}
