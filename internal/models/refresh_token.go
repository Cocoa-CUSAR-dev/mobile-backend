package models

import (
	"time"

	"github.com/google/uuid"
)

// RefreshToken backs GO-3's refresh flow. Only a hash of the raw token is
// stored (see the V19 migration) -- the raw value never touches the DB.
type RefreshToken struct {
	RefreshTokenID uuid.UUID  `gorm:"type:uuid;primaryKey;column:refresh_token_id;default:gen_random_uuid()" json:"refresh_token_id"`
	UserID         uuid.UUID  `gorm:"type:uuid;column:user_id;not null" json:"user_id"`
	TokenHash      string     `gorm:"column:token_hash;not null" json:"-"`
	ExpiresAt      time.Time  `gorm:"column:expires_at;not null" json:"expires_at"`
	UsedAt         *time.Time `gorm:"column:used_at" json:"used_at"`
	RevokedAt      *time.Time `gorm:"column:revoked_at" json:"revoked_at"`
	CreatedAt      time.Time  `gorm:"column:created_at;default:now();not null" json:"created_at"`
}

func (RefreshToken) TableName() string {
	return "auth.refresh_token"
}
