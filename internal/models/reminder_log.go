package models

import (
	"time"

	"github.com/google/uuid"
)

type ReminderLog struct {
	LogID   uuid.UUID `gorm:"type:uuid;primaryKey;column:log_id;default:gen_random_uuid()" json:"log_id"`
	UserID  uuid.UUID `gorm:"type:uuid;column:user_id;not null" json:"user_id"`
	TaskID  uuid.UUID `gorm:"type:uuid;column:task_id;not null" json:"task_id"`
	SentAt  time.Time `gorm:"column:sent_at;default:now();not null" json:"sent_at"`
	Channel string    `gorm:"column:channel;not null" json:"channel"`
	Status  string    `gorm:"column:status;not null" json:"status"`
}

func (ReminderLog) TableName() string {
	return "notify.reminder_log"
}
