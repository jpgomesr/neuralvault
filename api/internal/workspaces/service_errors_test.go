package workspaces

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// errPool is a storage.Pool that fails the operations a test selects, so the
// service's error-wrapping branches can be exercised without a database.
type errPool struct {
	beginErr    error
	queryErr    error
	queryRowErr error
}

func (p errPool) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (p errPool) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, p.queryErr }
func (p errPool) QueryRow(context.Context, string, ...any) pgx.Row        { return errRow{err: p.queryRowErr} }
func (p errPool) Begin(context.Context) (pgx.Tx, error)                   { return nil, p.beginErr }
func (p errPool) Ping(context.Context) error                              { return nil }
func (p errPool) Close()                                                  {}

// errRow is a pgx.Row whose Scan returns a fixed error.
type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

func TestCreate_BeginError(t *testing.T) {
	svc := &WorkspaceService{pool: errPool{beginErr: errors.New("no connection")}}

	_, err := svc.Create(context.Background(), uuid.New(), "WS")
	if err == nil || !strings.Contains(err.Error(), "begin tx") {
		t.Fatalf("Create: got %v, want a begin tx error", err)
	}
}

func TestList_QueryError(t *testing.T) {
	svc := &WorkspaceService{pool: errPool{queryErr: errors.New("query failed")}}

	_, err := svc.List(context.Background(), uuid.New())
	if err == nil || !strings.Contains(err.Error(), "querying workspaces") {
		t.Fatalf("List: got %v, want a querying workspaces error", err)
	}
}

func TestIsMember_QueryError(t *testing.T) {
	svc := &WorkspaceService{pool: errPool{queryRowErr: errors.New("scan failed")}}

	_, err := svc.IsMember(context.Background(), uuid.New(), uuid.New())
	if err == nil || !strings.Contains(err.Error(), "checking membership") {
		t.Fatalf("IsMember: got %v, want a checking membership error", err)
	}
}
