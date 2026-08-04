package models

import (
	"time"

	"github.com/google/uuid"
)

type UserAccount struct {
	UserID                  uuid.UUID `gorm:"type:uuid;primaryKey;column:user_id;default:gen_random_uuid()" json:"user_id"`
	Username                string    `gorm:"column:username;not null;unique" json:"username"`
	PasswordHash            *string   `gorm:"column:password_hash" json:"-"`
	IsPasswordReset         *bool     `gorm:"column:is_password_reset;default:false" json:"is_password_reset"`
	IsRequiresPasswordReset *bool     `gorm:"column:is_requires_password_reset;default:false" json:"is_requires_password_reset"`
	CreatedAt               time.Time `gorm:"column:created_at;default:now();not null" json:"created_at"`
	UpdatedAt               time.Time `gorm:"column:updated_at;default:now();not null" json:"updated_at"`
}

func (UserAccount) TableName() string {
	return "auth.user_account"
}

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type RegisterRequest struct {
	Username string  `json:"username" binding:"required"`
	Password string  `json:"password" binding:"required,min=6"`
	Email    *string `json:"email"`
}
