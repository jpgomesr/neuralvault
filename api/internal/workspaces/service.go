// Package workspaces implements workspace creation and listing, plus the
// membership check that enforces tenant isolation on workspace-scoped routes.
package workspaces

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/jpgomesr/neuralvault/api/internal/model"
	"github.com/jpgomesr/neuralvault/api/internal/storage"
)

// Service manages workspaces and answers membership questions.
type Service interface {
	// Create creates a workspace and makes userID its owner, atomically.
	Create(ctx context.Context, userID uuid.UUID, name string) (*model.Workspace, error)
	// List returns the workspaces userID belongs to, newest first.
	List(ctx context.Context, userID uuid.UUID) ([]model.Workspace, error)
	// IsMember reports whether userID belongs to workspaceID.
	IsMember(ctx context.Context, userID, workspaceID uuid.UUID) (bool, error)
}

// WorkspaceService is the concrete Service backed by Postgres.
type WorkspaceService struct {
	pool storage.Pool
}

// NewWorkspaceService constructs a WorkspaceService.
func NewWorkspaceService(pool storage.Pool) *WorkspaceService {
	return &WorkspaceService{pool: pool}
}

// Create inserts the workspace row and its owner user_workspace row in one
// transaction, so a workspace never exists without an owner.
func (s *WorkspaceService) Create(ctx context.Context, userID uuid.UUID, name string) (*model.Workspace, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	now := time.Now()
	ws := model.Workspace{ID: uuid.New(), Name: name, CreatedAt: now, UpdatedAt: now}

	if _, err := tx.Exec(ctx, `
		INSERT INTO workspace (id, name, created_at, updated_at)
		VALUES ($1, $2, $3, $4)`,
		ws.ID, ws.Name, ws.CreatedAt, ws.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("insert workspace: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_workspace (id, user_id, workspace_id, role, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		uuid.New(), userID, ws.ID, model.RoleOwner, now, now,
	); err != nil {
		return nil, fmt.Errorf("insert membership: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return &ws, nil
}

// List returns the workspaces userID belongs to, joined through user_workspace.
func (s *WorkspaceService) List(ctx context.Context, userID uuid.UUID) ([]model.Workspace, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT w.id, w.name, w.created_at, w.updated_at
		FROM workspace w
		JOIN user_workspace uw ON uw.workspace_id = w.id
		WHERE uw.user_id = $1
		ORDER BY w.created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying workspaces: %w", err)
	}
	defer rows.Close()

	var out []model.Workspace
	for rows.Next() {
		var w model.Workspace
		if err := rows.Scan(&w.ID, &w.Name, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning workspace row: %w", err)
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating workspace rows: %w", err)
	}
	return out, nil
}

// IsMember reports whether a user_workspace row exists for (userID, workspaceID).
func (s *WorkspaceService) IsMember(ctx context.Context, userID, workspaceID uuid.UUID) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM user_workspace WHERE user_id = $1 AND workspace_id = $2
		)`,
		userID, workspaceID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("checking membership: %w", err)
	}
	return exists, nil
}
