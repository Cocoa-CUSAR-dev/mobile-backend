package models

import (
	"github.com/google/uuid"
)

type ChatConversation struct {
	ConversationID    uuid.UUID  `gorm:"type:uuid;primaryKey;column:conversation_id;default:gen_random_uuid()" json:"conversation_id"`
	UserID            uuid.UUID  `gorm:"type:uuid;column:user_id;not null" json:"user_id"`
	TaskID            uuid.UUID  `gorm:"type:uuid;column:task_id;not null" json:"task_id"`
	TaskFormID        uuid.UUID  `gorm:"type:uuid;column:task_form_id;not null" json:"task_form_id"`
	Status            string     `gorm:"column:status;not null;default:active" json:"status"`
	CurrentQuestionID *uuid.UUID `gorm:"type:uuid;column:current_question_id" json:"current_question_id"`
}

func (ChatConversation) TableName() string {
	return "chat.conversation"
}
