package models

import (
	"time"

	"github.com/google/uuid"
)

type Response struct {
	ResponseID  uuid.UUID  `gorm:"type:uuid;primaryKey;column:response_id;default:gen_random_uuid()" json:"response_id"`
	TaskLogID   uuid.UUID  `gorm:"type:uuid;column:task_log_id;not null" json:"task_log_id"`
	UserID      uuid.UUID  `gorm:"type:uuid;column:user_id;not null" json:"user_id"`
	SubmittedAt *time.Time `gorm:"column:submitted_at;default:now()" json:"submitted_at"`
	Answer      []byte     `gorm:"type:jsonb;column:answer" json:"answer"`
	Status      *string    `gorm:"column:status" json:"status"`
	TaskFormID  *uuid.UUID `gorm:"type:uuid;column:task_form_id" json:"task_form_id"`
}

func (Response) TableName() string {
	return "form.response"
}
