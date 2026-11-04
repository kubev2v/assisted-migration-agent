# Offline database migration recovery (OMA-3107)

The agent provides an offline migration CLI for a host-coordinated appliance
upgrade. **Stop the agent and all other database users before invoking it.**
This change supplies the migration engine and CLI; maintenance/probation mode
and the host's activation/reboot protocol are follow-up work. It does not make
Podman's existing automatic image rollback safe for schema-changing upgrades.

The host must retain both images by immutable image ID. Run every command below
from the **candidate image**, including rollback after candidate startup fails.
The previous image does not contain the new down migrations.

## Startup migration policy

`agent run` defaults to `--auto-migrate=true`, preserving startup initialization
and forward migration for standalone containers. Host-managed activation can use
`agent run --auto-migrate=false` (or `AGENT_AUTO_MIGRATE=false`) after the offline
CLI has applied migrations. An explicit CLI flag overrides the environment.

Startup discovers collection files within the literal data directory, including
directory names containing glob characters. Automatic startup skips malformed
collection filenames; strict startup and offline recovery reject them.

With automatic migration disabled, startup requires an existing main database
and validates that the main and every discovered collection database have the
exact migration history expected by this image. Missing history, pending or
unknown versions, unreadable collections, and unsupported collection filenames
fail startup without applying SQL migrations. Migration-history validation
finishes before stale collection cleanup can remove files or lifecycle markers.
A fresh appliance must initialize its main database before using this mode; standalone startup
with the default setting still performs that initialization. Newly created
collections continue to initialize their schemas as part of collection creation.

This flag controls startup migrations only. It does not suppress ordinary
startup cleanup, API writes, or background work and is not upgrade probation.
Stale collections are excluded from the pool before cleanup. Cleanup attempts all
stale files and reports deletion failures; strict startup fails on these errors,
while automatic startup logs them and continues.
The separate RVTools Analyzer retains its existing automatic migration behavior.

## Offline recovery sequence

```sh
agent migrate prepare --data-folder /var/lib/agent --attempt-id <uuid> \
  --previous-image sha256:<old-id> --candidate-image sha256:<candidate-id>
agent migrate apply --data-folder /var/lib/agent --attempt-id <uuid>
# Start and validate the candidate without enabling ordinary writes (follow-up).
# That host-managed startup must pass --auto-migrate=false.
# If upgrade fails, stop it and invoke:
agent migrate rollback --data-folder /var/lib/agent --attempt-id <uuid> --dry-run
agent migrate rollback --data-folder /var/lib/agent --attempt-id <uuid>
agent migrate verify --data-folder /var/lib/agent --attempt-id <uuid> --target baseline
# Only after verified database recovery may the host start the previous image.
```

Replace each image placeholder with a complete immutable image ID:
`sha256:` followed by 64 lowercase hexadecimal digits. Attempt IDs contain
1–64 letters, digits, underscores, or hyphens and begin with a letter or digit.

Offline recovery supports databases already at main version 001 and collection
version 002 or newer. `prepare` rejects any older collection before publishing a
manifest or applying migrations. Every persisted collection must meet this
baseline, regardless of the running image version. Standalone forward migration
and fresh database initialization retain the historical up SQL.

`prepare` captures `agent.duckdb` and every `collection_*.duckdb` file, including
its migration versions, in `updates/attempts/<uuid>/manifest.json`. It records
migration registry checksums without inspecting schema objects. Preparation
rejects unreadable/uninitialized databases; it does not silently exclude them.
It never replaces an existing attempt, even after interrupted preparation.
Manifest publication uses fsync and atomic rename. Keep the whole attempt
until the host has resolved the upgrade.

`apply` preflights all database histories before applying ascending forward
steps. Each step's SQL and history update share a transaction. An error leaves
previously committed steps in place for recovery. No normal application
bootstrap, startup cleanup, extension loading, or network access is performed.

`rollback` checks every database's migration history before executing down SQL.
Steps commit individually, newest first, and collections recover before the main
database. Each step's SQL and history update
share a transaction; a failing step rolls back, while earlier committed steps
remain available for a retry. There is no SQL preview or runtime schema-equality
check. Down SQL must be transactional and must not have external side effects.
Cross-database migration dependencies are unsupported.

`--dry-run` reads the manifest and histories and reports planned steps without
executing SQL migrations. It does not prove that down SQL will succeed and
creates no tables, manifest, lock file, or database. Executing rollback and
`verify` check migration versions against the baseline. They do not prove schema
correctness, data integrity, or previous-image usability. Test the up/down SQL
and data preservation; application usability checks belong to the follow-up
maintenance/probation lifecycle.

A retry re-reads committed history and skips completed steps. Nothing depends
on a separate progress file being written between SQL commits. A database with
unknown/gapped history, missing history/files, a changed file inventory, a
symlink database, or a mismatched candidate registry fails preflight. Rollback
never initializes missing databases and never goes below each database's recorded
starting version. Main version 001 and collection versions 001/002 are permanent
history and cannot be reversed. The minimum is checked both during preparation
and when loading a manifest.
The directory lock serializes these offline commands; DuckDB file locks reject
other processes holding databases. Full application/CLI data exclusion belongs
to the follow-up maintenance implementation.

Each executed command writes one JSON result to stdout. `steps` lists planned
steps for dry-run and attempted steps for execution; `versions` lists observed
or successfully committed versions. Errors return a nonzero exit status and
include an `error` field. Cobra flag/argument errors are reported on stderr.
Recovery needs no JWT, UI, OPA policies, vCenter configuration, or network.

## Adding migrations

Files live in separate `up` and `down` directories for each schema:

```text
internal/store/migrations/main/up/002_example.sql
internal/store/migrations/main/down/002_example.sql
internal/store/migrations/collection/up/003_example.sql
internal/store/migrations/collection/down/003_example.sql
```

Existing `001_initial.sql` / `002_storage_metadata.sql` forward files move into
`up/` without changing their SQL or version IDs. The permanent baseline is main
001 and collection 002; their down files contain only comments explaining why
rollback never reverses them. Comment-only down files are allowed only for these
baseline migrations. Every newer migration needs matching SQL files with the
same name in `up/` and `down/`, with executable SQL in both directions.
Versions remain contiguous from 1. The loader rejects missing/empty required pairs,
malformed names, and duplicate versions before running SQL. Embedded-registry
validation is also exercised by unit tests in CI.

Both directions contain plain SQL. Rollback executes only migrations newer than
the database's recorded starting version, so it never reverses collection
migration 002 or removes its pre-existing storage metadata. No schema snapshots,
object-existence flags, or SQL templates are needed. Future migrations must have
a well-defined inverse for every supported starting state.

Keep released forward migrations immutable and add a new version for changes.
Retain data required to reverse destructive transformations until the host
commits the upgrade; a down file alone cannot recover lost values.

## Host follow-up contract

Load candidate → stop old agent → prepare baseline → apply migrations → start
candidate in probation → validate previously usable collections → durably commit
→ enable normal work.

On failure before commit: stop candidate and disable restart → run candidate
rollback CLI → verify baseline → select/start old image → validate old image →
publish `rolled-back`. A failed rollback leaves the agent stopped and retains
both images, data, archive, and attempt. Disable automatic image rollback, using
`--rollback=false` if keeping `podman auto-update` for activation. Never start the
old image first, or roll back migrations after normal writes resume.

The CLI does not manage the host's commit decision, image selection, status
publication, readiness, reboot gate, snapshots, or engine-format downgrade.
Those are prerequisites for automatically invoking this capability on appliances.
