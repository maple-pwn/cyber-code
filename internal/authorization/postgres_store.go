package authorization

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var ErrSnapshotConflict = errors.New("authorization_snapshot_conflict")

const postgresAuthorizationMigration = `
CREATE TABLE IF NOT EXISTS cyber_authorization_snapshots (
    store_key TEXT PRIMARY KEY,
    revision BIGINT NOT NULL CHECK (revision > 0),
    snapshot_json JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);`

// PostgresSnapshotStore provides transactionally serialized authorization
// snapshots for multi-instance remote deployments. The caller owns the SQL
// driver and connection pool.
type PostgresSnapshotStore struct {
	db  *sql.DB
	key string
}

func NewPostgresSnapshotStore(db *sql.DB, key string) (*PostgresSnapshotStore, error) {
	if db == nil || strings.TrimSpace(key) == "" || len(key) > 128 || strings.ContainsAny(key, "\x00\r\n") {
		return nil, fmt.Errorf("postgres authorization database and store key are required")
	}
	return &PostgresSnapshotStore{db: db, key: key}, nil
}

func (s *PostgresSnapshotStore) Migrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres authorization store is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, err := s.db.ExecContext(ctx, postgresAuthorizationMigration); err != nil {
		return fmt.Errorf("migrate authorization snapshots: %w", err)
	}
	return nil
}

func (s *PostgresSnapshotStore) Save(policy *Policy) error {
	_, err := s.save(context.Background(), policy, nil)
	return err
}

func (s *PostgresSnapshotStore) SaveIfRevision(ctx context.Context, policy *Policy, expected int64) (int64, error) {
	if expected < 0 {
		return 0, ErrSnapshotConflict
	}
	return s.save(ctx, policy, &expected)
}

func (s *PostgresSnapshotStore) save(ctx context.Context, policy *Policy, expected *int64) (revision int64, returnErr error) {
	if s == nil || s.db == nil || policy == nil {
		return 0, fmt.Errorf("postgres authorization store and policy are required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	payload, err := json.Marshal(policy.Snapshot())
	if err != nil {
		return 0, fmt.Errorf("encode authorization snapshot: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return 0, fmt.Errorf("begin authorization snapshot transaction: %w", err)
	}
	defer func() {
		if returnErr != nil {
			_ = tx.Rollback()
		}
	}()
	current := int64(0)
	err = tx.QueryRowContext(ctx, `SELECT revision FROM cyber_authorization_snapshots WHERE store_key = $1 FOR UPDATE`, s.key).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("lock authorization snapshot: %w", err)
	}
	if expected != nil && current != *expected {
		return 0, ErrSnapshotConflict
	}
	revision = current + 1
	_, err = tx.ExecContext(ctx, `
INSERT INTO cyber_authorization_snapshots (store_key, revision, snapshot_json)
VALUES ($1, $2, $3)
ON CONFLICT (store_key) DO UPDATE
SET revision = EXCLUDED.revision, snapshot_json = EXCLUDED.snapshot_json, updated_at = NOW()`, s.key, revision, payload)
	if err != nil {
		return 0, fmt.Errorf("write authorization snapshot: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit authorization snapshot: %w", err)
	}
	return revision, nil
}

func (s *PostgresSnapshotStore) Load() (*Policy, error) {
	policy, _, err := s.LoadRevision(context.Background())
	return policy, err
}

func (s *PostgresSnapshotStore) LoadRevision(ctx context.Context) (*Policy, int64, error) {
	if s == nil || s.db == nil {
		return nil, 0, fmt.Errorf("postgres authorization store is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var payload []byte
	var revision int64
	if err := s.db.QueryRowContext(ctx, `SELECT snapshot_json, revision FROM cyber_authorization_snapshots WHERE store_key = $1`, s.key).Scan(&payload, &revision); err != nil {
		return nil, 0, fmt.Errorf("read authorization snapshot: %w", err)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return nil, 0, fmt.Errorf("decode authorization snapshot: %w", err)
	}
	policy, err := NewPolicyFromSnapshot(snapshot)
	if err != nil {
		return nil, 0, err
	}
	return policy, revision, nil
}

var _ SnapshotStore = (*PostgresSnapshotStore)(nil)
