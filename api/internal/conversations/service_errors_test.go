package conversations

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jpgomesr/NeuralVault/internal/model"
)

// errPool is a storage.Pool that fails the operations a test selects, so the
// service's error-wrapping branches can be exercised without a database.
type errPool struct {
	execErr     error
	queryErr    error
	queryRowErr error
	beginErr    error
	tx          pgx.Tx
}

func (p errPool) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, p.execErr
}
func (p errPool) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, p.queryErr }
func (p errPool) QueryRow(context.Context, string, ...any) pgx.Row        { return errRow{err: p.queryRowErr} }
func (p errPool) Begin(context.Context) (pgx.Tx, error) {
	if p.beginErr != nil {
		return nil, p.beginErr
	}
	return p.tx, nil
}
func (p errPool) Ping(context.Context) error { return nil }
func (p errPool) Close()                     {}

// errRow is a pgx.Row whose Scan returns a fixed error.
type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

// fakeTx is a pgx.Tx whose Exec/Commit behavior is controlled per test, so
// AppendMessage's transaction-internal error branches can be exercised
// without a database. Embeds pgx.Tx (nil) — AppendMessage only ever calls
// Exec, Commit, and (deferred, ignored) Rollback.
type fakeTx struct {
	pgx.Tx
	// failExecOnCall, when > 0, fails only the Nth Exec call (1-indexed):
	// AppendMessage's first call inserts the message, its second updates the
	// conversation. 0 means Exec always succeeds.
	failExecOnCall int
	execErr        error
	commitErr      error

	execCalls int
}

func (t *fakeTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	t.execCalls++
	if t.failExecOnCall != 0 && t.execCalls == t.failExecOnCall {
		return pgconn.CommandTag{}, t.execErr
	}
	return pgconn.CommandTag{}, nil
}
func (t *fakeTx) Commit(context.Context) error   { return t.commitErr }
func (t *fakeTx) Rollback(context.Context) error { return nil }

func TestCreate_ExecError(t *testing.T) {
	svc := &ConversationService{pool: errPool{execErr: errors.New("insert failed")}}

	_, err := svc.Create(context.Background(), uuid.New())
	if err == nil || !strings.Contains(err.Error(), "insert conversation") {
		t.Fatalf("Create: got %v, want an insert conversation error", err)
	}
}

func TestList_QueryError(t *testing.T) {
	svc := &ConversationService{pool: errPool{queryErr: errors.New("query failed")}}

	_, err := svc.List(context.Background(), uuid.New())
	if err == nil || !strings.Contains(err.Error(), "querying conversations") {
		t.Fatalf("List: got %v, want a querying conversations error", err)
	}
}

func TestGetByID_QueryRowError(t *testing.T) {
	svc := &ConversationService{pool: errPool{queryRowErr: errors.New("connection reset")}}

	_, err := svc.GetByID(context.Background(), uuid.New())
	if err == nil || errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "get conversation by id") {
		t.Fatalf("GetByID: got %v, want a get conversation by id error", err)
	}
}

func TestListMessages_QueryError(t *testing.T) {
	svc := &ConversationService{pool: errPool{queryErr: errors.New("query failed")}}

	_, err := svc.ListMessages(context.Background(), uuid.New())
	if err == nil || !strings.Contains(err.Error(), "querying messages") {
		t.Fatalf("ListMessages: got %v, want a querying messages error", err)
	}
}

func TestAppendMessage_BeginError(t *testing.T) {
	svc := &ConversationService{pool: errPool{beginErr: errors.New("no connection")}}

	_, err := svc.AppendMessage(context.Background(), uuid.New(), model.MessageRoleUser, "hi", nil)
	if err == nil || !strings.Contains(err.Error(), "begin tx") {
		t.Fatalf("AppendMessage: got %v, want a begin tx error", err)
	}
}

func TestAppendMessage_InsertError(t *testing.T) {
	tx := &fakeTx{failExecOnCall: 1, execErr: errors.New("insert failed")}
	svc := &ConversationService{pool: errPool{tx: tx}}

	_, err := svc.AppendMessage(context.Background(), uuid.New(), model.MessageRoleUser, "hi", nil)
	if err == nil || !strings.Contains(err.Error(), "insert message") {
		t.Fatalf("AppendMessage: got %v, want an insert message error", err)
	}
}

func TestAppendMessage_UpdateConversationError(t *testing.T) {
	tx := &fakeTx{failExecOnCall: 2, execErr: errors.New("update failed")}
	svc := &ConversationService{pool: errPool{tx: tx}}

	_, err := svc.AppendMessage(context.Background(), uuid.New(), model.MessageRoleUser, "hi", nil)
	if err == nil || !strings.Contains(err.Error(), "update conversation") {
		t.Fatalf("AppendMessage: got %v, want an update conversation error", err)
	}
}

func TestAppendMessage_CommitError(t *testing.T) {
	tx := &fakeTx{commitErr: errors.New("commit failed")}
	svc := &ConversationService{pool: errPool{tx: tx}}

	_, err := svc.AppendMessage(context.Background(), uuid.New(), model.MessageRoleUser, "hi", nil)
	if err == nil || !strings.Contains(err.Error(), "commit tx") {
		t.Fatalf("AppendMessage: got %v, want a commit tx error", err)
	}
}

func TestDeriveTitle_TruncatesLongContent(t *testing.T) {
	long := strings.Repeat("a", 100)

	got := deriveTitle(long)

	runes := []rune(got)
	wantLen := titleMaxRunes + len([]rune("…"))
	if len(runes) != wantLen {
		t.Fatalf("deriveTitle length: got %d runes, want %d", len(runes), wantLen)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected a truncated title to end with an ellipsis, got %q", got)
	}
}
