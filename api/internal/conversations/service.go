// Package conversations implements persisted chat conversations and their
// messages, scoped to a workspace.
package conversations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jpgomesr/NeuralVault/internal/model"
	"github.com/jpgomesr/NeuralVault/internal/storage"
)

// ErrNotFound is returned by GetByID when no conversation exists with the
// given id.
var ErrNotFound = errors.New("conversation not found")

// titleMaxRunes bounds the derived title stored on a conversation.
const titleMaxRunes = 60

// Service manages conversations and their messages.
type Service interface {
	// Create starts a new, untitled conversation in workspaceID.
	Create(ctx context.Context, workspaceID uuid.UUID) (*model.Conversation, error)
	// List returns workspaceID's conversations, most recently active first.
	List(ctx context.Context, workspaceID uuid.UUID) ([]model.Conversation, error)
	// GetByID returns a conversation by id, or ErrNotFound if none exists.
	GetByID(ctx context.Context, id uuid.UUID) (*model.Conversation, error)
	// ListMessages returns conversationID's messages, oldest first.
	ListMessages(ctx context.Context, conversationID uuid.UUID) ([]model.Message, error)
	// AppendMessage inserts a message into conversationID, bumps its
	// updated_at, and derives its title from the first message if unset.
	AppendMessage(ctx context.Context, conversationID uuid.UUID, role model.MessageRole, content string, sources json.RawMessage) (*model.Message, error)
}

// ConversationService is the concrete Service backed by Postgres.
type ConversationService struct {
	pool storage.Pool
}

// NewConversationService constructs a ConversationService.
func NewConversationService(pool storage.Pool) *ConversationService {
	return &ConversationService{pool: pool}
}

// Create inserts an untitled conversation row.
func (s *ConversationService) Create(ctx context.Context, workspaceID uuid.UUID) (*model.Conversation, error) {
	now := time.Now()
	conv := model.Conversation{ID: uuid.New(), WorkspaceID: workspaceID, Title: "", CreatedAt: now, UpdatedAt: now}

	if _, err := s.pool.Exec(ctx, `
		INSERT INTO conversations (id, workspace_id, title, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)`,
		conv.ID, conv.WorkspaceID, conv.Title, conv.CreatedAt, conv.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("insert conversation: %w", err)
	}
	return &conv, nil
}

// List returns workspaceID's conversations ordered by most recent activity.
func (s *ConversationService) List(ctx context.Context, workspaceID uuid.UUID) ([]model.Conversation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, workspace_id, title, created_at, updated_at
		FROM conversations
		WHERE workspace_id = $1
		ORDER BY updated_at DESC`,
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying conversations: %w", err)
	}
	defer rows.Close()

	var out []model.Conversation
	for rows.Next() {
		var c model.Conversation
		if err := rows.Scan(&c.ID, &c.WorkspaceID, &c.Title, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning conversation row: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating conversation rows: %w", err)
	}
	return out, nil
}

// GetByID returns the conversation with id, or ErrNotFound if none exists.
func (s *ConversationService) GetByID(ctx context.Context, id uuid.UUID) (*model.Conversation, error) {
	var c model.Conversation
	err := s.pool.QueryRow(ctx, `
		SELECT id, workspace_id, title, created_at, updated_at
		FROM conversations WHERE id = $1`, id,
	).Scan(&c.ID, &c.WorkspaceID, &c.Title, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get conversation by id: %w", err)
	}
	return &c, nil
}

// ListMessages returns conversationID's messages ordered oldest first.
func (s *ConversationService) ListMessages(ctx context.Context, conversationID uuid.UUID) ([]model.Message, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, conversation_id, role, content, sources, created_at
		FROM messages
		WHERE conversation_id = $1
		ORDER BY created_at ASC`,
		conversationID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying messages: %w", err)
	}
	defer rows.Close()

	var out []model.Message
	for rows.Next() {
		var m model.Message
		var sourcesBytes []byte
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &sourcesBytes, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning message row: %w", err)
		}
		if sourcesBytes != nil {
			m.Sources = json.RawMessage(sourcesBytes)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating message rows: %w", err)
	}
	return out, nil
}

// AppendMessage inserts a message into conversationID and, in the same
// transaction, bumps the conversation's updated_at and — if this is the
// first message the conversation has ever received — derives its title from
// content. The title is derived unconditionally on every call; the CASE in
// the UPDATE only ever applies it once, since the first message appended to
// a conversation is always the user's question.
func (s *ConversationService) AppendMessage(ctx context.Context, conversationID uuid.UUID, role model.MessageRole, content string, sources json.RawMessage) (*model.Message, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	msg := model.Message{
		ID:             uuid.New(),
		ConversationID: conversationID,
		Role:           role,
		Content:        content,
		Sources:        sources,
		CreatedAt:      time.Now(),
	}

	var sourcesBytes []byte
	if sources != nil {
		sourcesBytes = []byte(sources)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO messages (id, conversation_id, role, content, sources, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		msg.ID, msg.ConversationID, msg.Role, msg.Content, sourcesBytes, msg.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("insert message: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE conversations
		SET updated_at = now(),
		    title = CASE WHEN title = '' THEN $2 ELSE title END
		WHERE id = $1`,
		conversationID, deriveTitle(content),
	); err != nil {
		return nil, fmt.Errorf("update conversation: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return &msg, nil
}

// deriveTitle collapses whitespace in content and truncates it to
// titleMaxRunes, so a conversation's title stays a single readable line.
func deriveTitle(content string) string {
	collapsed := strings.Join(strings.Fields(content), " ")
	runes := []rune(collapsed)
	if len(runes) <= titleMaxRunes {
		return collapsed
	}
	return string(runes[:titleMaxRunes]) + "…"
}
