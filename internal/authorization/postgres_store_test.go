package authorization

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
)

type migrationConnector struct{ statement *string }

func (connector migrationConnector) Connect(context.Context) (driver.Conn, error) {
	return migrationConn{statement: connector.statement}, nil
}
func (migrationConnector) Driver() driver.Driver { return migrationDriver{} }

type migrationDriver struct{}

func (migrationDriver) Open(string) (driver.Conn, error) { return nil, io.EOF }

type migrationConn struct{ statement *string }

func (connection migrationConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not supported")
}
func (migrationConn) Close() error              { return nil }
func (migrationConn) Begin() (driver.Tx, error) { return nil, errors.New("not supported") }
func (connection migrationConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	*connection.statement = query
	return driver.RowsAffected(0), nil
}

func TestPostgresSnapshotStoreMigrationAndValidation(t *testing.T) {
	if _, err := NewPostgresSnapshotStore(nil, "default"); err == nil {
		t.Fatal("accepted nil database")
	}
	statement := ""
	database := sql.OpenDB(migrationConnector{statement: &statement})
	defer database.Close()
	store, err := NewPostgresSnapshotStore(database, "default")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"CREATE TABLE IF NOT EXISTS", "store_key TEXT PRIMARY KEY", "snapshot_json JSONB", "revision BIGINT"} {
		if !strings.Contains(statement, required) {
			t.Fatalf("migration missing %q: %s", required, statement)
		}
	}
	if _, err := store.SaveIfRevision(context.Background(), NewPolicy(nil), -1); !errors.Is(err, ErrSnapshotConflict) {
		t.Fatalf("negative revision = %v", err)
	}
}
