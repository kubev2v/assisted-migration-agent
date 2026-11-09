# Job draining and offline saved-data validation (OMA-3107)

This adds two agent-side operations to the offline migration CLI:

- Wait for ongoing work before the host stops the agent.
- Check that the candidate can read existing data before normal work starts.

The host owns container shutdown, migration, image selection, and automatic
rollback. The agent does not activate images or decide when to recover.
There is no probation server, release endpoint, persisted host authorization,
or additional application data-directory lock.

## Drain ongoing work

The host sends `PUT /api/v2/upgrade` with:

```json
{
  "action": "maintenance",
  "attempt_id": "upgrade-123",
  "previous_image": "sha256:<64 lowercase hex digits>",
  "candidate_image": "sha256:<64 lowercase hex digits>"
}
```

Both PUT and GET require a real loopback peer and `X-Upgrade-Token`, read by the
host from `/var/lib/agent/updates/control-token`. Forwarded IP headers do not
qualify. The token prevents network clients from disabling ordinary API access.
Image IDs and attempt IDs associate the saved-data baseline with the host's
migration attempt; the host must pass the actual immutable image IDs.

Admission closes immediately: newly arriving ordinary API calls return 503.
Already admitted handlers, including uploads, finish normally. Collection,
inspection, and forecasting jobs finish before services and database connections
close. Collector finalization is included even if its UI state is already
terminal. GET handlers also remain blocked because some initialize services.
Version and upgrade-control endpoints remain available.

The host polls `GET /api/v2/upgrade` until:

- `maintenance`: work has finished, services have closed, and the source's
  read-only saved-data baseline is durable at
  `updates/validation/<attempt_id>.json`. The host may now stop the container.
- `failed`, `services_closed: false`: draining timed out without canceling jobs.
  Abort the upgrade and send `action: resume` with the same attempt ID to reopen
  API admission. Do not stop the container or run migrations on this path.
- `failed`, `services_closed: true`: jobs finished but baseline capture failed.
  Abort without running migrations and restart the source container. Services
  cannot resume in the same process after they have closed.

`agent run --maintenance-timeout` defaults to five minutes. Expiry aborts the
upgrade attempt; it does not impose a deadline on the jobs themselves. Once
maintenance succeeds, normal operation resumes through a fresh container start.
If the source cannot supply this drain protocol, the host must decline an
automatic upgrade rather than stop potentially active jobs.

## Validate saved data offline

After stopping the agent and every other data consumer and confirming their
exit, the host runs the candidate image's existing `migrate prepare` and
`migrate apply` commands. It then invokes the candidate binary directly,
bypassing the image entrypoint:

```sh
/app/agent upgrade validate --data-folder /var/lib/agent \
  --attempt-id upgrade-123 --image-id sha256:<candidate-id>
```

Use a one-shot container with the existing data directory mounted read-only,
no network, and `--entrypoint /app/agent`. Validation does not run application
bootstrap, apply migrations, clean up stale collections, load extensions,
initialize credentials, or copy bundled files. The containerfiles and ordinary
entrypoint behavior are unchanged.

The command checks this image's expected migration histories and exercises its
store reads for configuration, collection metadata, inventory, VM data and
annotations, groups, inspection, rightsizing, forecasts, and credentials. It
compares database inventory and saved-record counts with the source baseline,
and checks retained credential-key and VDDK asset hashes. Device-key-managed
credentials must decrypt; credentials requiring an interactive master password
fail this unattended check.

Exit status zero means these checks passed. A validation error returns nonzero;
stdout contains one JSON result with `contract_version`, `attempt_id`, `image_id`,
`valid`, and an optional `error`. `--timeout` defaults to five minutes. The
baseline and migration manifest must identify the same attempt and image pair.
The host supplies the ID of the image actually running the command.

This tests application reads in addition to successful SQL migration execution.
It is not a complete integrity scan of every record, a writable-service startup
test, or a guarantee against failures in external dependencies. Keep checks
aligned with the stores when their data formats change.

## Host sequence and recovery

1. Drain the source and wait for `maintenance`. On timeout, resume admission and
   abort; ongoing jobs continue.
2. Stop all data consumers and confirm exit; prevent automatic restart while
   operating offline. Retain both images by immutable ID.
3. Run candidate `migrate prepare` and `migrate apply` for the drained attempt.
4. Run candidate `upgrade validate` offline. Do not start normal candidate work
   before it passes.
5. On success, let the host durably accept the candidate and start it with
   `--auto-migrate=false`.
6. On apply or validation failure, use the **candidate** image's `migrate rollback`
   and `migrate verify --target baseline`. Then run the previous image's
   `upgrade validate` against the recovered data before starting the previous
   container. If recovery fails, keep the containers stopped and report failure.

The host implementation in migration-planner must call these operations for the
end-to-end guarantees. Supported source images must include the drain API and
offline validation command; upgrading existing appliances to that baseline is a
separate rollout step. SQL down migrations must preserve recoverable data;
validation cannot reconstruct data discarded by a destructive migration.
Once normal candidate writes begin, this pre-acceptance recovery window ends.
The offline migration contract and supported baseline versions are documented
in [database-migration-rollback.md](database-migration-rollback.md).
