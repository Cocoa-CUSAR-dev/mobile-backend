package models

import (
	"github.com/google/uuid"
)

type ReminderSchedule struct {
	ScheduleID uuid.UUID `gorm:"type:uuid;primaryKey;column:schedule_id;default:gen_random_uuid()" json:"schedule_id"`
	TaskID     uuid.UUID `gorm:"type:uuid;column:task_id;not null" json:"task_id"`
	Cadence    string    `gorm:"column:cadence;not null" json:"cadence"`
	TimeOfDay  string    `gorm:"type:time;column:time_of_day;not null" json:"time_of_day"`
	IsActive   bool      `gorm:"column:is_active;not null;default:true" json:"is_active"`
	CreatedBy  uuid.UUID `gorm:"type:uuid;column:created_by;not null" json:"created_by"`
}

func (ReminderSchedule) TableName() string {
	return "notify.reminder_schedule"
}
