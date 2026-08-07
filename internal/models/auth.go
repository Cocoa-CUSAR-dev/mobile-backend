package models

import (
	"time"
	"github.com/google/uuid"
)

type UserAccount struct {
	UserID                  uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Username                string    `gorm:"not null"`
	PasswordHash            string    `gorm:"not null"`
	IsPasswordReset         bool      `gorm:"default:false"`
	IsRequiresPasswordReset bool      `gorm:"default:false"`
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

func (UserAccount) TableName() string {
	return "auth.user_account"
}

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type RegisterRequest struct {
	Username string  `json:"username" binding:"required"` // เบอร์โทร
	Password string  `json:"password" binding:"required,min=6"`
	Email    *string `json:"email"` // ไม่บังคับ (Optional)
}

type LineLinkRequest struct {
	UserID   string `json:"user_id" binding:"required"`
	LineUserID string `json:"line_user_id" binding:"required"`
	DisplayName string `json:"display_name" binding:"required"`
}	

	