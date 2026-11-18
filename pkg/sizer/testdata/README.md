Reference fixtures are generated from the original TypeScript sizer
(https://github.com/red-hat-storage/sizer) at commit `6c01a70`.

# Regenerating

```sh
git clone https://github.com/red-hat-storage/sizer.git
cd sizer/lib && npm install && npm run build
cd <this module>/sizer && node testdata/generate.js <path-to-sizer>/lib/dist
```

`generate.js` holds the corpus. A case is written in the Go request schema
(`../models.go`), translated to the library's descriptor schema, sized by
`ClusterSizer.size`, and translated back to the TypeScript response schema. The
source repository is not modified, and results keep native `float64` precision:
the service's `.toFixed(2)` calls are not replayed. The compatibility test
flattens the reference `advanced` zone groups into the port's `Nodes` list.

# Case contract

Each case lists the assertions the Go port must satisfy:

- `nodeCount` — number of created nodes;
- `totals` — physical CPU and memory sums;
- `consumption` — aggregate requests, reporting-only limits, and overcommit
  ratios;
- `nodes` — detailed node and service reporting;
- `error` — `Calculate` must fail, as the original did.

Cases with an empty list are recorded reference data, not assertions; each
carries a `note` explaining why the two implementations differ.

# Known divergences

- `service-zones-ignored`: the original spreads `Service.Zones` replicas across
  separate zones; this port has no zone concept and reports a flat node list.
  The fixture's request and response still carry the TypeScript `zones` fields,
  which the Go models ignore.
- `control-plane-denied`: the original places on a `controlPlane` machine set even
  when `allowWorkloadScheduling` is false, if the workload names that set; this
  port rejects the placement.
- `fits-by-memory`: the original creates the first machine set named in
  `usesMachines` without rechecking fit, so a 20 GB service lands on an 8 GB node;
  this port picks a set that fits.
- Cross-workload `RunsWith`/`Avoid` names resolve only inside one workload in the
  original; this port resolves them across workloads. `runs-with-across-workloads`
  therefore asserts totals only.

Two schema differences follow from the models: the original names the workload's
machine sets `usesMachines` and identifies control-plane sets by the names
`controlPlane`/`control-plane`, while this port uses `AllowedMachineSets` and an
explicit `IsControlPlane` flag. The corpus names control-plane sets `controlPlane`
so both implementations agree.

Requests omit zero limits, matching the former Go HTTP client's `omitempty`
behavior. The Go domain models cannot distinguish an omitted limit from zero.
