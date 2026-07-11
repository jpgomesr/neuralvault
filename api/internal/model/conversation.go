package model

import (
	"time"

	"github.com/google/uuid"
)

type Conversation struct {
	ID          uuid.UUID `db:"id"`
	WorkspaceID uuid.UUID `db:"workspace_id"`
	Title       string    `db:"title"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}
