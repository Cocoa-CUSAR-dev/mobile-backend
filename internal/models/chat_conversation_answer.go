package models

import (
	"github.com/google/uuid"
)

type ChatConversationAnswer struct {
	ConversationAnswerID uuid.UUID `gorm:"type:uuid;primaryKey;column:conversation_answer_id;default:gen_random_uuid()" json:"conversation_answer_id"`
	ConversationID uuid.UUID `gorm:"type:uuid;column:conversation_id;not null" json:"conversation_id"`
	QuestionID uuid.UUID `gorm:"type:uuid;column:question_id;not null" json:"question_id"`
	Answer []byte `gorm:"type:jsonb;column:answer" json:"answer"`
	Source string `gorm:"column:source;not null" json:"source"`
}

func (ChatConversationAnswer) TableName() string {
	return "chat.conversation_answer"
}
