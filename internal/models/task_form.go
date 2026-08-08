package models

import (
	"time"

	"github.com/google/uuid"
)

type TaskForm struct {
	FormID           uuid.UUID `gorm:"type:uuid;primaryKey;column:form_id;default:gen_random_uuid()" json:"form_id"`
	TaskID           uuid.UUID `gorm:"type:uuid;column:task_id;not null" json:"task_id"`
	Title            *string   `gorm:"column:title" json:"title"`
	Description      *string   `gorm:"column:description" json:"description"`
	Content          *string   `gorm:"column:content;default:''" json:"content"`
	Handler          *string   `gorm:"column:handler" json:"handler"`
	IsMultipleSubmit *bool     `gorm:"column:is_multiple_submit;default:false" json:"is_multiple_submit"`
	Version          int       `gorm:"column:version;not null;default:1" json:"version"`
	IsActive         bool      `gorm:"column:is_active;not null;default:true" json:"is_active"`
	CreatedAt        time.Time `gorm:"column:created_at;default:now();not null" json:"created_at"`
}

func (TaskForm) TableName() string {
	return "form.task_form"
}
