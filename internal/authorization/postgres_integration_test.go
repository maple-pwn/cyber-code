package authorization

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgreSQLIntegrationMultiInstance(t *testing.T) {
	dsn := os.Getenv("CYBER_CODE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CYBER_CODE_TEST_POSTGRES_DSN is not set; real PostgreSQL acceptance was not run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	firstDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer firstDB.Close()
	secondDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer secondDB.Close()
	if err := firstDB.PingContext(ctx); err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	key := fmt.Sprintf("integration-%d", time.Now().UnixNano())
	backupKey := key + "-backup"
	defer firstDB.ExecContext(context.Background(), `DELETE FROM cyber_authorization_snapshots WHERE store_key = $1`, key)
	defer firstDB.ExecContext(context.Background(), `DELETE FROM cyber_authorization_snapshots WHERE store_key = $1`, backupKey)
	first, err := NewPostgresSnapshotStore(firstDB, key)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewPostgresSnapshotStore(secondDB, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := second.Migrate(ctx); err != nil {
		t.Fatalf("idempotent migration: %v", err)
	}
	base := NewPolicy([]Member{{TenantID: "tenant-a", Principal: "owner", Role: RoleOwner, Active: true}})
	revision, err := first.SaveIfRevision(ctx, base, 0)
	if err != nil || revision != 1 {
		t.Fatalf("initial revision=%d err=%v", revision, err)
	}
	left, loadedRevision, err := first.LoadRevision(ctx)
	if err != nil || loadedRevision != 1 {
		t.Fatalf("first load revision=%d err=%v", loadedRevision, err)
	}
	right, loadedRevision, err := second.LoadRevision(ctx)
	if err != nil || loadedRevision != 1 {
		t.Fatalf("second load revision=%d err=%v", loadedRevision, err)
	}
	if err := left.UpdateMemberRole("tenant-a", "owner", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := right.UpdateMemberRole("tenant-a", "owner", RoleAuditor); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var wait sync.WaitGroup
	stores := []*PostgresSnapshotStore{first, second}
	for index, update := range []*Policy{left, right} {
		wait.Add(1)
		go func(store *PostgresSnapshotStore, policy *Policy) {
			defer wait.Done()
			_, saveErr := store.SaveIfRevision(ctx, policy, 1)
			results <- saveErr
		}(stores[index], update)
	}
	wait.Wait()
	close(results)
	successes, conflicts := 0, 0
	for result := range results {
		switch {
		case result == nil:
			successes++
		case errors.Is(result, ErrSnapshotConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent save error: %v", result)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent saves successes=%d conflicts=%d", successes, conflicts)
	}
	finalPolicy, finalRevision, err := first.LoadRevision(ctx)
	if err != nil || finalRevision != 2 || len(finalPolicy.Members("tenant-a")) != 1 {
		t.Fatalf("final revision=%d policy=%#v err=%v", finalRevision, finalPolicy, err)
	}
	if err := finalPolicy.RevokeMember("tenant-a", "owner"); err != nil {
		t.Fatal(err)
	}
	if revokedRevision, err := first.SaveIfRevision(ctx, finalPolicy, finalRevision); err != nil || revokedRevision != 3 {
		t.Fatalf("revoked revision=%d err=%v", revokedRevision, err)
	}
	revokedPolicy, revokedRevision, err := second.LoadRevision(ctx)
	if err != nil || revokedRevision != 3 {
		t.Fatalf("second instance revoked revision=%d err=%v", revokedRevision, err)
	}
	members := revokedPolicy.Members("tenant-a")
	if len(members) != 1 || members[0].Active {
		t.Fatalf("revocation was not visible to second instance: %#v", members)
	}
	if err := second.Migrate(ctx); err != nil {
		t.Fatalf("rollback-compatible migration rerun: %v", err)
	}
	preserved, preservedRevision, err := first.LoadRevision(ctx)
	if err != nil || preservedRevision != 3 || len(preserved.Members("tenant-a")) != 1 || preserved.Members("tenant-a")[0].Active {
		t.Fatalf("migration rerun did not preserve revoked snapshot: revision=%d policy=%#v err=%v", preservedRevision, preserved, err)
	}
	backup, err := NewPostgresSnapshotStore(firstDB, backupKey)
	if err != nil {
		t.Fatal(err)
	}
	if backupRevision, err := backup.SaveIfRevision(ctx, preserved, 0); err != nil || backupRevision != 1 {
		t.Fatalf("backup revision=%d err=%v", backupRevision, err)
	}
	restored, restoredRevision, err := backup.LoadRevision(ctx)
	if err != nil || restoredRevision != 1 || len(restored.Members("tenant-a")) != 1 || restored.Members("tenant-a")[0].Active {
		t.Fatalf("restored revision=%d policy=%#v err=%v", restoredRevision, restored, err)
	}
}
