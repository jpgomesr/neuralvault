package model

import (
	"time"

	"github.com/google/uuid"
)

type UserIdentity struct {
	ID         uuid.UUID `db:"id"`
	UserID     uuid.UUID `db:"user_id"`
	Provider   string    `db:"provider"`
	ExternalID string    `db:"external_id"`
	CreatedAt  time.Time `db:"created_at"`
}
