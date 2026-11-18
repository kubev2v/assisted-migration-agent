# Bare-metal sizing

This package sizes bare-metal clusters for a set of workloads. It places service
copies on the machine sets you supply and reports the resulting nodes and
resource consumption. It replaces the historical TypeScript placement algorithm;
the original reservation and reporting arithmetic remain.

## API

`NewSizer()` returns a `Sizer` with a single method:

```go
Calculate(ctx context.Context, machineSets []MachineSet, workloads []Workload, detailed bool) (*SizerResponse, error)
```

A `Sizer` keeps no shared state, so one value can be reused concurrently. `ctx` is
accepted for request scoping and the calculation runs synchronously; the call never
modifies the machine sets or workloads it receives.

```go
calculator := sizer.NewSizer()
response, err := calculator.Calculate(ctx,
    []sizer.MachineSet{{Name: "worker", CPU: 8, Memory: 32}},
    []sizer.Workload{{
        Name: "applications", Count: 1,
        AllowedMachineSets: []string{"worker"},
        Services: []sizer.Service{{Name: "application", RequiredCPU: 1, RequiredMemory: 2}},
    }},
    true,
)
```

### Inputs

- `MachineSets` — node templates; names must be unique.
- `Workloads` — nonempty; each workload contributes `Count` copies.
- `Detailed` — include per-node details in `Nodes`.

`MachineSet` is a node template:

- `Name` — unique template name, also used by `Workload.AllowedMachineSets`.
- `IsControlPlane` / `AllowWorkloadScheduling` — control-plane sets accept
  workloads only when `AllowWorkloadScheduling` is explicitly true.
- `CPU` / `Memory` — physical capacity of each node (cores and GB).
- `ControlPlaneReservedResources` — extra CPU and memory added to a control-plane
  node's baseline reservations.
- `InstanceName`, `NumberOfDisks`, `Label` — reporting metadata, not used for
  placement.

`Workload` groups services that share placement settings:

- `Name` — informational.
- `Count` — copies to place; zero or negative ignores the workload.
- `AllowedMachineSets` — eligible templates; an empty list allows any eligible
  template.
- `Services` — each service starts in its own bundle; `RunsWith` and `Avoid`
  join and split those bundles.

`Service` describes one placed component:

- `Name` — unique across all workloads; `RunsWith` and `Avoid` reference it by
  name.
- `RequiredCPU` / `RequiredMemory` — requests used for placement (cores and GB).
- `LimitCPU` / `LimitMemory` — configured size used only for limit and
  overcommit reporting; zero falls back to the request.
- `RunsWith` / `Avoid` — service names to co-locate with or keep apart.

### Response

`SizerResponse` reports the created cluster:

- `NodeCount` — number of created nodes.
- `Nodes` — per-node details when `Detailed` is true; each `Node` reports its
  machine set, control-plane flag, `Resource` CPU and memory, and placed `Service`
  values.
- `TotalCPU` / `TotalMemory` — physical capacity of the created nodes.
- `ResourceConsumption` — placed service requests, plus optional `Limits` and
  `OverCommitRatio` when any placed service sets a limit.

### Errors

- Invalid input returns a plain `error`, for example
  `duplicate service name "app"`, an `AllowedMachineSets` entry naming an unknown
  set, or a workload with no allowed machine set that permits workloads.
- `*NotEnoughResourcesError` when a permitted machine set exists but none fits.

## Usage

- Machine set names must be unique, the workload list must be nonempty, and
  service names must be unique across all workloads.
- CPU is measured in cores and memory in GB.
- Each call owns its state and leaves its inputs unchanged.
- One `Sizer` can be reused concurrently.

## Implementation

Bundle construction, the placement algorithm, reporting arithmetic, reservations,
out-of-scope items, known limitations, and verification steps are described in
[implementation.md](implementation.md).
