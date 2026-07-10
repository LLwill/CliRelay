# PostgreSQL / Redis Runtime and Legacy SQLite Import

## Runtime boundary

CliRelay runs exclusively on PostgreSQL 15+, Redis 7+, and Ent ORM.

- PostgreSQL is the only persistent runtime data source.
- Redis stores cache, locks, rate limits, queues, and rebuildable state only.
- SQLite is not opened during normal startup, health checks, blue-green deploys, or OTA updates.
- The application entrypoint does not scan for `usage.db` and does not invoke the legacy import script.
- Stack upgrades remove a stale `clirelay-migrate` service and legacy `CLIRELAY_SQLITE_AUTO_*` application environment entries, but do not delete SQLite files.

For Docker Compose deployments:

```bash
docker compose up -d
```

The stack starts `clirelay-init`, PostgreSQL, Redis, the application, and `clirelay-updater`. OTA state is stored by the updater in `.clirelay-updater-status.json` and streamed to the management API through SSE. SQLite inventory or import is never an OTA stage.

## Who should run the legacy importer

Run the importer only when all of the following are true:

1. The deployment originated from a SQLite-based CliRelay release.
2. Historical data in the old `usage.db` must be retained.
3. PostgreSQL is already provisioned and `CLIRELAY_POSTGRES_DSN` points to the intended target database.
4. The original SQLite file has been backed up and is no longer being written by the old release.

Fresh installations and deployments already running on PostgreSQL must not run the importer.

## Preserved migration tooling

The following compatibility tools remain available:

- `scripts/migrate-sqlite-to-postgres.sh`
- `-sqlite-dry-run`
- `-sqlite-import`
- `-sqlite-import-dry-run=false`

They are manual tools and are not called by the application entrypoint or updater.

The script performs these operations in order:

1. Read-only SQLite inventory.
2. PostgreSQL import dry-run.
3. Apply import, unless `CLIRELAY_SQLITE_AUTO_IMPORT=false` is set.

It never deletes, moves, or writes the SQLite source. PostgreSQL records an import fingerprint in `sqlite_import_runs`, uses an advisory lock to serialize imports, and skips a source that has already been applied successfully.

## Non-Docker import

```bash
CLIRELAY_BIN=/opt/clirelay2/clirelay2 \
CLIRELAY_POSTGRES_DSN='postgres://user:pass@127.0.0.1:5432/cliproxy?sslmode=disable' \
./scripts/migrate-sqlite-to-postgres.sh /opt/clirelay2/usage.db
```

To stop after inventory and PostgreSQL dry-run:

```bash
CLIRELAY_SQLITE_AUTO_IMPORT=false \
CLIRELAY_BIN=/opt/clirelay2/clirelay2 \
CLIRELAY_POSTGRES_DSN='postgres://user:pass@127.0.0.1:5432/cliproxy?sslmode=disable' \
./scripts/migrate-sqlite-to-postgres.sh /opt/clirelay2/usage.db
```

## Docker Compose import

Start the PostgreSQL and Redis services first:

```bash
docker compose up -d postgres redis
```

Mount the old SQLite file read-only into a one-off application container:

```bash
docker compose run --rm --no-deps \
  -e CLIRELAY_BIN=/CLIProxyAPI/CLIProxyAPI \
  -v /absolute/path/to/usage.db:/migration/usage.db:ro \
  cli-proxy-api \
  /usr/local/bin/migrate-sqlite-to-postgres.sh /migration/usage.db
```

Dry-run only:

```bash
docker compose run --rm --no-deps \
  -e CLIRELAY_BIN=/CLIProxyAPI/CLIProxyAPI \
  -e CLIRELAY_SQLITE_AUTO_IMPORT=false \
  -v /absolute/path/to/usage.db:/migration/usage.db:ro \
  cli-proxy-api \
  /usr/local/bin/migrate-sqlite-to-postgres.sh /migration/usage.db
```

## Separate inventory and apply commands

```bash
./cli-proxy-api -sqlite-dry-run /path/to/usage.db

CLIRELAY_POSTGRES_DSN='postgres://user:pass@127.0.0.1:5432/cliproxy?sslmode=disable' \
./cli-proxy-api -sqlite-import /path/to/usage.db

CLIRELAY_POSTGRES_DSN='postgres://user:pass@127.0.0.1:5432/cliproxy?sslmode=disable' \
./cli-proxy-api -sqlite-import /path/to/usage.db -sqlite-import-dry-run=false
```

Inventory and dry-run output should be reviewed for table coverage, source and target columns, row counts, ID/time ranges, checksums, and planned inserts before apply.

## Old Compose and updater transition

For a SQLite-only Docker deployment:

1. Back up `usage.db`, `docker-compose.yml`, `.env`, and PostgreSQL data if it already exists.
2. Replace `docker-compose.yml` with the current repository version.
3. Start the runtime dependencies and updater:

   ```bash
   docker compose up -d postgres redis clirelay-updater
   ```

4. Run the manual SQLite import if historical data is required.
5. Start or recreate the application service.

An updater sidecar from a release that predates the SSE protocol must be recreated once:

```bash
docker compose up -d --force-recreate clirelay-updater
```

After that transition, the management panel receives real updater snapshots and can recover after API-container restarts or temporary SSE disconnects.

## Validation

Run the repository tests:

```bash
rtk go test ./cmd/updater -count=1
rtk go test ./internal/management/updateflow ./internal/api/handlers/management -count=1
rtk go test ./internal/storage/postgres/... ./internal/usage -count=1
rtk go test ./...
```

With integration services available:

```bash
CLIRELAY_POSTGRES_TEST_DSN='postgres://cliproxy:cliproxy@127.0.0.1:55432/cliproxy?sslmode=disable' \
CLIRELAY_REDIS_TEST_ADDR='127.0.0.1:56379' \
rtk go test ./internal/usage -run TestPostgresRuntimeDataStackIntegration -count=1 -v

CLIRELAY_POSTGRES_TEST_DSN='postgres://cliproxy:cliproxy@127.0.0.1:55432/cliproxy?sslmode=disable' \
rtk go test ./internal/storage/postgres/sqliteinventory -run TestImportSQLiteDryRunAndApply -count=1 -v
```

Keep the original SQLite file as a read-only backup until PostgreSQL row counts, checksums, key CRUD paths, management queries, request logs, quota state, and identity data have been verified.
