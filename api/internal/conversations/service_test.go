package conversations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/jpgomesr/neuralvault/api/internal/config"
	"github.com/jpgomesr/neuralvault/api/internal/model"
	pgstore "github.com/jpgomesr/neuralvault/api/internal/storage/postgres"
)

var sharedPool *pgxpool.Pool

func TestMain(m *testing.M) {
	os.Exit(runAllTests(m))
}

func runAllTests(m *testing.M) int {
	ctx := context.Background()

	pgCtr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:17",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_USER":     "neuralvault",
				"POSTGRES_PASSWORD": "neuralvault",
				"POSTGRES_DB":       "neuralvault",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		},
		Started: true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "start postgres: %v\n", err)
		return 1
	}
	defer func() { _ = pgCtr.Terminate(ctx) }()

	pgHost, err := pgCtr.Host(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "postgres host: %v\n", err)
		return 1
	}
	pgPort, err := pgCtr.MappedPort(ctx, "5432")
	if err != nil {
		fmt.Fprintf(os.Stderr, "postgres port: %v\n", err)
		return 1
	}

	sharedPool, err = pgstore.NewPool(ctx, config.Config{
		Postgres: config.Postgres{
			Host:     pgHost,
			Port:     int(pgPort.Num()),
			Username: "neuralvault",
			Password: "neuralvault",
			Name:     "neuralvault",
			SSLMode:  "disable",
			MaxConns: 10,
			MinConns: 1,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "create pool: %v\n", err)
		return 1
	}
	defer sharedPool.Close()

	sqlDB := stdlib.OpenDBFromPool(sharedPool)
	defer sqlDB.Close() //nolint:errcheck

	if err := goose.SetDialect("postgres"); err != nil {
		fmt.Fprintf(os.Stderr, "goose dialect: %v\n", err)
		return 1
	}
	wd, _ := os.Getwd()
	migrationsDir := filepath.Join(wd, "../storage/postgres/migrations")
	if err := goose.Up(sqlDB, migrationsDir); err != nil {
		fmt.Fprintf(os.Stderr, "goose up: %v\n", err)
		return 1
	}

	return m.Run()
}

// insertWorkspace inserts a workspace row and schedules its deletion on test
// cleanup. Deleting the workspace cascades to any conversations (and their
// messages) created against it, so tests don't need separate teardown.
func insertWorkspace(ctx context.Context, t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := sharedPool.Exec(ctx, "INSERT INTO workspace (id, name) VALUES ($1, $2)", id, "test"); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = sharedPool.Exec(context.Background(), "DELETE FROM workspace WHERE id = $1", id)
	})
	return id
}

func TestCreate(t *testing.T) {
	ctx := context.Background()
	svc := NewConversationService(sharedPool)
	wsID := insertWorkspace(ctx, t)

	conv, err := svc.Create(ctx, wsID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if conv.ID == uuid.Nil {
		t.Fatal("expected a conversation ID")
	}
	if conv.WorkspaceID != wsID {
		t.Errorf("workspace_id: got %s, want %s", conv.WorkspaceID, wsID)
	}
	if conv.Title != "" {
		t.Errorf("expected an untitled conversation, got title %q", conv.Title)
	}
}

func TestGetByID_NotFound(t *testing.T) {
	ctx := context.Background()
	svc := NewConversationService(sharedPool)

	_, err := svc.GetByID(ctx, uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetByID on missing conversation = %v, want ErrNotFound", err)
	}
}

func TestList_OrdersByMostRecentActivity(t *testing.T) {
	ctx := context.Background()
	svc := NewConversationService(sharedPool)
	wsID := insertWorkspace(ctx, t)

	older, err := svc.Create(ctx, wsID)
	if err != nil {
		t.Fatalf("Create older: %v", err)
	}
	newer, err := svc.Create(ctx, wsID)
	if err != nil {
		t.Fatalf("Create newer: %v", err)
	}

	// Bump older's activity so it should now sort ahead of newer.
	if _, err := svc.AppendMessage(ctx, older.ID, model.MessageRoleUser, "hello", nil); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	list, err := svc.List(ctx, wsID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 conversations, got %d", len(list))
	}
	if list[0].ID != older.ID {
		t.Errorf("expected the recently-active conversation first, got %s", list[0].ID)
	}
	if list[1].ID != newer.ID {
		t.Errorf("expected the untouched conversation second, got %s", list[1].ID)
	}
}

func TestAppendMessage_DerivesTitleFromFirstMessage(t *testing.T) {
	ctx := context.Background()
	svc := NewConversationService(sharedPool)
	wsID := insertWorkspace(ctx, t)

	conv, err := svc.Create(ctx, wsID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := svc.AppendMessage(ctx, conv.ID, model.MessageRoleUser, "  what is   NeuralVault?  ", nil); err != nil {
		t.Fatalf("AppendMessage(user): %v", err)
	}
	if _, err := svc.AppendMessage(ctx, conv.ID, model.MessageRoleAssistant, "It's a memory platform.", []byte(`{"results":[]}`)); err != nil {
		t.Fatalf("AppendMessage(assistant): %v", err)
	}

	got, err := svc.GetByID(ctx, conv.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Title != "what is NeuralVault?" {
		t.Errorf("title: got %q", got.Title)
	}

	msgs, err := svc.ListMessages(ctx, conv.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != model.MessageRoleUser || msgs[1].Role != model.MessageRoleAssistant {
		t.Errorf("expected user then assistant, got %v then %v", msgs[0].Role, msgs[1].Role)
	}
	// JSONB round-trips through Postgres with its own formatting (e.g. a space
	// after ":"), so compare decoded values rather than raw bytes.
	var sources struct {
		Results []any `json:"results"`
	}
	if err := json.Unmarshal(msgs[1].Sources, &sources); err != nil {
		t.Fatalf("decoding sources: %v", err)
	}
	if sources.Results == nil || len(sources.Results) != 0 {
		t.Errorf("sources.results: got %v, want an empty slice", sources.Results)
	}
}

func TestAppendMessage_TitleUnchangedAfterFirstMessage(t *testing.T) {
	ctx := context.Background()
	svc := NewConversationService(sharedPool)
	wsID := insertWorkspace(ctx, t)

	conv, err := svc.Create(ctx, wsID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := svc.AppendMessage(ctx, conv.ID, model.MessageRoleUser, "first question", nil); err != nil {
		t.Fatalf("AppendMessage(1): %v", err)
	}
	if _, err := svc.AppendMessage(ctx, conv.ID, model.MessageRoleUser, "second question", nil); err != nil {
		t.Fatalf("AppendMessage(2): %v", err)
	}

	got, err := svc.GetByID(ctx, conv.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Title != "first question" {
		t.Errorf("title should stay derived from the first message, got %q", got.Title)
	}
}
