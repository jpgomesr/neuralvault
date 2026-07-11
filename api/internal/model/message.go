package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type MessageRole string

const (
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
)

// Message is one turn in a Conversation. Sources carries the grounding chunks
// cited by an assistant message (nil for a user message).
type Message struct {
	ID             uuid.UUID       `db:"id"`
	ConversationID uuid.UUID       `db:"conversation_id"`
	Role           MessageRole     `db:"role"`
	Content        string          `db:"content"`
	Sources        json.RawMessage `db:"sources"`
	CreatedAt      time.Time       `db:"created_at"`
}
