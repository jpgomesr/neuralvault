package workspaces

import (
	"context"
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

	"github.com/jpgomesr/NeuralVault/internal/config"
	pgstore "github.com/jpgomesr/NeuralVault/internal/storage/postgres"
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

// insertUser inserts a users row (satisfying the user_workspace FK) and
// schedules its cleanup.
func insertUser(ctx context.Context, t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	email := fmt.Sprintf("user-%s@example.com", id)
	if _, err := sharedPool.Exec(ctx, "INSERT INTO users (id, email, name) VALUES ($1, $2, $3)", id, email, "Test User"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = sharedPool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", id)
	})
	return id
}

func TestCreate_MakesCreatorOwner(t *testing.T) {
	ctx := context.Background()
	svc := NewWorkspaceService(sharedPool)
	userID := insertUser(ctx, t)

	ws, err := svc.Create(ctx, userID, "My Workspace")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = sharedPool.Exec(context.Background(), "DELETE FROM user_workspace WHERE workspace_id = $1", ws.ID)
		_, _ = sharedPool.Exec(context.Background(), "DELETE FROM workspace WHERE id = $1", ws.ID)
	})

	if ws.ID == uuid.Nil {
		t.Fatal("expected a workspace ID")
	}
	if ws.Name != "My Workspace" {
		t.Errorf("name: got %q", ws.Name)
	}

	var role string
	err = sharedPool.QueryRow(ctx,
		"SELECT role FROM user_workspace WHERE user_id = $1 AND workspace_id = $2", userID, ws.ID,
	).Scan(&role)
	if err != nil {
		t.Fatalf("querying membership: %v", err)
	}
	if role != "owner" {
		t.Errorf("expected creator role 'owner', got %q", role)
	}
}

func TestIsMember(t *testing.T) {
	ctx := context.Background()
	svc := NewWorkspaceService(sharedPool)

	owner := insertUser(ctx, t)
	outsider := insertUser(ctx, t)

	ws, err := svc.Create(ctx, owner, "Private")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = sharedPool.Exec(context.Background(), "DELETE FROM user_workspace WHERE workspace_id = $1", ws.ID)
		_, _ = sharedPool.Exec(context.Background(), "DELETE FROM workspace WHERE id = $1", ws.ID)
	})

	member, err := svc.IsMember(ctx, owner, ws.ID)
	if err != nil {
		t.Fatalf("IsMember(owner): %v", err)
	}
	if !member {
		t.Error("expected owner to be a member")
	}

	member, err = svc.IsMember(ctx, outsider, ws.ID)
	if err != nil {
		t.Fatalf("IsMember(outsider): %v", err)
	}
	if member {
		t.Error("expected outsider to not be a member")
	}
}

func TestList_ReturnsOnlyJoinedWorkspaces(t *testing.T) {
	ctx := context.Background()
	svc := NewWorkspaceService(sharedPool)

	userA := insertUser(ctx, t)
	userB := insertUser(ctx, t)

	wsA, err := svc.Create(ctx, userA, "A's workspace")
	if err != nil {
		t.Fatalf("Create A: %v", err)
	}
	wsB, err := svc.Create(ctx, userB, "B's workspace")
	if err != nil {
		t.Fatalf("Create B: %v", err)
	}
	t.Cleanup(func() {
		for _, id := range []uuid.UUID{wsA.ID, wsB.ID} {
			_, _ = sharedPool.Exec(context.Background(), "DELETE FROM user_workspace WHERE workspace_id = $1", id)
			_, _ = sharedPool.Exec(context.Background(), "DELETE FROM workspace WHERE id = $1", id)
		}
	})

	list, err := svc.List(ctx, userA)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 workspace for user A, got %d", len(list))
	}
	if list[0].ID != wsA.ID {
		t.Errorf("expected A's workspace, got %s", list[0].ID)
	}
}
