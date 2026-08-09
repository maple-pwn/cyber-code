# PostgreSQL Authorization Store

Remote multi-instance deployments can use `PostgresSnapshotStore` with a caller-owned `database/sql` pool. Local-only deployments continue to use the portable file store and do not require PostgreSQL.

## Acceptance test

Run against a temporary Docker database:

```bash
sh scripts/test-postgres-integration.sh
```

Run against an existing database:

```bash
CYBER_CODE_TEST_POSTGRES_DSN='postgres://user:password@host/database?sslmode=require' \
  go test ./internal/authorization -run PostgreSQLIntegration -count=1 -v
```

Without Docker or `CYBER_CODE_TEST_POSTGRES_DSN`, the test reports an explicit `SKIP`; that is not production acceptance evidence.

## Migration and rollback

`Migrate` is idempotent and uses additive `CREATE TABLE IF NOT EXISTS` DDL. Application rollback keeps the table and snapshot JSON intact; cyber-code does not automatically drop authorization data. Before a release, copy the active snapshot to a separate store key or take a database backup, deploy the new binary, and verify `LoadRevision` from a second instance.

The integration suite verifies:

- two independent connection pools see the same snapshot;
- concurrent writes at one expected revision produce one success and one conflict;
- PostgreSQL serialization failures are normalized to revision conflicts;
- membership revocation written by one instance is immediately visible to another;
- migration can be run repeatedly;
- migration reruns preserve the active snapshot for application rollback;
- a revoked final snapshot can be copied to a backup key and restored;
- revisions remain monotonic.
