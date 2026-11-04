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

With automatic migration disabled, startup requires an existing main database
and validates that the main and every discovered collection database have the
exact migration history expected by this image. Missing history, pending or
unknown versions, unreadable collections, and unsupported collection filenames
fail startup without applying SQL migrations. Schema validation finishes before
stale collection cleanup can remove files or lifecycle markers. A fresh appliance
must initialize its main database before using this mode; standalone startup
with the default setting still performs that initialization. Newly created
collections continue to initialize their schemas as part of collection creation.

This flag controls startup migrations only. It does not suppress ordinary
startup cleanup, API writes, or background work and is not upgrade probation.
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

`prepare` captures `agent.duckdb` and every `collection_*.duckdb` file, including
its versions and schema columns, in
`updates/attempts/<uuid>/manifest.json`. It records migration registry checksums
and which objects already existed before conditional migrations. Preparation
rejects unreadable/uninitialized databases; it does not silently exclude them.
It never replaces an existing attempt, even after interrupted preparation.
Manifest publication uses fsync and atomic rename. Keep the whole attempt
until the host has resolved the upgrade.

`apply` preflights all database histories before applying ascending forward
steps. Each step's SQL and history update share a transaction. An error leaves
previously committed steps in place for recovery. No normal application
bootstrap, startup cleanup, extension loading, or network access is performed.

`rollback` validates every database and renders its down plan before committing
any down SQL. For an executing rollback it also previews each complete down
path in a transaction that is rolled back, checking the resulting baseline
columns and versions. This catches down SQL errors and schema drift before
committed recovery begins. Down SQL therefore must be transactional and must
not have external side effects. After preflight, steps commit individually,
newest first, and collections recover before the main database. Cross-database
migration dependencies are unsupported.

`--dry-run` only reads the manifest, histories, and schemas, and reports planned
steps. It does not run the transactional preview or prove that down SQL will
succeed. It creates no tables, manifest, lock file, or database. Executing
rollback and `verify` compare final column names, order, types, nullability,
defaults, and versions with the baseline. This is not a full data integrity,
constraint, index, or previous-image usability check.

A retry re-reads committed history and skips completed steps. Nothing depends
on a separate progress file being written between SQL commits. A database with
unknown/gapped history, missing history/files, a changed file inventory, a
symlink database, or a mismatched candidate registry fails preflight. Rollback
never initializes missing databases and never goes below initialized version 1.
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
`up/` without changing their SQL or version IDs. Every version needs matching
files with the same name in `up/` and `down/`;
versions are contiguous from 1. The loader rejects missing/empty pairs,
malformed names, duplicate versions, and invalid down templates before running
SQL. Embedded-registry validation is also exercised by unit tests in CI.

Forward files contain SQL. Down files contain SQL, optionally using Go templates
with `.Tables` and `.Columns` maps from the captured baseline. Map keys are table
names and `table.column` names. Templates are embedded code, not SQL loaded from
the manifest. For example:

```sql
{{if not (index .Columns "example.optional")}}
ALTER TABLE example DROP COLUMN IF EXISTS optional;
{{end}}
```

Collection migration 002 uses this provenance to preserve parser-owned tables
and columns that existed before the upgrade. Initial down partners are present
for registry completeness; the appliance recovery API never executes them.
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
