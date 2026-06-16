package model

import (
	"time"

	"github.com/google/uuid"
)

type WorkspaceRole string

const (
	RoleOwner  WorkspaceRole = "owner"
	RoleAdmin  WorkspaceRole = "admin"
	RoleMember WorkspaceRole = "member"
)

type UserWorkspace struct {
	ID          uuid.UUID     `db:"id"`
	UserID      uuid.UUID     `db:"user_id"`
	WorkspaceID uuid.UUID     `db:"workspace_id"`
	Role        WorkspaceRole `db:"role"`
	CreatedAt   time.Time     `db:"created_at"`
	UpdatedAt   time.Time     `db:"updated_at"`
}
