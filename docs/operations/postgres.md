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

## Deployment checklist

1. Create a dedicated database role with only the schema privileges required by the authorization store; require TLS outside a private loopback fixture.
2. Run `Migrate` from one deployment instance before admitting traffic. A second call must remain idempotent.
3. Take a database-native backup and record the active authorization revision before deploying a new binary.
4. Start two application instances and verify that a revision written through one is immediately readable through the other.
5. Exercise one expected-revision conflict and confirm it is reported as a conflict rather than silently overwriting data.
6. Revoke a fixture membership through one instance and confirm the other instance rejects it before declaring the deployment healthy.
7. On application rollback, retain the additive schema and restore the recorded snapshot only when its revision policy permits it. Never drop the table as an automated rollback step.

The harness does not claim PostgreSQL acceptance unless it connects to a real service. A `SKIP` caused by an absent DSN or Docker is expected for local-only development and must remain visible in release evidence.
