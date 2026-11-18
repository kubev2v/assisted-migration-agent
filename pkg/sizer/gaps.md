# Gaps

Open problems in the placement port, in order of importance. Each entry states the
current behavior, why it matters, and the direction of a fix. Items marked
*decision* need an answer before code changes; items marked *fixed* stay in place
for history and point at the regression coverage; items marked *by design* record
behavior that was reviewed and kept intentionally.

## 1. Placement failures do not distinguish policy from capacity — *fixed*

The two policy failures are now validation errors returned by `Calculate` before
bundling: `AllowedMachineSets` naming an unknown set, and a workload whose allowed
sets all refuse workloads (including an empty list when every set is a closed
control plane). `CreateAndPlace` only reports `NotEnoughResourcesError`, i.e.
capacity. Covered by `calculate_placement_test.go`; the unused `PlacementError`
type was removed.

## 2. RunWith join replicates the larger-count workload — *by design*

Joining bundles takes `Count = max(Counts)`. With `web` (Count 2) running with
`data` (Count 3), the result is three copies of `{api, db}` — three `api`
replicas although `web` asked for two (`create_placement_bundles_test.go` asserts
this).

This is intentional. `RunsWith` makes the services inseparable, so `web` and
`data` cannot be placed in different bundles without violating the rule. Placing
copies per service instead of per bundle would dissolve the bundle itself, which
is the unit of placement. The only consistent result is the larger count, with
the smaller workload's services replicated to match; `implementation.md`
documents this.

## 3. Avoid split does not re-resolve RunsWith

The Avoid pass runs after the RunWith merge and only moves the later service out.
If that service was the bridge of a `RunsWith` chain, the remaining members stay
in their old bundle with no link to the split service, silently violating
`RunsWith`. Fix: after splitting, re-run the link resolution (or iterate until
stable) on the affected bundles.

## 4. Not modeled from the original

These inputs exist in the TypeScript sizer (or its requests) and are ignored here;
they are out of scope for the bare-metal placement port but will surprise anyone
comparing results:

- `Service.Zones` — the original spreads copies across zones, a concept this
  bare-metal API does not model (the `service-zones-ignored` fixture records the
  difference).
- `MachineSet.OnlyFor` (dedicated machine sets) and `Workload.RequireControlPlane`
  — no equivalent field. `onlyFor` was a reverse `AllowedMachineSets` (a set
  listing its workloads), so the two filters could contradict each other. The
  port keeps one direction (`AllowedMachineSets`) plus the
  `AllowWorkloadScheduling` gate; dedication is expressed by omitting a set
  elsewhere. Only “any set except control plane” needs the worker sets listed.
- `MachineSet.Label`, `InstanceName`, `NumberOfDisks` — carried but unused by
  sizing.
- Disk constraints, VM batching, SMT adjustment, 80 % headroom, failover nodes —
  listed as out of scope in `implementation.md`.
- No node budget: nodes are created on demand, so a request never fails for
  "cluster full", only for "no template fits".

## 5. Original-sizer bugs recorded by the fixtures

`testdata/reference.json` is generated from the original implementation, so two of
its cases document bugs there rather than intended behavior:

- `control-plane-denied` — a new node is created on a `controlPlane` set even when
  `allowWorkloadScheduling` is false, as long as the workload names that set. The
  original only checks the permission when *reusing* an existing control-plane node
  (`lib/src/utils/node.ts`) and in the no-affinity validation branch
  (`lib/src/utils/workload.ts`). This port enforces it everywhere, which is the
  intended semantics; the fixture only records the original's output.
- `fits-by-memory` — the original creates the first machine set named in
  `usesMachines` without rechecking fit, placing a 20 GB service on an 8 GB node
  (`lib/src/utils/service.ts`). This port picks a template that fits.

Neither case asserts anything, so they cannot turn into false parity failures.

## 6. Reference fixtures are manual

`testdata/generate.js` needs a local checkout of the TypeScript sizer
(https://github.com/red-hat-storage/sizer, `lib` directory) built with
`npm run build`; it is not part of CI, and nothing detects when the original
drifts. Regeneration is documented in `testdata/README.md`. A pinned submodule
or a published package version would make this reproducible.

Naming, stale comments and the dead `PlacementBundle.Clone` have been repaired; see
the git log for that change.

## 7. Control-plane reservations diverge from the original

`MachineSet.ControlPlaneReservedResources` is subtracted from usable capacity in
`CreateNode`, `Place` and `CreateAndPlace`, but the pinned original never applies
it: `canNodeSupportRequirements` hardcodes control-plane overhead to zero. The
original also defaults the reservation to 2 CPU / 4 GB for sets named
`controlPlane`; this port uses it only when set.

The `control-plane-reserved` fixture cannot catch this: its 1 CPU / 2 GB service
fits either way and has no limits, so the asserted values are unchanged. Only the
`CreateNode` baseline (reservation included in `Requested`) is covered.

To decide: keep the reservation (document it under Reservations and add a
capacity-deciding fixture/test), or drop the subtraction to match the original.
