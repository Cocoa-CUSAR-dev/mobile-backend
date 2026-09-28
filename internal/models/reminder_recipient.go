package models

import (
	"time"

	"github.com/google/uuid"
)

// ReminderRecipient is one targeting rule of a notify.reminder_schedule: every
// holder of a role (RecipientType "ROLE", RoleID set) or exactly one person
// ("USER", UserID set). A schedule with no rows here reminds everyone who
// still owes the task. Only the chatbot service writes these; this model
// exists so the Go GORM models stay in sync with the schema (V26), like
// ReminderSchedule and ReminderLog.
type ReminderRecipient struct {
	RecipientID   uuid.UUID  `gorm:"type:uuid;primaryKey;column:recipient_id;default:gen_random_uuid()" json:"recipient_id"`
	ScheduleID    uuid.UUID  `gorm:"type:uuid;column:schedule_id;not null" json:"schedule_id"`
	RecipientType string     `gorm:"column:recipient_type;not null" json:"recipient_type"`
	RoleID        *uuid.UUID `gorm:"type:uuid;column:role_id" json:"role_id,omitempty"`
	UserID        *uuid.UUID `gorm:"type:uuid;column:user_id" json:"user_id,omitempty"`
	CreatedAt     time.Time  `gorm:"column:created_at;not null;default:now()" json:"created_at"`
}

func (ReminderRecipient) TableName() string {
	return "notify.reminder_recipient"
}
