# Agent lifecycle for host-coordinated appliance upgrades

This implements the agent side of OMA-3107 on top of the offline migration CLI.
The host coordinator, durable commit/reboot gate, tested transition allowlist,
and existing-appliance rollout are separate planner work. Keep automatic
schema-changing upgrades disabled until that complete protocol passes appliance
acceptance. Capability support alone does not authorize a transition.

## Capability probe and process exclusion

`agent upgrade capabilities` prints contract version 1 and the supported feature
names as one JSON document. It needs no JWT, OPA, UI, data directory, network,
or running application. Invoke `/app/agent` directly in an offline container.

The agent and analyzer hold an exclusive lock on the data-directory inode for
their entire process lifetime. The offline migration commands acquire the same
lock. All application/CLI containers must use the same persistent mount and
rootless `core` UID mapping. A lock failure stops startup before opening stores.
Probation keeps the lock through release and normal bootstrap. HTTP shutdown
waits for requests before closing services or releasing that lock.

## Local host controls

Normal startup creates `updates/control-token` under the persistent data mount
with mode 0600. Probation only reads it. The token is deliberately separate
from the agent's console JWT and is never returned in an API response.

The OpenAPI-defined `/api/v2/upgrade` resource requires both a real loopback peer
and `X-Upgrade-Token`. Forwarded client-IP headers cannot authorize a remote
caller. The planner's host coordinator reads the token from the shared mount;
it should keep it out of logs and command-line arguments.

- `GET` returns `contract_version`, `state`, `attempt_id`, `image_id`, and optional
  `error`. Poll this resource after a transition; a 202 is not completion.
- `PUT` with `action: maintenance`, `attempt_id`, `previous_image`, and
  `candidate_image` closes admission immediately and returns 202. Image IDs are
  full immutable Podman `sha256:<64 lowercase hex>` IDs. Repeating the same
  request for the active attempt is safe; another attempt conflicts.
- Maintenance blocks every ordinary API handler with 503, including reads that
  can initialize services. `/version` and authenticated upgrade controls remain
  available. Already admitted requests, including uploads, finish; collection,
  inspection, and forecasting must finish before ready. Collector finalization
  counts as active work even if its UI state is terminal.
- Once work drains, the agent stops background services/reporting, closes its
  pool, and performs strict read-only validation. State becomes `maintenance`
  only after the validation baseline is persisted. The host can now stop the
  process and invoke candidate `migrate prepare` and `apply`.
- A maintenance timeout or invalid baseline yields `failed`; no forward migration
  has run. Admission stays closed until the host explicitly aborts/resumes or
  stops the process. It never silently cancels active work to satisfy the deadline.

`updates/validation/<attempt-id>.json` contains the application validation
baseline, independently of `updates/attempts/<attempt-id>/manifest.json` owned
by the migration engine. Baselines cannot be overwritten. The host must retain
both until the attempt is resolved. A partial baseline after interrupted writing
cannot be treated as ready; readiness follows successful file/directory fsync.

## Host authorization and probation

The host atomically publishes and fsyncs `updates/authorization.json` before
starting the managed process. It is an agent-facing decision derived from the
host's recovery journal, not a replacement for that journal or final status.

```json
{
  "contract_version": 1,
  "attempt_id": "upgrade-123",
  "phase": "validating",
  "previous_image": "sha256:<64 lowercase hex>",
  "candidate_image": "sha256:<64 lowercase hex>"
}
```

For probation use:

```sh
agent run --data-folder /var/lib/agent \
  --upgrade-attempt-id upgrade-123 --upgrade-image-id sha256:<candidate-id> \
  --auto-migrate=false --upgrade-timeout 5m
```

Provide the normal run configuration too; it is needed after release. Probation
itself needs no OPA directory or JWT, creates no credential key, performs no
migrations/cleanup, starts no normal services, and permits no ordinary API
handlers. It validates read-only before opening the HTTP listener. The container
entrypoints skip extension copying for probation (including its environment
flag), offline commands, and an existing host authorization. The host should
still bypass the entrypoint for offline migration containers.

`validating` authorizes only the candidate in probation; `restoring` authorizes
only the previous image in probation, after the host has successfully executed
candidate rollback and baseline verification. Unsafe/incomplete phases, malformed
state, unknown contract versions, and mismatched image/attempt IDs reject startup.
The ordinary `run` path also obeys a present authorization file. Managed normal
startup supplies `--upgrade-image-id`; unmanaged standalone startup retains its
previous behavior when no authorization exists.

Probation requires matching validation and migration baselines for the attempt.
It checks exact migration histories expected by its own image, the complete
file inventory, representative inventory/group/VM/inspection/rightsizing/forecast
store reads, saved device-key credential decryption, and key/VDDK asset hashes.
Baseline record/annotation counts also reject lost optional data even when a
store still opens. These counts permit added records during reversible migrations;
transitions that reduce tracked records need an explicit validation contract.
No network or vCenter connection is required. Unreadable/uninitialized databases,
unsupported filenames, symlink databases, missing assets, and unexpected asset
changes fail validation. Credentials requiring an interactive master password
reject unattended activation; the implementation does not guess that password
or replace a key. Representative reads are not a full data-integrity check;
image-pair acceptance must verify populated application data as well.

`--upgrade-timeout` (default 5m) bounds each maintenance or probation validation
phase. It is independent of the host's startup/rollback deadlines. `/version`
only confirms HTTP startup; the host waits for the authenticated `ready` state
bound to the expected attempt/image before committing.

## Release, abort, and retries

After candidate validation, the host durably commits and publishes authorization
phase `committed`, then requests `action: release` for the same attempt. After
verified old-image recovery, use phase `rolled-back` and release old probation.
The agent rejects release while authorization still says `validating/restoring`.
It drains the HTTP server, closes probation, and starts normal services with
automatic startup migrations disabled, retaining the data lock throughout.
The host must confirm `normal` and inspect the running image before publishing
`applied` or `rolled-back`. Repeated release requests and lost acknowledgments
are safe; rebooted normal startup checks the persisted host decision. The host
must select normal-start arguments for terminal decisions on future service
starts, clearing probation attempt flags; a probation start after commit is
rejected.

To abort maintenance before migration, the host confirms no forward work ran,
publishes phase `aborted` for the old image, and requests `action: resume`.
If draining timed out before stores closed, resume reopens admission in place
without canceling the existing jobs. Once stores were closed, normal services
are reconstructed rather than reusing closed store handles.
A timed-out maintenance worker must finish stopping before resume is accepted.

A post-commit normal startup failure cannot authorize down SQL. The host keeps
the committed candidate selected and retries startup/release or reporting. An
unresolved recovery failure keeps the process stopped and retains both images,
data, archive, and attempt. The agent never writes host authorization, selects
images, runs its own image rollback, or decides that recovery is complete.
