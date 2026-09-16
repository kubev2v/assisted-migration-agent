# Advanced Scripting

- [Why](#why)
- [What](#what)
- [API Overview](#api-overview)
- **Types**
  - [CollectionList](#collectionlist) — `latest()`, `get()`, `filter()`, `map()`, `reduce()`
  - [Collection](#collection) — `virtual_machines()`, `inventory()`, `groups()`, `summary()`
  - [VirtualMachineList](#virtualmachinelist) — `filter()`, `map()`, `reduce()`, `sort()`, `first()`, `group_by()`, `get()`, `totals()`
  - [VirtualMachine](#virtualmachine)
    - [Disk](#disk)
    - [InspectionConcern](#inspectionconcern)
    - [Application](#application)
    - [ApplicationList](#applicationlist)
    - [NIC](#nic)
    - [Issue](#issue)
    - [GuestNetwork](#guestnetwork)
  - [Group](#group)
  - [Inventory](#inventory)
  - [ForecastStats](#forecaststats)
- **Functions**
  - [collections()](#collections)
  - [sprintf()](#sprintf)
  - [json()](#json)
  - [csv()](#csv)
  - [forecast_stats()](#forecast_stats)
- **Modules**
  - [math](#math-module) — `ceil`, `floor`, `round`, `abs`, `min`, `max`, `pow`, `sqrt`, `sum`, `mean`, `median`, `stddev`, `percentile`
  - [template](#template-module) — `render`
  - [diff](#diff-module) — `vms`, `vm`, `sets`, `where`, `timeline`, `fields`, `summary`, `rank_changes`, `distribution`, `resource_delta`
  - [debug](#debug-module) — `dump`, `describe`, `peek`
- [Examples](#example-scripts)
  - [Templated Reports](#templated-report-examples)
  - [Comparison Scripts](#comparison-example-scripts)
- [Implementation Notes](#implementation-notes)

---

## Why

The REST API serves individual resources well — one collection's VMs, one group's membership, one pairwise comparison — and that's the right interface for UI-driven workflows and integrations. Migration planning, however, asks cross-cutting questions that cut across those boundaries: "which VMs changed across five collections?", "how do I batch 400 VMs into waves that fit our capacity?", "which apps span multiple clusters and need co-migration?" These require joining data the API returns separately, looping over collections, and producing output in formats the API was never designed to offer. That's where scripting comes in — not to replace the API, but to work on top of it.

Migration planning demands complex analysis — wave sizing under resource constraints, multi-collection drift detection, topology change tracking, application co-migration risk — that rigid APIs cannot express. These problems are inherently programmable: they need loops, conditionals, aggregation, and custom output formats that evolve faster than any API surface can. Advanced scripting provides the same flexibility as a Jupyter notebook — iterative exploration of collection data with the ability to aggregate, filter, join, and render results — built into the agent as a sandboxed, deterministic runtime.

Scripts also bridge the gap between assessment and execution. Templates can generate Forklift migration plans and Ansible playbooks directly from collection data — populating VM lists, resource constraints, and network mappings into ready-to-apply manifests. Instead of exporting CSVs and manually assembling migration artifacts, a script produces the YAML that Forklift or Ansible consumes in a single run. Custom reporting beyond what the agent ships (executive summaries, compliance audits, per-cluster breakdowns) follows the same pattern.

## What

Scripts run in Starlark — a deterministic, sandboxed Python dialect. The runtime injects functions and modules that delegate to existing Go stores and services (`VMStore`, `GroupStore`, `InventoryStore`, `ForecasterService`). Scripts have read-only access to collection data through a scoped object graph: `collections()` returns collection objects, and each collection exposes its VMs, inventory, and groups as methods. Scripts can filter, aggregate, compare, and export results as CSV or JSON. No mutations, no network access. The `template` module handles Go template rendering; the `diff` module handles structured comparison across collections.

```python
col = collections()[0]

# scoped access — VMs, groups, and inventory belong to a collection
prod_vms = col.virtual_machines(expression="cluster = 'prod'")
groups = col.groups()

# iterate with full details available directly
for vm in prod_vms:
    if vm.issue_count > 0:
        for c in vm.inspection_concerns:
            print("%-30s  %s: %s" % (vm.name, c.category, c.label))

# group-scoped VM access
for group in groups:
    group_vms = group.virtual_machines()
    total_disk_gb = sum([vm.disk_size for vm in group_vms]) / 1024.0
    print("%-20s  %d VMs  %.1f GB" % (group.name, len(group_vms), total_disk_gb))

# render and save
rows = [[vm.name, vm.cluster, vm.cpu_count, vm.memory_mb] for vm in prod_vms]
md = report.table(["Name", "Cluster", "vCPU", "Memory MB"], rows)
report.save("prod_vms.md", md)

# templated output — Forklift manifest, Ansible playbook, HTML report, anything
yaml = report.render("""apiVersion: forklift.konveyor.io/v1beta1
kind: Plan
metadata:
  name: {{.plan_name}}
spec:
  vms:
{{range .vms}}
    - id: {{.id}}
      name: {{.name}}
{{end}}""", {
    "plan_name": "wave-1",
    "vms": [{"id": vm.id, "name": vm.name} for vm in prod_vms],
})
report.save("forklift-plan.yaml", yaml)
```

---

## API Overview

Scripts interact with the agent through functions, modules, and typed objects across data access, reporting, comparison, and math.

| Builtin | Category | Purpose |
|---|---|---|
| `collections()` | Data | Returns a `CollectionList` with `.latest()`, `.get()` |
| `Collection.virtual_machines()` | Data | All VMs in a collection as a `VirtualMachineList`; accepts optional `expression` filter; auto-paginates |
| `Collection.inventory()` | Data | Stored inventory snapshot for a collection (updated_at timestamp, raw JSON data) |
| `Collection.groups()` | Data | All groups defined in a collection, each with `.virtual_machines()` for scoped VM access |
| `Collection.summary()` | Data | Quick stats dict: total, migratable, templates, powered_off, excluded, issues |
| `CollectionList.latest()` | Data | Most recent collection by timestamp |
| `CollectionList.get(id_or_fn)` | Data | By ID (string) → Collection or None; by predicate (callable) → filtered CollectionList |
| `CollectionList.filter(fn)` | Data | Returns a filtered CollectionList |
| `CollectionList.map(fn)` | Data | Returns a list of computed values |
| `CollectionList.reduce(initial, fn)` | Data | Accumulate a value over collections |
| `VirtualMachineList.get(id)` | Data | Lookup a VM by ID within the (possibly filtered) set |
| `VirtualMachineList.filter(fn)` | Data | Returns a new VirtualMachineList with VMs where `fn(vm)` is truthy |
| `VirtualMachineList.map(fn)` | Data | Returns a list of `fn(vm)` results |
| `VirtualMachineList.reduce(initial, fn)` | Data | Accumulate a value over VMs |
| `VirtualMachineList.sort(key=fn, reverse=False)` | Data | Returns a sorted VirtualMachineList by key function |
| `VirtualMachineList.first(fn)` | Data | First VM where `fn(vm)` is truthy, or None |
| `VirtualMachineList.group_by(fn)` | Data | Dict mapping `fn(vm)` result → VirtualMachineList |
| `VirtualMachineList.group_by_cluster()` | Data | Shortcut: group by `cluster` field |
| `VirtualMachineList.group_by_datacenter()` | Data | Shortcut: group by `datacenter` field |
| `VirtualMachineList.group_by_power_state()` | Data | Shortcut: group by `power_state` field |
| `VirtualMachineList.count_by(fn)` | Data | Dict mapping `fn(vm)` result → count (int) |
| `VirtualMachineList.sum_by(key_fn, val_fn)` | Data | Dict mapping `key_fn(vm)` → sum of `val_fn(vm)` (float) |
| `VirtualMachineList.totals(fields)` | Data | Sum specified numeric fields across all VMs; returns dict of field → sum (int for int fields, float for float fields); skips None values |
| `VirtualMachineList.merge(*others)` | Data | Combine multiple VirtualMachineLists into one |
| `vms_a + vms_b` | Data | Concatenate two VirtualMachineLists (operator) |
| `VirtualMachine.os_name` | Data | Guest OS name reported by VMware Tools |
| `VirtualMachine.applications` | Data | Guest applications as an `ApplicationList` |
| `VirtualMachine.nics` | Data | Network interfaces with MAC, network, IPv4/IPv6 |
| `VirtualMachine.issues` | Data | Migration issues with label, description, category |
| `VirtualMachine.networks` | Data | Guest-level network configuration |
| `ApplicationList.filter(fn)` | Data | Returns a filtered ApplicationList |
| `ApplicationList.map(fn)` | Data | Returns a list of computed values |
| `ApplicationList.any(fn)` | Data | Returns True if any application matches the predicate |
| `Group.virtual_machines()` | Data | VMs matched by this group's filter, returned as a `VirtualMachineList` |
| `forecast_stats()` | Data | Throughput statistics (mean, median, CI95) for all benchmark pairs |
| `sprintf(format, ...)` | Format | Go `fmt.Sprintf` — full format specifiers (`%.1f`, `%-30s`, `%5d`, etc.) |
| `json(data, indent=2)` | Export | JSON serialization of dicts, lists, primitives |
| `csv(headers, rows)` | Export | RFC 4180 CSV with formula-injection sanitization |
| `math.ceil(x)` | Math | Round up to int |
| `math.floor(x)` | Math | Round down to int |
| `math.round(x)` | Math | Round to nearest int |
| `math.abs(x)` | Math | Absolute value (preserves int/float type) |
| `math.min(a, b)` | Math | Smaller of two values |
| `math.max(a, b)` | Math | Larger of two values |
| `math.pow(base, exp)` | Math | Exponentiation |
| `math.sqrt(x)` | Math | Square root |
| `math.sum(list)` | Math | Sum a list of numbers |
| `math.mean(list)` | Math | Arithmetic mean |
| `math.median(list)` | Math | Median value |
| `math.stddev(list)` | Math | Population standard deviation |
| `math.percentile(list, p)` | Math | P-th percentile (0-100) with interpolation |
| `template.render(tmpl, data)` | Template | Go template rendering with substitution, loops, conditionals |
| `diff.vms()` | Diff | Bulk VM comparison — added/removed/changed/unchanged across two VM lists |
| `diff.vm()` | Diff | Field-level diff of a single VM pair with native-typed old/new values |
| `diff.sets()` | Diff | Set-difference on string lists (clusters, labels, datastores) |
| `diff.where(vms_a, vms_b, fn)` | Diff | VMs where `fn(vm)` result differs between two collections |
| `diff.timeline()` | Diff | Evaluate a function across N collections for time-series analysis |
| `diff.fields()` | Diff | Compare specific fields between two structs |
| `diff.summary()` | Diff | Aggregate change statistics without per-VM detail |
| `diff.rank_changes()` | Diff | VMs with largest delta for a specific field |
| `diff.distribution(dict_a, dict_b, top_n)` | Diff | Compare two count dicts (e.g. from `count_by`); ranks by combined total, folds tail into "Other"; returns `{labels, values_a, values_b}` |
| `diff.resource_delta(vms_a, vms_b)` | Diff | Compute resource deltas (B − A); returns `{cpu, memory_mb, disk_mb}` as ints |
| `debug.dump()` | Debug | Pretty-print any value with all fields |
| `debug.describe()` | Debug | Show type and available attributes |
| `debug.peek()` | Debug | Tabular preview of VMs with selected fields |

Data access is scoped through the `Collection` object graph:

```
collections() → CollectionList
  ├─ .latest() → Collection
  ├─ .get(id) → Collection | None
  ├─ .get(fn) → CollectionList
  ├─ .filter(fn) → CollectionList
  ├─ .map(fn) → List
  ├─ .reduce(initial, fn) → any
  └─ iteration / indexing yields Collection
       ├─ .virtual_machines(expression?) → VirtualMachineList
       │    ├─ .get(id) → VirtualMachine
       │    ├─ .filter(fn) → VirtualMachineList
       │    ├─ .map(fn) → List
       │    ├─ .reduce(initial, fn) → any
       │    ├─ .sort(key=fn) → VirtualMachineList
       │    ├─ .first(fn) → VirtualMachine | None
       │    ├─ .group_by(fn) → Dict[key → VirtualMachineList]
       │    ├─ .group_by_cluster() → Dict[str → VirtualMachineList]
       │    ├─ .group_by_datacenter() → Dict[str → VirtualMachineList]
       │    ├─ .group_by_power_state() → Dict[str → VirtualMachineList]
       │    ├─ .count_by(fn) → Dict[key → int]
       │    ├─ .sum_by(key_fn, val_fn) → Dict[key → float]
       │    ├─ .totals(fields) → Dict[field → int|float]
       │    ├─ .merge(*others) → VirtualMachineList
       │    ├─ + operator → VirtualMachineList
       │    └─ iteration yields VirtualMachine
       │         ├─ .os_name → string
       │         ├─ .disks → List[Disk]
       │         ├─ .nics → List[NIC]
       │         ├─ .issues → List[Issue]
       │         ├─ .inspection_concerns → List[InspectionConcern]
       │         ├─ .applications → ApplicationList
       │         │    ├─ .filter(fn) → ApplicationList
       │         │    ├─ .map(fn) → List
       │         │    └─ .any(fn) → bool
       │         └─ .networks → List[GuestNetwork]
       ├─ .inventory() → Inventory
       ├─ .summary() → Dict
       └─ .groups() → List[Group]
              └─ Group
                   └─ .virtual_machines() → VirtualMachineList
```

---

## Functions

### `collections()`

```
() → CollectionList
```

Returns all collections as a `CollectionList`. Supports iteration, indexing, `len()`, and the methods below.

### `sprintf(format, ...)`

```
(format: string, *args) → string
```

Go `fmt.Sprintf` with full format specifier support. Use for formatted strings with precision, width, and alignment.

```python
s = sprintf("%-30s %5d %10.1f", name, count, value)
print(sprintf("CPU: %.2f cores, Memory: %.1f GB", 63.77, 244.12))
```

### `json(data, indent=2)`

```
(data: any, indent: int) → string
```

Serializes dicts, lists, and primitives to JSON.

```python
output = json({"total": 400, "clusters": ["prod", "dev"]})
```

### `csv(headers, rows)`

```
(headers: List[string], rows: List[List]) → string
```

Generates RFC 4180 CSV with proper quoting/escaping. Applies formula-injection sanitization (prefixes `=`, `+`, `@`, `\t`, `\r`, `-` with `'`).

```python
output = csv(["Name", "CPU", "Memory"], [["vm-1", 4, 8192], ["vm-2", 8, 16384]])
```

### `forecast_stats()`

```
() → Dict[str, ForecastStats]
```

Returns throughput statistics for all benchmark pairs. Maps to `ForecasterService.GetStats` iterated over all known pair names from `ForecasterService.ListRuns`.

---

## CollectionList

Returned by `collections()`. Supports iteration, indexing, `len()`, and the functional methods below.

```
.latest()                  → Collection | None
.get(id)                   → Collection | None
.get(fn)                   → CollectionList
.filter(fn)                → CollectionList
.map(fn)                   → List
.reduce(initial, fn)       → any
```

### `CollectionList.latest()`

Returns the most recent collection by timestamp. Returns `None` if the list is empty.

### `CollectionList.get(id_or_predicate)`

With a string argument, looks up a collection by ID. With a callable, filters collections by predicate.

### `CollectionList.filter(fn)`

Returns a new CollectionList containing only collections where `fn(col)` is truthy.

### `CollectionList.map(fn)`

Returns a plain list of computed values.

### `CollectionList.reduce(initial, fn)`

Accumulates a value by applying `fn(accumulator, collection)` to each collection.

```python
cols = collections()

# latest collection
col = cols.latest()

# by ID
col = cols.get("col-123")

# filter by timestamp
recent = cols.filter(lambda c: c.timestamp > "2026-08-01")

# extract all timestamps
timestamps = cols.map(lambda c: c.timestamp)

# count total VMs across all collections
total = cols.reduce(0, lambda acc, c: acc + len(c.virtual_machines()))
```

---

## Collection

```
.id           string      # unique identifier
.timestamp    string      # ISO 8601
.virtual_machines(expression=None) → VirtualMachineList
.inventory()  → Inventory
.groups()     → List[Group]
.summary()    → Dict
```

### `Collection.summary()`

```
() → Dict
```

Returns a dict with quick stats about the collection's VMs:

```python
s = col.summary()
# {
#   "total": 400,
#   "migratable": 101,
#   "templates": 50,
#   "powered_off": 200,
#   "excluded": 30,
#   "issues": 19,
# }
```

### `Collection.virtual_machines()`

```
(expression=None) → VirtualMachineList
```

Returns VMs from this collection. Auto-paginates internally — callers always receive the full list.

- No args: all VMs in the collection.
- `expression="cluster = 'prod'"`: passes filter DSL to `VirtualMachineListParams.Expression`.

### `Collection.inventory()`

```
() → Inventory
```

Returns the stored inventory for this collection. Maps to `InventoryStore.Get`.

### `Collection.groups()`

```
() → List[Group]
```

Returns all groups defined in this collection. Maps to `GroupStore.List`.

---

## VirtualMachineList

Returned by `Collection.virtual_machines()` and `Group.virtual_machines()`. Supports `len()`, iteration, indexing, and comprehensions.

```
.get(id)                   → VirtualMachine | None
.filter(fn)                → VirtualMachineList
.map(fn)                   → List
.reduce(initial, fn)       → any
.sort(key=fn, reverse=False) → VirtualMachineList
.first(fn)                 → VirtualMachine | None
.group_by(fn)              → Dict[key → VirtualMachineList]
.group_by_cluster()        → Dict[str → VirtualMachineList]
.group_by_datacenter()     → Dict[str → VirtualMachineList]
.group_by_power_state()    → Dict[str → VirtualMachineList]
.count_by(fn)              → Dict[key → int]
.sum_by(key_fn, val_fn)    → Dict[key → float]
.merge(*others)            → VirtualMachineList
+ operator                 → VirtualMachineList
```

### `VirtualMachineList.filter(fn)`

Returns a new VirtualMachineList containing only VMs where `fn(vm)` is truthy. Chainable — the result supports all the same methods.

### `VirtualMachineList.map(fn)`

Returns a plain list of computed values.

### `VirtualMachineList.reduce(initial, fn)`

Accumulates a value by applying `fn(accumulator, vm)` to each VM.

### `VirtualMachineList.sort(key=fn, reverse=False)`

Returns a new sorted VirtualMachineList. Does not mutate the original.

### `VirtualMachineList.first(fn)`

Returns the first VM where `fn(vm)` is truthy, or `None` if no match.

### `VirtualMachineList.group_by(fn)`

Groups VMs by the return value of `fn(vm)`. Returns a dict mapping keys to VirtualMachineLists.

### `VirtualMachineList.group_by_cluster()` / `group_by_datacenter()` / `group_by_power_state()`

Shortcuts for grouping by common fields. Equivalent to `group_by(lambda vm: vm.cluster)`, etc.

```python
by_cluster = vms.group_by_cluster()
by_dc = vms.group_by_datacenter()
by_power = vms.group_by_power_state()
```

### `VirtualMachineList.count_by(fn)`

Returns a dict mapping `fn(vm)` result → count of VMs in that group (int).

```python
os_counts = vms.count_by(lambda vm: vm.os_name)
dc_counts = vms.count_by(lambda vm: vm.datacenter)
```

### `VirtualMachineList.sum_by(key_fn, val_fn)`

Returns a dict mapping `key_fn(vm)` → sum of `val_fn(vm)` for that group (float).

```python
cpu_by_cluster = vms.sum_by(lambda vm: vm.cluster, lambda vm: vm.cpu_count)
mem_by_dc = vms.sum_by(lambda vm: vm.datacenter, lambda vm: vm.memory_mb / 1024.0)
```

### `VirtualMachineList.merge(*others)`

Combines multiple VirtualMachineLists into one. The `+` operator also works for two lists.

```python
# merge multiple
all_vms = prod_vms.merge(dev_vms, staging_vms)

# + operator for two
combined = prod_vms + dev_vms
```

### Examples

```python
col = collections().latest()
vms = col.virtual_machines()

# lookup by ID
vm = vms.get("vm-123")

# expression filter at query level
prod_vms = col.virtual_machines(expression="cluster = 'prod'")

# filter — chainable
ready = vms.filter(lambda vm: vm.is_migratable and not vm.migration_excluded)
prod_ready = ready.filter(lambda vm: vm.cluster == "prod")

# map — extract values
names = vms.map(lambda vm: vm.name)
disk_sizes = vms.map(lambda vm: vm.disk_size)

# reduce — accumulate
total_disk = vms.reduce(0, lambda acc, vm: acc + vm.disk_size)
total_mem = ready.reduce(0, lambda acc, vm: acc + vm.memory_mb)

# sort
biggest_first = vms.sort(key=lambda vm: vm.disk_size, reverse=True)
by_name = vms.sort(key=lambda vm: vm.name)

# first — find one
big = vms.first(lambda vm: vm.memory_mb > 32768)
if big != None:
    print(big.name)

# group_by — partition into buckets
by_cluster = vms.group_by(lambda vm: vm.cluster)
for name, cluster_vms in by_cluster.items():
    total = cluster_vms.reduce(0, lambda acc, vm: acc + vm.memory_mb)
    print(sprintf("%s: %d VMs, %d MB", name, len(cluster_vms), total))

# chaining — filter + sort + map in one expression
top_10_names = vms.filter(lambda vm: vm.is_migratable).sort(
    key=lambda vm: vm.disk_size, reverse=True).map(lambda vm: vm.name)

# CSV export from functional chain
output = csv(
    ["Name", "Cluster", "Memory MB"],
    vms.filter(lambda vm: vm.power_state == "poweredOn").map(
        lambda vm: [vm.name, vm.cluster, vm.memory_mb]),
)
```

---

## VirtualMachine

Iteration over a VirtualMachineList yields VMs with all fields populated, including disks, applications, and inspection concerns. All detail fields are direct attributes — no method calls needed.

```
.id                        string
.name                      string
.power_state               string    # "poweredOn" | "poweredOff" | "suspended"
.cluster                   string
.datacenter                string
.cpu_count                 int
.memory_mb                 int
.disk_size                 int       # total disk in MB
.is_migratable             bool
.is_template               bool
.has_rdm_disk              bool
.migration_excluded        bool
.issue_count               int
.inspection_status         string    # "not_started"|"pending"|"running"|"completed"|"error"|"canceled"
.inspection_concern_count  int
.labels                    List[string]
.groups                    List[string]
.utilization_cpu_p95       float | None
.utilization_mem_p95       float | None
.utilization_cpu_max       float | None
.utilization_mem_max       float | None
.utilization_disk          float | None
.utilization_confidence    float | None
.os_name                   string    # guest OS reported by VMware Tools
.disks                     List[Disk]
.inspection_concerns       List[InspectionConcern]
.applications              ApplicationList
.nics                      List[NIC]
.issues                    List[Issue]
.networks                  List[GuestNetwork]
```

```python
vm = col.virtual_machines().get("vm-123")
for disk in vm.disks:
    print("%s  %d GB  rdm=%s" % (disk.datastore, disk.capacity / 1073741824, disk.rdm))
```

### Disk

Available via `vm.disks`.

```
.file        string    # VMDK path
.capacity    int       # bytes
.shared      bool
.rdm         bool
.bus         string
.mode        string
.datastore   string    # derived — parsed from the "[datastoreName] path.vmdk" file string
```

### InspectionConcern

Available via `vm.inspection_concerns`.

```
.category    string
.label       string
.message     string
```

### Application

Available via `vm.applications`. Each application represents a guest process discovered by VMware Tools.

```
.name         string    # application/package name
.process      string    # process path or command
```

### ApplicationList

Returned by `vm.applications`. Supports `len()`, iteration, indexing, and the methods below.

```
.filter(fn)    → ApplicationList    # applications where fn(app) is truthy
.map(fn)       → List               # list of fn(app) results
.any(fn)       → bool               # True if any application matches fn(app)
```

The `any` method is particularly useful for appliance detection — it short-circuits on the first match:

```python
has_netscaler = vm.applications.any(lambda a: "netscaler" in a.name.lower())
httpd_apps = vm.applications.filter(lambda a: "httpd" in a.name)
all_names = vm.applications.map(lambda a: a.name)
```

### NIC

Available via `vm.nics`.

```
.mac         string    # MAC address
.network     string    # vSphere network name
.ipv4        string    # IPv4 address (empty if unavailable)
.ipv6        string    # IPv6 address (empty if unavailable)
.index       int       # NIC index within the VM
```

### Issue

Available via `vm.issues`.

```
.label       string    # short label (e.g. "VMware Tools not installed")
.description string    # detailed description with recommendations
.category    string    # "Critical" | "Warning" | "Information" | "Advisory" | "Error" | "Other"
```

### GuestNetwork

Available via `vm.networks`.

```
.device         string    # network device name inside the guest (e.g. "eth0")
.mac            string    # MAC address as seen by the guest
.ip             string    # IP address
.prefix_length  int       # CIDR prefix length
.network        string    # network name from the guest
```

---

## Group

```
.id           string    # UUID
.name         string
.description  string
.filter       string    # DSL expression
.virtual_machines() → VirtualMachineList
```

Source: `models.Group`

### `Group.virtual_machines()`

```
() → VirtualMachineList
```

Returns VMs matched by this group's filter. Same `VirtualMachineList` type — supports `.get(id)`, iteration, comprehensions.

```python
for group in col.groups():
    vms = group.virtual_machines()
    print("%-30s %d VMs" % (group.name, len(vms)))
```

---

## Inventory

```
.updated_at  string        # ISO 8601 timestamp
.data        dict          # the raw inventory JSON, decoded
```

Returned by `collection.inventory()`.

---

## ForecastStats

```
.pair_name      string
.sample_count   int
.mean_mbps      float
.median_mbps    float
.min_mbps       float
.max_mbps       float
.std_dev_mbps   float
.ci95_lower     float
.ci95_upper     float
```

Returned by `forecast_stats()`.

---

## Math Module

The `math` module provides standard math functions missing from Starlark.

### `math.ceil(x)`

```
(x: number) → int
```

Round up to the nearest integer. Accepts int or float.

### `math.floor(x)`

```
(x: number) → int
```

Round down to the nearest integer.

### `math.round(x)`

```
(x: number) → int
```

Round to the nearest integer (half rounds away from zero).

### `math.abs(x)`

```
(x: number) → number
```

Absolute value. Preserves type: int input → int output, float input → float output.

### `math.min(a, b)` / `math.max(a, b)`

```
(a: number, b: number) → number
```

Returns the smaller / larger of two values. When both args are int, returns int; otherwise float.

### `math.pow(base, exp)`

```
(base: number, exp: number) → float
```

Exponentiation. Always returns float.

```python
overhead = math.ceil(vm.utilization_cpu_p95 * 1.2)
capped = math.min(vm.memory_mb, 65536)
factor = math.pow(10, 3)  # 1000.0
```

### `math.sqrt(x)`

```
(x: number) → float
```

Square root. Cleaner than `math.pow(x, 0.5)`.

### `math.sum(list)`

```
(list: List[number]) → float
```

Sum a list of numbers. Returns 0 for an empty list.

### `math.mean(list)`

```
(list: List[number]) → float
```

Arithmetic mean. List must not be empty.

### `math.median(list)`

```
(list: List[number]) → float
```

Median value. For even-length lists, returns the average of the two middle values.

### `math.stddev(list)`

```
(list: List[number]) → float
```

Population standard deviation.

### `math.percentile(list, p)`

```
(list: List[number], p: number) → float
```

P-th percentile (0-100) with linear interpolation between values.

```python
values = vms.map(lambda vm: vm.utilization_cpu_p95)

math.sum(values)            # total
math.mean(values)           # average
math.median(values)         # middle value
math.stddev(values)         # spread
math.percentile(values, 95) # 95th percentile
math.percentile(values, 50) # same as median
```

---

## Template Module

The `template` module provides Go template rendering for generating structured output — YAML manifests, HTML reports, or any text with variable substitution.

### `template.render(tmpl, data)`

```
(tmpl: string, data: dict) → string
```

Renders a Go template string with the given data dict.

Template syntax:

```
{{.key}}                       — value substitution
{{range .items}}...{{end}}     — loop over list
{{if .items}}...{{end}}        — render if truthy
{{if not .items}}...{{end}}    — render if falsy
{{if .items}}...{{else}}...{{end}} — conditional with fallback
```

```python
yaml = template.render("""apiVersion: forklift.konveyor.io/v1beta1
kind: Plan
metadata:
  name: {{.plan_name}}
spec:
  vms:
{{range .vms}}
    - id: {{.id}}
      name: {{.name}}
{{end}}""", {
    "plan_name": "wave-1",
    "vms": [{"id": vm.id, "name": vm.name} for vm in prod_vms],
})
```

---

## Gaps the API Doesn't Cover

These are cross-cutting, analytical, and automation gaps that Starlark scripts fill.

| # | Gap | API Limitation | Starlark Fix |
|---|-----|---------------|-------------|
| 1 | Multi-collection trends | Compare only works pairwise | Loop over all collections, compute time-series |
| 2 | Wave planning | No resource-budgeted batching | Bin-pack VMs into waves with CPU/mem/disk caps |
| 3 | Inspection coverage | Status is per-VM only, no aggregation | Roll up status + top concern categories |
| 4 | Group overlap detection | Groups queried independently | Cross-join groups, find multi-membership + orphans |
| 5 | Migration time estimation | Forecast stats and VM disk data are separate | Join throughput stats with per-group disk totals |
| 6 | Datastore hotspots | Datastores listed without VM allocation context | Aggregate disk allocations per datastore |
| 7 | App-cluster affinity | Apps listed without cluster context | Map applications to hosting clusters, flag splits |

---

## Example Scripts

### 1. List all collections and their status

```python
# health_check.star

cols = collections()
print("Collections: %d" % len(cols))

for c in cols:
    print("  %s  %s" % (c.id, c.timestamp))
```

### 2. Find oversized VMs

```python
# oversized_vms.star

col = collections()[0]
vms = col.virtual_machines()

for vm in vms:
    if vm.cpu_count > 8 and vm.memory_mb > 32768:
        print("%-40s  %2d vCPU  %6d MB  cluster=%s  power=%s" % (
            vm.name, vm.cpu_count, vm.memory_mb, vm.cluster, vm.power_state,
        ))
```

### 3. Inventory summary report

```python
# inventory_report.star

for col in collections():
    inv = col.inventory()
    vms = col.virtual_machines()

    powered_on  = len([vm for vm in vms if vm.power_state == "poweredOn"])
    powered_off = len([vm for vm in vms if vm.power_state == "poweredOff"])
    total_disk  = sum([vm.disk_size for vm in vms])

    print("=== %s ===" % col.id)
    print("  VMs: %d on, %d off" % (powered_on, powered_off))
    print("  Total disk: %.1f GB" % (total_disk / 1024.0))
    print("  Last updated: %s" % inv.updated_at)
```

### 4. Migration readiness assessment

```python
# migration_readiness.star

col = collections()[0]
vms = col.virtual_machines()

ready    = []
excluded = []
has_rdm  = []
blocked  = []

for vm in vms:
    if vm.migration_excluded:
        excluded.append(vm)
    elif vm.has_rdm_disk:
        has_rdm.append(vm)
    elif vm.issue_count > 0:
        blocked.append(vm)
    else:
        ready.append(vm)

print("Ready:    %d" % len(ready))
print("Excluded: %d" % len(excluded))
print("RDM disk: %d" % len(has_rdm))
print("Blocked:  %d (have concerns)" % len(blocked))

for vm in blocked:
    print("  %s — %d issues" % (vm.name, vm.issue_count))
```

### 5. Multi-collection trend analysis

```python
# trends.star

cols = collections()
if len(cols) < 2:
    print("Need at least 2 collections for trend analysis")
else:
    print("%-20s %6s %10s %14s %8s" % ("COLLECTION", "TOTAL", "MIGRATABLE", "NON-MIGRATABLE", "ISSUES"))
    prev_total = None
    for col in cols:
        vms = col.virtual_machines()
        total = len(vms)
        migratable = len([vm for vm in vms if vm.is_migratable])
        non_migratable = total - migratable
        total_issues = sum([vm.issue_count for vm in vms])
        delta = ""
        if prev_total != None:
            d = total - prev_total
            delta = " (%s%d)" % ("+" if d >= 0 else "", d)
        print("%-20s %6d %10d %14d %8d%s" % (
            col.id, total, migratable, non_migratable, total_issues, delta,
        ))
        prev_total = total
```

### 6. Migration wave planning

```python
# wave_planner.star

MAX_VCPU_PER_WAVE = 128
MAX_MEM_GB_PER_WAVE = 512
MAX_DISK_TB_PER_WAVE = 10

col = collections()[0]
vms = col.virtual_machines()

candidates = [vm for vm in vms
              if vm.is_migratable
              and not vm.migration_excluded
              and vm.power_state == "poweredOn"]

candidates = sorted(candidates, key=lambda vm: vm.disk_size, reverse=True)

waves = []
for vm in candidates:
    placed = False
    vm_mem_gb = vm.memory_mb / 1024.0
    vm_disk_tb = vm.disk_size / (1024.0 * 1024.0)
    for wave in waves:
        if (wave["vcpu"] + vm.cpu_count <= MAX_VCPU_PER_WAVE and
            wave["mem_gb"] + vm_mem_gb <= MAX_MEM_GB_PER_WAVE and
            wave["disk_tb"] + vm_disk_tb <= MAX_DISK_TB_PER_WAVE):
            wave["vms"].append(vm.name)
            wave["vcpu"] += vm.cpu_count
            wave["mem_gb"] += vm_mem_gb
            wave["disk_tb"] += vm_disk_tb
            placed = True
            break
    if not placed:
        waves.append({
            "vms": [vm.name],
            "vcpu": vm.cpu_count,
            "mem_gb": vm_mem_gb,
            "disk_tb": vm_disk_tb,
        })

for i, w in enumerate(waves):
    print("Wave %d: %d VMs, %d vCPU, %.1f GB mem, %.2f TB disk" % (
        i + 1, len(w["vms"]), w["vcpu"], w["mem_gb"], w["disk_tb"],
    ))
    for name in w["vms"][:5]:
        print("  - %s" % name)
    if len(w["vms"]) > 5:
        print("  ... +%d more" % (len(w["vms"]) - 5))
```

### 7. Inspection coverage report

```python
# inspection_coverage.star

col = collections()[0]
vms = col.virtual_machines()

by_status = {}
concern_categories = {}
vms_with_concerns = []

for vm in vms:
    status = vm.inspection_status
    by_status[status] = by_status.get(status, 0) + 1

    if vm.inspection_concern_count > 0:
        vms_with_concerns.append(vm)

total = len(vms)
inspected = by_status.get("completed", 0)
print("Inspection Coverage: %d/%d (%.1f%%)" % (inspected, total, inspected * 100.0 / total if total else 0))
print("")
print("Status breakdown:")
for status, count in sorted(by_status.items()):
    print("  %-12s %4d" % (status, count))

print("")
print("Top concerns:")
for vm in sorted(vms_with_concerns, key=lambda vm: vm.inspection_concern_count, reverse=True)[:10]:
    for concern in vm.inspection_concerns:
        cat = concern.category
        concern_categories[cat] = concern_categories.get(cat, 0) + 1
    print("  %-40s %d concerns" % (vm.name, vm.inspection_concern_count))

print("")
print("Concern categories:")
for cat, count in sorted(concern_categories.items(), key=lambda x: x[1], reverse=True):
    print("  %-25s %4d" % (cat, count))
```

### 8. Group overlap detection

```python
# group_overlap.star

col = collections()[0]
vms = col.virtual_machines()
groups = col.groups()

vm_groups = {}
empty_groups = []

for group in groups:
    group_vms = group.virtual_machines()
    if len(group_vms) == 0:
        empty_groups.append(group.name)
        continue
    for vm in group_vms:
        vm_groups.setdefault(vm.id, []).append(group.name)

overlaps = {vm_id: grps for vm_id, grps in vm_groups.items() if len(grps) > 1}

if empty_groups:
    print("Empty groups (filter matches no VMs):")
    for name in empty_groups:
        print("  - %s" % name)
    print("")

if overlaps:
    print("VMs in multiple groups (%d):" % len(overlaps))
    for vm_id, grps in sorted(overlaps.items()):
        vm = vms.get(vm_id)
        name = vm.name if vm else vm_id
        print("  %-40s -> %s" % (name, ", ".join(grps)))
else:
    print("No group overlaps detected.")

grouped_ids = set(vm_groups.keys())
unassigned = [vm for vm in vms if vm.is_migratable and vm.id not in grouped_ids]
print("")
print("Migratable VMs not in any group: %d" % len(unassigned))
for vm in unassigned[:10]:
    print("  - %s (%s)" % (vm.name, vm.cluster))
```

### 9. Migration time estimation

```python
# migration_estimate.star

col = collections()[0]
stats = forecast_stats()

if not stats:
    print("No forecast data available. Run the forecaster first.")
else:
    throughputs = [s.median_mbps for s in stats.values() if s.median_mbps > 0]
    if not throughputs:
        print("No successful benchmark runs found.")
    else:
        median_throughput = sorted(throughputs)[len(throughputs) // 2]
        print("Using median throughput: %.1f MB/s\n" % median_throughput)

        for group in col.groups():
            group_vms = group.virtual_machines()
            migratable = [vm for vm in group_vms if vm.is_migratable and not vm.migration_excluded]
            total_disk_mb = sum([vm.disk_size for vm in migratable])
            total_disk_gb = total_disk_mb / 1024.0

            if median_throughput > 0:
                est_seconds = total_disk_mb / median_throughput
                hours = est_seconds / 3600.0
            else:
                hours = 0

            print("%-30s %3d VMs  %8.1f GB  ~%.1f hours" % (
                group.name, len(migratable), total_disk_gb, hours,
            ))
```

### 10. Datastore hotspot analysis

```python
# datastore_hotspots.star

col = collections()[0]
vms = col.virtual_machines()

ds_stats = {}
for vm in vms:
    for disk in vm.disks:
        ds = disk.datastore
        entry = ds_stats.get(ds, {"vm_ids": set(), "total_gb": 0.0})
        entry["vm_ids"].add(vm.id)
        entry["total_gb"] += disk.capacity / (1024.0 * 1024.0 * 1024.0)
        ds_stats[ds] = entry

print("%-35s %5s %10s" % ("DATASTORE", "VMs", "ALLOC (GB)"))
for ds, info in sorted(ds_stats.items(), key=lambda x: x[1]["total_gb"], reverse=True):
    print("%-35s %5d %10.1f" % (ds, len(info["vm_ids"]), info["total_gb"]))
```

### 11. Application-to-cluster affinity

```python
# app_cluster_affinity.star

col = collections()[0]
vms = col.virtual_machines()

app_clusters = {}
for vm in vms:
    for app in vm.applications:
        entry = app_clusters.get(app.name, {"clusters": {}, "vm_count": 0})
        entry["clusters"][vm.cluster] = entry["clusters"].get(vm.cluster, 0) + 1
        entry["vm_count"] += 1
        app_clusters[app.name] = entry

print("%-30s %5s  %s" % ("APPLICATION", "VMs", "CLUSTERS (vm count)"))
for app_name, info in sorted(app_clusters.items(), key=lambda x: x[1]["vm_count"], reverse=True):
    cluster_str = ", ".join(["%s(%d)" % (c, n) for c, n in sorted(info["clusters"].items())])
    print("%-30s %5d  %s" % (app_name, info["vm_count"], cluster_str))

split = {k: v for k, v in app_clusters.items() if len(v["clusters"]) > 1}
if split:
    print("")
    print("WARNING: %d apps span multiple clusters (co-migration risk):" % len(split))
    for name in sorted(split.keys()):
        print("  - %s" % name)
```

---

## Templated Report Examples

### 12. Executive summary — HTML

```python
# executive_summary.star

col = collections()[0]
vms = col.virtual_machines()
inv = col.inventory()

migratable = [vm for vm in vms if vm.is_migratable and not vm.migration_excluded]
excluded = [vm for vm in vms if vm.migration_excluded]
blocked = [vm for vm in vms if not vm.is_migratable and not vm.migration_excluded]

clusters = {}
for vm in migratable:
    c = clusters.get(vm.cluster, {"count": 0, "vcpu": 0, "mem_gb": 0})
    c["count"] += 1
    c["vcpu"] += vm.cpu_count
    c["mem_gb"] += vm.memory_mb / 1024.0
    clusters[vm.cluster] = c

cluster_rows = [[name, c["count"], c["vcpu"], "%.1f" % c["mem_gb"]]
                for name, c in sorted(clusters.items())]

html = report.render("""
<html>
<head><title>Migration Assessment — {{.date}}</title>
<style>
  body { font-family: system-ui; max-width: 900px; margin: 2em auto; }
  table { border-collapse: collapse; width: 100%; }
  th, td { border: 1px solid #ddd; padding: 8px; text-align: left; }
  th { background: #2c3e50; color: white; }
  .metric { font-size: 2em; font-weight: bold; }
  .card { display: inline-block; padding: 1em; margin: 0.5em; border-radius: 8px; }
  .green { background: #e8f5e9; }
  .yellow { background: #fff8e1; }
  .red { background: #ffebee; }
</style></head>
<body>
<h1>Migration Assessment</h1>
<p>Collection: <code>{{.collection}}</code> — Last updated: {{.updated_at}}</p>

<div>
  <div class="card green">
    <div class="metric">{{.migratable_count}}</div>
    <div>Migratable</div>
  </div>
  <div class="card yellow">
    <div class="metric">{{.excluded_count}}</div>
    <div>Excluded</div>
  </div>
  <div class="card red">
    <div class="metric">{{.blocked_count}}</div>
    <div>Blocked</div>
  </div>
</div>

<h2>Cluster Breakdown</h2>
{{.cluster_table}}

<h2>Top Blockers</h2>
{{.blocker_table}}
</body></html>
""", {
    "date": "2026-08-28",
    "collection": col.id,
    "updated_at": inv.updated_at,
    "migratable_count": len(migratable),
    "excluded_count": len(excluded),
    "blocked_count": len(blocked),
    "cluster_table": report.table(
        ["Cluster", "VMs", "vCPU", "Memory (GB)"],
        cluster_rows,
        format="html",
    ),
    "blocker_table": report.table(
        ["VM", "Cluster", "Issues"],
        [[vm.name, vm.cluster, vm.issue_count]
         for vm in sorted(blocked, key=lambda v: v.issue_count, reverse=True)[:15]],
        format="html",
    ),
})

path = report.save("migration_assessment.html", html)
print("Report saved to %s" % path)
```

### 13. Custom CSV export — joined data the API exports separately

```python
# custom_export.star

col = collections()[0]
vms = col.virtual_machines()

headers = [
    "VM ID", "Name", "Cluster", "Datacenter", "Power State",
    "vCPU", "Memory (MB)", "Disk (MB)", "Migratable", "Excluded",
    "Issues", "Labels", "Groups",
    "CPU p95%", "Mem p95%", "Disk%", "Confidence%",
]

rows = []
for vm in vms:
    rows.append([
        vm.id, vm.name, vm.cluster, vm.datacenter, vm.power_state,
        vm.cpu_count, vm.memory_mb, vm.disk_size,
        vm.is_migratable, vm.migration_excluded,
        vm.issue_count,
        "; ".join(vm.labels),
        "; ".join(vm.groups),
        "%.1f" % vm.utilization_cpu_p95 if vm.utilization_cpu_p95 != None else "",
        "%.1f" % vm.utilization_mem_p95 if vm.utilization_mem_p95 != None else "",
        "%.1f" % vm.utilization_disk if vm.utilization_disk != None else "",
        "%.1f" % vm.utilization_confidence if vm.utilization_confidence != None else "",
    ])

content = report.csv(headers, rows)
path = report.save("vm_complete_export.csv", content)
print("Exported %d VMs to %s" % (len(rows), path))
```

### 14. Inspection findings — Markdown

```python
# inspection_report.star

col = collections()[0]
vms = col.virtual_machines()

inspected = [vm for vm in vms if vm.inspection_status == "completed"]

by_category = {}
for vm in inspected:
    for c in vm.inspection_concerns:
        entry = by_category.get(c.category, [])
        entry.append({"vm": vm.name, "label": c.label, "msg": c.message})
        by_category[c.category] = entry

parts = [report.section("Inspection Report",
    "**%d** VMs inspected out of %d total.\n" % (len(inspected), len(vms)))]

for cat in sorted(by_category.keys()):
    concerns = by_category[cat]
    rows = [[c["vm"], c["label"], c["msg"]] for c in concerns]
    parts.append(report.section(
        "%s (%d)" % (cat, len(concerns)),
        report.table(["VM", "Label", "Message"], rows),
    ))

md = report.concat(*parts)
path = report.save("inspection_findings.md", md)
print("Report saved to %s" % path)
```

### 15. Forecast throughput — JSON + Markdown

```python
# forecast_report.star

stats = forecast_stats()
if not stats:
    print("No forecast data available.")
else:
    json_data = []
    for name, s in sorted(stats.items()):
        json_data.append({
            "pair": s.pair_name,
            "samples": s.sample_count,
            "mean_mbps": round(s.mean_mbps, 2),
            "median_mbps": round(s.median_mbps, 2),
            "min_mbps": round(s.min_mbps, 2),
            "max_mbps": round(s.max_mbps, 2),
            "ci95": [round(s.ci95_lower, 2), round(s.ci95_upper, 2)],
        })
    report.save("forecast_stats.json", report.json(json_data))

    rows = [[s.pair_name, s.sample_count,
             "%.1f" % s.median_mbps,
             "%.1f" % s.min_mbps,
             "%.1f" % s.max_mbps,
             "%.1f-%.1f" % (s.ci95_lower, s.ci95_upper)]
            for _, s in sorted(stats.items())]

    md = report.concat(
        report.section("Forecast Results",
            report.table(
                ["Pair", "Samples", "Median MB/s", "Min", "Max", "95% CI"],
                rows,
            )),
    )
    report.save("forecast_report.md", md)
    print("Saved forecast_stats.json and forecast_report.md")
```

### 16. Migration wave plan — HTML + CSV

```python
# wave_report.star

MAX_VCPU = 128
MAX_MEM_GB = 512

col = collections()[0]
vms = col.virtual_machines()
candidates = sorted(
    [vm for vm in vms if vm.is_migratable and not vm.migration_excluded],
    key=lambda vm: vm.disk_size, reverse=True,
)

waves = []
for vm in candidates:
    mem_gb = vm.memory_mb / 1024.0
    placed = False
    for w in waves:
        if w["vcpu"] + vm.cpu_count <= MAX_VCPU and w["mem_gb"] + mem_gb <= MAX_MEM_GB:
            w["vms"].append(vm)
            w["vcpu"] += vm.cpu_count
            w["mem_gb"] += mem_gb
            placed = True
            break
    if not placed:
        waves.append({"vms": [vm], "vcpu": vm.cpu_count, "mem_gb": mem_gb})

csv_rows = []
for i, w in enumerate(waves):
    for vm in w["vms"]:
        csv_rows.append([i + 1, vm.id, vm.name, vm.cluster,
                         vm.cpu_count, vm.memory_mb, vm.disk_size])

report.save("wave_plan.csv", report.csv(
    ["Wave", "VM ID", "Name", "Cluster", "vCPU", "Memory MB", "Disk MB"],
    csv_rows,
))

wave_sections = []
for i, w in enumerate(waves):
    rows = [[vm.name, vm.cluster, vm.cpu_count, vm.memory_mb] for vm in w["vms"]]
    wave_sections.append(report.section(
        "Wave %d — %d VMs, %d vCPU, %.0f GB mem" % (
            i + 1, len(w["vms"]), w["vcpu"], w["mem_gb"]),
        report.table(["VM", "Cluster", "vCPU", "Memory MB"], rows, format="html"),
    ))

html = report.render("""
<html><head><title>Migration Wave Plan</title>
<style>
  body { font-family: system-ui; max-width: 1000px; margin: 2em auto; }
  table { border-collapse: collapse; width: 100%; margin-bottom: 2em; }
  th, td { border: 1px solid #ddd; padding: 6px; }
  th { background: #34495e; color: white; }
  h2 { border-bottom: 2px solid #3498db; padding-bottom: 0.3em; }
</style></head><body>
<h1>Migration Wave Plan</h1>
<p>{{.vm_count}} VMs across {{.wave_count}} waves
   (caps: {{.max_vcpu}} vCPU, {{.max_mem}} GB mem per wave)</p>
{{.wave_content}}
</body></html>
""", {
    "vm_count": len(candidates),
    "wave_count": len(waves),
    "max_vcpu": MAX_VCPU,
    "max_mem": MAX_MEM_GB,
    "wave_content": report.concat(*wave_sections),
})

path = report.save("wave_plan.html", html)
print("Saved wave_plan.csv and %s" % path)
```

### 17. Forklift migration plan per wave

```python
# forklift_plans.star — Generate Forklift Plan CRs from wave-planned VMs

MAX_VCPU = 128
MAX_MEM_GB = 512

col = collections()[0]
vms = col.virtual_machines()
candidates = sorted(
    [vm for vm in vms if vm.is_migratable and not vm.migration_excluded],
    key=lambda vm: vm.disk_size, reverse=True,
)

waves = []
for vm in candidates:
    mem_gb = vm.memory_mb / 1024.0
    placed = False
    for w in waves:
        if w["vcpu"] + vm.cpu_count <= MAX_VCPU and w["mem_gb"] + mem_gb <= MAX_MEM_GB:
            w["vms"].append(vm)
            w["vcpu"] += vm.cpu_count
            w["mem_gb"] += mem_gb
            placed = True
            break
    if not placed:
        waves.append({"vms": [vm], "vcpu": vm.cpu_count, "mem_gb": mem_gb})

plan_template = """apiVersion: forklift.konveyor.io/v1beta1
kind: Plan
metadata:
  name: {{.plan_name}}
  namespace: {{.namespace}}
spec:
  provider:
    source:
      name: {{.source_provider}}
      namespace: {{.namespace}}
    destination:
      name: {{.dest_provider}}
      namespace: {{.namespace}}
  map:
    network:
      name: {{.network_map}}
      namespace: {{.namespace}}
    storage:
      name: {{.storage_map}}
      namespace: {{.namespace}}
  targetNamespace: {{.target_namespace}}
  vms:
{{.vm_entries}}"""

all_plans = []
for i, w in enumerate(waves):
    vm_entries = ""
    for vm in w["vms"]:
        vm_entries += "    - id: %s\n      name: %s\n" % (vm.id, vm.name)

    plan = report.render(plan_template, {
        "plan_name": "wave-%d" % (i + 1),
        "namespace": "openshift-mtv",
        "source_provider": "vmware-source",
        "dest_provider": "host",
        "network_map": "network-map",
        "storage_map": "storage-map",
        "target_namespace": "migrated-vms",
        "vm_entries": vm_entries,
    })
    all_plans.append(plan)

content = "---\n".join(all_plans)
path = report.save("forklift-plans.yaml", content)
print("Generated %d Forklift plans (%d VMs) → %s" % (len(waves), len(candidates), path))
```

### 18. Forklift network and storage maps

```python
# forklift_maps.star — Generate Forklift NetworkMap and StorageMap from collection data

col = collections()[0]
vms = col.virtual_machines()

datastores = list(set([vm.cluster + "/" + vm.datacenter for vm in vms]))
clusters = list(set([vm.cluster for vm in vms]))

network_map = report.render("""apiVersion: forklift.konveyor.io/v1beta1
kind: NetworkMap
metadata:
  name: {{.map_name}}
  namespace: {{.namespace}}
spec:
  provider:
    source:
      name: {{.source_provider}}
      namespace: {{.namespace}}
    destination:
      name: {{.dest_provider}}
      namespace: {{.namespace}}
  map:
{{.entries}}""", {
    "map_name": "network-map",
    "namespace": "openshift-mtv",
    "source_provider": "vmware-source",
    "dest_provider": "host",
    "entries": "    - source:\n        type: pod\n      destination:\n        type: pod\n",
})

storage_map = report.render("""apiVersion: forklift.konveyor.io/v1beta1
kind: StorageMap
metadata:
  name: {{.map_name}}
  namespace: {{.namespace}}
spec:
  provider:
    source:
      name: {{.source_provider}}
      namespace: {{.namespace}}
    destination:
      name: {{.dest_provider}}
      namespace: {{.namespace}}
  map:
{{.entries}}""", {
    "map_name": "storage-map",
    "namespace": "openshift-mtv",
    "source_provider": "vmware-source",
    "dest_provider": "host",
    "entries": "    - source:\n        name: default\n      destination:\n        storageClass: ocs-storagecluster-ceph-rbd\n",
})

content = network_map + "\n---\n" + storage_map
path = report.save("forklift-maps.yaml", content)
print("Generated NetworkMap + StorageMap → %s" % path)
```

### 19. Ansible pre-migration playbook

```python
# ansible_playbook.star — Generate an Ansible playbook for pre-migration tasks

col = collections()[0]
groups = col.groups()

playbook_template = """---
{{range .waves}}
- name: "Pre-migration — {{.group_name}}"
  hosts: {{.group_name}}
  become: true
  vars:
    migration_wave: "{{.group_name}}"
  tasks:
    - name: Gather facts
      setup:

    - name: Check disk space
      assert:
        that: ansible_mounts | selectattr('mount', 'equalto', '/') | map(attribute='size_available') | first > 1073741824
        fail_msg: "Less than 1 GB free on /"

    - name: Stop application services
      service:
        name: "{{"{{"}}" }} item {{"{{"}}" }}"
        state: stopped
      loop:
        - httpd
        - mysqld
      ignore_errors: true

    - name: Flush filesystem buffers
      command: sync

    - name: Write migration marker
      copy:
        content: "migration_wave={{.group_name}} timestamp={{"{{"}}" }} ansible_date_time.iso8601 {{"{{"}}" }}"
        dest: /etc/migration-marker

{{end}}"""

inventory_lines = ["[all:vars]", "ansible_user=migrate", "ansible_become=true", ""]

wave_data = []
for group in groups:
    group_vms = group.virtual_machines()
    migratable = [vm for vm in group_vms if vm.is_migratable and not vm.migration_excluded]
    if not migratable:
        continue

    wave_data.append({"group_name": group.name})
    inventory_lines.append("[%s]" % group.name)
    for vm in migratable:
        inventory_lines.append("%s  # %s %d vCPU %d MB" % (
            vm.name, vm.cluster, vm.cpu_count, vm.memory_mb))
    inventory_lines.append("")

playbook = report.render(playbook_template, {"waves": wave_data})
inventory = "\n".join(inventory_lines)

pb_path = report.save("pre_migration.yml", playbook)
inv_path = report.save("inventory.ini", inventory)
print("Generated playbook → %s" % pb_path)
print("Generated inventory (%d groups) → %s" % (len(wave_data), inv_path))
```

---

## Diff Module Builtins

The `diff` module provides structured comparison tools across collections, VMs, and inventories. The existing REST API only compares two collections with aggregate counts (total/migratable/non-migratable). The diff module goes deeper: field-level VM changes, resource drift, multi-collection timelines, and ranked change reports.

### Type Handling

All `diff` return types preserve native Starlark values — `int`, `float`, `string`, `bool`, `list`, `None` — not stringified representations. The Go side dispatches per field name through a bounded converter:

```go
func fieldToStarlark(v any) starlark.Value {
    switch val := v.(type) {
    case int, int32, int64:
        return starlark.MakeInt64(reflect.ValueOf(val).Int())
    case float64:
        return starlark.Float(val)
    case bool:
        return starlark.Bool(val)
    case string:
        return starlark.String(val)
    case []string:
        elems := make([]starlark.Value, len(val))
        for i, s := range val {
            elems[i] = starlark.String(s)
        }
        return starlark.NewList(elems)
    case nil:
        return starlark.None
    default:
        return starlark.String(fmt.Sprintf("%v", val))
    }
}
```

This means arithmetic, list operations, and comparisons work naturally in scripts:

```python
if c.field == "memory_mb":
    delta = c.new - c.old                              # int arithmetic
elif c.field == "labels":
    added = [l for l in c.new if l not in c.old]       # list ops
elif c.field == "utilization_cpu_p95" and c.new != None:
    print("%.1f%%" % c.new)                            # None guard for nullable floats
```

### `FieldChange` Struct

Returned by `diff.vm()` and `diff.fields()`. One per changed field.

```
.field    string    # field name (e.g. "memory_mb", "cluster", "labels")
.old      any       # value in source — native Starlark type (int/float/string/bool/list/None)
.new      any       # value in target — same type as .old
```

For list fields (labels, groups, disks), `.old` and `.new` are full lists. Use set operations in the script to derive added/removed.

### `VMDiff` Struct

Returned by `diff.vms()`. Categorizes VMs across two collections.

```
.added      List[VirtualMachine]   # present in B, absent from A
.removed    List[VirtualMachine]   # present in A, absent from B
.changed    List[VMChange]         # present in both, at least one field differs
.unchanged  int                    # count of VMs identical in both
```

### `VMChange` Struct

One entry per VM that exists in both collections but has differences.

```
.vm_id     string              # the VM ID
.vm_a      VirtualMachine      # the VM from collection A
.vm_b      VirtualMachine      # the VM from collection B
.changes   List[FieldChange]   # only the fields that differ
```

### `SetDiff` Struct

Returned by `diff.sets()`. Generic set-difference result.

```
.only_in_a  List[string]   # items in A but not B
.only_in_b  List[string]   # items in B but not A
.common     List[string]   # items in both
```

### `TimelinePoint` Struct

Returned by `diff.timeline()`. One data point per collection.

```
.collection  Collection   # the collection object
.value       any          # the computed value for this collection
```

---

### `diff.vms(vms_a, vms_b)`

```
(vms_a: VirtualMachineList, vms_b: VirtualMachineList) → VMDiff
```

Bulk comparison of two VM lists. Matches VMs by `.id`, then runs field-level diff on each matched pair. Returns a `VMDiff` with added/removed/changed/unchanged.

```python
cols = collections()
vms_a = cols[0].virtual_machines()
vms_b = cols[1].virtual_machines()

d = diff.vms(vms_a, vms_b)
print("Added: %d, Removed: %d, Changed: %d, Unchanged: %d" % (
    len(d.added), len(d.removed), len(d.changed), d.unchanged))

for c in d.changed:
    fields = ", ".join([f.field for f in c.changes])
    print("  %s: %s" % (c.vm_id, fields))
```

Maps to: in-memory set operations on VM ID, then `diff.vm()` per matched pair. No direct store equivalent — the API's `ComparisonService` only does aggregate counts.

### `diff.vm(vm_a, vm_b)`

```
(vm_a: VirtualMachine, vm_b: VirtualMachine) → List[FieldChange]
```

Compares a single VM across two collections. Walks all comparable fields and returns only those that differ, with native-typed `.old` and `.new` values.

```python
changes = diff.vm(vm_old, vm_new)
for c in changes:
    print("%-20s  %s → %s" % (c.field, c.old, c.new))
```

Compared fields (all `VirtualMachine` struct fields):

| Scalars | Lists |
|---|---|
| `name`, `power_state`, `cluster`, `datacenter` | `labels` |
| `cpu_count`, `memory_mb`, `disk_size` | `groups` |
| `is_migratable`, `is_template`, `migration_excluded` | |
| `issue_count`, `inspection_status`, `inspection_concern_count` | |
| `os_name`, `firmware`, `folder`, `host`, `hostname`, `ip_address` | |
| `utilization_cpu_p95`, `utilization_mem_p95`, etc. (nullable) | |

### `diff.sets(list_a, list_b)`

```
(list_a: List[string], list_b: List[string]) → SetDiff
```

Generic set-difference on string lists. Useful for comparing cluster names, datastore names, labels, group names — anything extracted from two collections.

```python
clusters_a = list(set([vm.cluster for vm in vms_a]))
clusters_b = list(set([vm.cluster for vm in vms_b]))
d = diff.sets(clusters_a, clusters_b)
print("New clusters: %s" % d.only_in_b)
print("Removed clusters: %s" % d.only_in_a)
```

### `diff.timeline(collections, fn)`

```
(collections: List[Collection], fn: function(Collection) → any) → List[TimelinePoint]
```

Evaluates a function against each collection and returns a time-series. The function receives a `Collection` object and returns a scalar or dict. Enables trend analysis across N collections — something the pairwise API cannot do.

```python
cols = collections()

points = diff.timeline(cols, lambda col: len(col.virtual_machines()))
for p in points:
    print("%s: %d VMs" % (p.collection.id, p.value))
```

### `diff.fields(obj_a, obj_b, fields)`

```
(obj_a: struct, obj_b: struct, fields: List[string]) → List[FieldChange]
```

Compare specific fields between any two structs of the same type. A more targeted version of `diff.vm()` — lets you pick exactly which fields to compare.

```python
changes = diff.fields(vm_old, vm_new, ["cpu_count", "memory_mb", "disk_size"])
```

### `diff.summary(vms_a, vms_b)`

```
(vms_a: VirtualMachineList, vms_b: VirtualMachineList) → dict
```

Returns a dict of aggregate change statistics without per-VM detail. Cheaper than `diff.vms()` for dashboard/report use.

```python
s = diff.summary(vms_a, vms_b)
# {
#   "added": 12,
#   "removed": 3,
#   "changed": 45,
#   "unchanged": 340,
#   "total_a": 388,
#   "total_b": 397,
#   "fields_changed": {"memory_mb": 20, "cluster": 8, "power_state": 17, ...},
# }
```

`fields_changed` is a dict mapping field name to the count of VMs where that field changed — tells you what *kind* of drift is happening without listing every VM.

### `diff.rank_changes(vms_a, vms_b, field)`

```
(vms_a: VirtualMachineList, vms_b: VirtualMachineList, field: string) → List[VMChange]
```

Finds VMs where a specific field changed, sorted by magnitude of change (largest delta first for numeric fields, alphabetical for string fields). Returns only changed VMs, not the full diff.

```python
# Which VMs had the biggest memory changes?
top = diff.rank_changes(vms_a, vms_b, "memory_mb")
for c in top[:10]:
    ch = c.changes[0]  # single field
    print("%-30s  %d → %d MB  (%+d)" % (c.vm_a.name, ch.old, ch.new, ch.new - ch.old))
```

### `diff.where(vms_a, vms_b, fn)`

```
(vms_a: VirtualMachineList, vms_b: VirtualMachineList, fn: callable) → List[Dict]
```

Predicate-based diff. Matches VMs by ID, applies `fn(vm)` to both versions, and returns only VMs where the result differs. Each entry is a dict with `vm_a`, `vm_b`, `old`, `new`.

```python
# VMs that changed cluster
changes = diff.where(vms_a, vms_b, lambda vm: vm.cluster)
for c in changes:
    print(sprintf("%s: %s -> %s", c["vm_a"].name, c["old"], c["new"]))

# VMs where a computed value changed
changes = diff.where(vms_a, vms_b, lambda vm: vm.memory_mb > 32768)
```

Useful for detecting specific kinds of drift without inspecting all fields. The predicate can return any hashable value — strings, ints, bools, tuples.

### `diff.distribution(dict_a, dict_b, top_n)`

```
(dict_a: Dict[str → int], dict_b: Dict[str → int], top_n: int) → Dict
```

Compare two count dictionaries (e.g. from `count_by`) with top-N folding. Merges all keys, ranks by combined total descending, keeps the top N entries, and folds the rest into "Other". Returns a dict with three parallel lists:

- `labels`: list of category names (top N + "Other" if needed)
- `values_a`: counts from dict_a for each label
- `values_b`: counts from dict_b for each label

```python
os_a = vms_a.count_by(lambda vm: vm.os_name if vm.os_name else "Unknown")
os_b = vms_b.count_by(lambda vm: vm.os_name if vm.os_name else "Unknown")
d = diff.distribution(os_a, os_b, 8)

# d["labels"]   → ["RHEL 8", "Windows Server 2019", ..., "Other"]
# d["values_a"] → [120, 45, ..., 12]
# d["values_b"] → [118, 50, ..., 15]
```

Keys present in one dict but not the other get a count of 0. Useful for building side-by-side comparison charts between collections.

### `diff.resource_delta(vms_a, vms_b)`

```
(vms_a: VirtualMachineList, vms_b: VirtualMachineList) → Dict
```

Compute aggregate resource deltas between two VM lists (B − A). Returns a dict with:

- `cpu`: int — total vCPU difference
- `memory_mb`: int — total memory difference in MB
- `disk_mb`: int — total disk difference in MB

```python
delta = diff.resource_delta(vms_a, vms_b)
print(sprintf("CPU: %+d, RAM: %+d MB, Disk: %+d MB",
    delta["cpu"], delta["memory_mb"], delta["disk_mb"]))
```

Positive values mean collection B has more resources; negative means less.

---

## VirtualMachineList.totals

### `vms.totals(fields)`

```
(fields: List[str]) → Dict[str → int|float]
```

Sum the specified numeric VM attributes across all VMs in the list. Returns a dict mapping each field name to its total. Int fields produce int sums; float fields produce float sums. None values are skipped.

```python
t = vms.totals(["cpu_count", "memory_mb", "disk_size"])
print(sprintf("Total: %d vCPU, %.1f GB RAM, %.1f GB disk",
    t["cpu_count"], t["memory_mb"] / 1024.0, t["disk_size"] / 1024.0))

# Works with any numeric VM attribute
u = vms.totals(["utilization_cpu_p95", "utilization_mem_p95"])
```

---

## Debug Module

The `debug` module provides introspection tools for exploring data while writing scripts. All functions return a formatted string and print it to stdout.

### `debug.dump(value)`

```
(value: any) → string
```

Pretty-prints any value with all its fields and values. Knows about all scripting types.

```python
debug.dump(vm)
# VirtualMachine:
#   cluster:                     "prod"
#   cpu_count:                   4
#   memory_mb:                   8192
#   is_migratable:               True
#   ...

debug.dump(vms)
# VirtualMachineList (101 items):
#   [0] "web-1"  prod  4 vCPU  8192 MB
#   [1] "db-1"  prod  8 vCPU  16384 MB
#   ... (91 more)
#   [100] "cache-3"  dev  2 vCPU  4096 MB

debug.dump({"total": 400, "migratable": 101})
# dict:
#   "total":              400
#   "migratable":         101
```

### `debug.describe(value)`

```
(value: any) → string
```

Shows the type name and available attributes. Like an enhanced `dir()`.

```python
debug.describe(vm)
# VirtualMachine — attributes:
#   cluster, cpu_count, datacenter, disk_size, disks, groups, ...
```

### `debug.peek(vms, fields, limit=20)`

```
(vms: VirtualMachineList, fields: List[string], limit: int) → string
```

Tabular preview of a VirtualMachineList with selected fields, column-aligned.

```python
debug.peek(vms, ["name", "cluster", "memory_mb", "is_migratable"])
# name           cluster     memory_mb  is_migratable
# web-1          prod        8192       True
# db-1           prod        16384      True
# ... (97 more, 101 total)

debug.peek(vms, ["name", "memory_mb"], limit=5)
```

---

## Comparison Example Scripts

### 20. Full collection diff report

```python
# collection_diff.star — Field-level diff between two collections

cols = collections()
if len(cols) < 2:
    print("Need at least 2 collections")
else:
    vms_a = cols[0].virtual_machines()
    vms_b = cols[1].virtual_machines()

    d = diff.vms(vms_a, vms_b)

    print("=== %s vs %s ===" % (cols[0].id, cols[1].id))
    print("Added:     %d" % len(d.added))
    print("Removed:   %d" % len(d.removed))
    print("Changed:   %d" % len(d.changed))
    print("Unchanged: %d" % d.unchanged)

    if d.added:
        print("\nNew VMs:")
        for vm in d.added[:10]:
            print("  + %-40s  %s  %d vCPU  %d MB" % (
                vm.name, vm.cluster, vm.cpu_count, vm.memory_mb))

    if d.removed:
        print("\nRemoved VMs:")
        for vm in d.removed[:10]:
            print("  - %-40s  %s" % (vm.name, vm.cluster))

    if d.changed:
        print("\nChanged VMs:")
        for c in d.changed[:20]:
            fields = ", ".join([f.field for f in c.changes])
            print("  ~ %-40s  [%s]" % (c.vm_a.name, fields))
```

### 21. Resource drift detection

```python
# resource_drift.star — Find VMs whose CPU/memory/disk changed between collections

cols = collections()
vms_a = cols[0].virtual_machines()
vms_b = cols[1].virtual_machines()

resource_fields = ["cpu_count", "memory_mb", "disk_size"]

d = diff.vms(vms_a, vms_b)
drifted = []
for c in d.changed:
    resource_changes = [f for f in c.changes if f.field in resource_fields]
    if resource_changes:
        drifted.append((c, resource_changes))

print("Resource drift: %d VMs changed CPU/memory/disk\n" % len(drifted))
print("%-35s %-12s %15s %15s %10s" % ("VM", "FIELD", "OLD", "NEW", "DELTA"))
for c, changes in drifted:
    for f in changes:
        delta = f.new - f.old
        print("%-35s %-12s %15s %15s %+10d" % (
            c.vm_a.name, f.field, f.old, f.new, delta))
```

### 22. Cluster mobility tracker

```python
# cluster_mobility.star — VMs that moved between clusters across collections

cols = collections()
vms_a = cols[0].virtual_machines()
vms_b = cols[1].virtual_machines()

d = diff.vms(vms_a, vms_b)
moves = []
for c in d.changed:
    cluster_change = [f for f in c.changes if f.field == "cluster"]
    if cluster_change:
        moves.append((c.vm_a.name, cluster_change[0].old, cluster_change[0].new))

if not moves:
    print("No VMs moved clusters.")
else:
    print("%d VMs moved clusters:\n" % len(moves))
    paths = {}
    for name, old, new in moves:
        key = "%s → %s" % (old, new)
        paths.setdefault(key, []).append(name)

    for path, names in sorted(paths.items(), key=lambda x: len(x[1]), reverse=True):
        print("%s  (%d VMs)" % (path, len(names)))
        for n in names[:5]:
            print("  - %s" % n)
        if len(names) > 5:
            print("  ... +%d more" % (len(names) - 5))
```

### 23. Multi-collection timeline

```python
# timeline.star — Track key metrics across all collections

cols = collections()

points_total      = diff.timeline(cols, lambda col: len(col.virtual_machines()))
points_migratable = diff.timeline(cols, lambda col: len([vm for vm in col.virtual_machines() if vm.is_migratable]))
points_excluded   = diff.timeline(cols, lambda col: len([vm for vm in col.virtual_machines() if vm.migration_excluded]))
points_issues     = diff.timeline(cols, lambda col: sum([vm.issue_count for vm in col.virtual_machines()]))

print("%-20s %12s %12s %12s %12s" % ("COLLECTION", "TOTAL", "MIGRATABLE", "EXCLUDED", "ISSUES"))
for i, col in enumerate(cols):
    print("%-20s %12d %12d %12d %12d" % (
        col.id,
        points_total[i].value,
        points_migratable[i].value,
        points_excluded[i].value,
        points_issues[i].value,
    ))
```

### 24. Infrastructure topology diff

```python
# topology_diff.star — Compare clusters, datastores, and networks between collections

cols = collections()
vms_a = cols[0].virtual_machines()
vms_b = cols[1].virtual_machines()

# Cluster diff
clusters_a = list(set([vm.cluster for vm in vms_a]))
clusters_b = list(set([vm.cluster for vm in vms_b]))
cd = diff.sets(clusters_a, clusters_b)

print("=== Cluster Changes ===")
print("  Common:  %d" % len(cd.common))
if cd.only_in_a:
    print("  Removed: %s" % ", ".join(cd.only_in_a))
if cd.only_in_b:
    print("  Added:   %s" % ", ".join(cd.only_in_b))

# Datastore diff
ds_a = list(set([d.datastore for vm in vms_a for d in vm.disks]))
ds_b = list(set([d.datastore for vm in vms_b for d in vm.disks]))
dd = diff.sets(ds_a, ds_b)

print("\n=== Datastore Changes ===")
print("  Common:  %d" % len(dd.common))
if dd.only_in_a:
    print("  Removed: %s" % ", ".join(dd.only_in_a))
if dd.only_in_b:
    print("  Added:   %s" % ", ".join(dd.only_in_b))

# Datacenter diff
dc_a = list(set([vm.datacenter for vm in vms_a]))
dc_b = list(set([vm.datacenter for vm in vms_b]))
dcd = diff.sets(dc_a, dc_b)

print("\n=== Datacenter Changes ===")
print("  Common:  %d" % len(dcd.common))
if dcd.only_in_a:
    print("  Removed: %s" % ", ".join(dcd.only_in_a))
if dcd.only_in_b:
    print("  Added:   %s" % ", ".join(dcd.only_in_b))
```

### 25. Ranked change analysis

```python
# biggest_changes.star — What changed the most between collections?

cols = collections()
vms_a = cols[0].virtual_machines()
vms_b = cols[1].virtual_machines()

# Field-level change frequency
s = diff.summary(vms_a, vms_b)
print("=== Change Summary ===")
print("Total A: %d → Total B: %d (delta: %+d)\n" % (
    s["total_a"], s["total_b"], s["total_b"] - s["total_a"]))

print("Most commonly changed fields:")
ranked = sorted(s["fields_changed"].items(), key=lambda x: x[1], reverse=True)
for field, count in ranked:
    print("  %-25s %d VMs" % (field, count))

# Top memory changes
print("\nBiggest memory changes:")
top_mem = diff.rank_changes(vms_a, vms_b, "memory_mb")
for c in top_mem[:10]:
    ch = c.changes[0]
    print("  %-35s  %6d → %6d MB  (%+d)" % (
        c.vm_a.name, ch.old, ch.new, ch.new - ch.old))

# Top disk changes
print("\nBiggest disk changes:")
top_disk = diff.rank_changes(vms_a, vms_b, "disk_size")
for c in top_disk[:10]:
    ch = c.changes[0]
    delta_gb = (ch.new - ch.old) / 1024.0
    print("  %-35s  %+.1f GB" % (c.vm_a.name, delta_gb))
```

### 26. Migratability change tracking

```python
# migratability_changes.star — VMs that became migratable or lost migratability

cols = collections()
vms_a = cols[0].virtual_machines()
vms_b = cols[1].virtual_machines()

d = diff.vms(vms_a, vms_b)

became_migratable = []
lost_migratable = []

for c in d.changed:
    mig_change = [f for f in c.changes if f.field == "is_migratable"]
    if mig_change:
        if mig_change[0].new == True:
            became_migratable.append(c)
        else:
            lost_migratable.append(c)

print("Newly migratable: %d VMs" % len(became_migratable))
for c in became_migratable:
    other_changes = [f.field for f in c.changes if f.field != "is_migratable"]
    reason = " (also changed: %s)" % ", ".join(other_changes) if other_changes else ""
    print("  + %s%s" % (c.vm_b.name, reason))

print("\nLost migratability: %d VMs" % len(lost_migratable))
for c in lost_migratable:
    issue_change = [f for f in c.changes if f.field == "issue_count"]
    if issue_change:
        print("  - %s (issues: %d → %d)" % (c.vm_a.name, issue_change[0].old, issue_change[0].new))
    else:
        print("  - %s" % c.vm_a.name)
```

### 27. Collection diff HTML report

```python
# diff_report.star — HTML report comparing two collections

cols = collections()
vms_a = cols[0].virtual_machines()
vms_b = cols[1].virtual_machines()

d = diff.vms(vms_a, vms_b)
s = diff.summary(vms_a, vms_b)

added_rows = [[vm.name, vm.cluster, vm.cpu_count, vm.memory_mb, vm.disk_size]
              for vm in d.added]
removed_rows = [[vm.name, vm.cluster, vm.cpu_count, vm.memory_mb, vm.disk_size]
                for vm in d.removed]
changed_rows = [[c.vm_a.name, c.vm_a.cluster, len(c.changes),
                  ", ".join([f.field for f in c.changes])]
                for c in d.changed[:50]]

field_rows = [[field, count] for field, count in
              sorted(s["fields_changed"].items(), key=lambda x: x[1], reverse=True)]

html = report.render("""
<html><head><title>Collection Diff</title>
<style>
  body { font-family: system-ui; max-width: 1000px; margin: 2em auto; }
  table { border-collapse: collapse; width: 100%; margin-bottom: 2em; }
  th, td { border: 1px solid #ddd; padding: 6px; }
  th { background: #34495e; color: white; }
  .added { color: #27ae60; }
  .removed { color: #e74c3c; }
  .metric { font-size: 1.5em; font-weight: bold; }
</style></head><body>
<h1>Collection Diff</h1>
<p><code>{{.col_a}}</code> → <code>{{.col_b}}</code></p>

<div>
  <span class="added metric">+{{.added}}</span> added &nbsp;
  <span class="removed metric">-{{.removed}}</span> removed &nbsp;
  <span class="metric">~{{.changed}}</span> changed &nbsp;
  <span>={{.unchanged}} unchanged</span>
</div>

<h2>Most Changed Fields</h2>
{{.field_table}}

<h2>Added VMs</h2>
{{.added_table}}

<h2>Removed VMs</h2>
{{.removed_table}}

<h2>Changed VMs (top 50)</h2>
{{.changed_table}}
</body></html>
""", {
    "col_a": cols[0].id,
    "col_b": cols[1].id,
    "added": len(d.added),
    "removed": len(d.removed),
    "changed": len(d.changed),
    "unchanged": d.unchanged,
    "field_table": report.table(["Field", "VMs Changed"], field_rows, format="html"),
    "added_table": report.table(
        ["Name", "Cluster", "vCPU", "Memory MB", "Disk MB"], added_rows, format="html"),
    "removed_table": report.table(
        ["Name", "Cluster", "vCPU", "Memory MB", "Disk MB"], removed_rows, format="html"),
    "changed_table": report.table(
        ["Name", "Cluster", "Fields Changed", "Details"], changed_rows, format="html"),
})

path = report.save("collection_diff.html", html)
print("Report: %s" % path)
```

### 28. Label drift between collections

```python
# label_drift.star — Track how labels evolved across two collections

cols = collections()
vms_a = cols[0].virtual_machines()
vms_b = cols[1].virtual_machines()

# Per-VM label changes
d = diff.vms(vms_a, vms_b)
label_added = {}
label_removed = {}

for c in d.changed:
    label_change = [f for f in c.changes if f.field == "labels"]
    if not label_change:
        continue
    old_labels = set(label_change[0].old)
    new_labels = set(label_change[0].new)
    for l in new_labels - old_labels:
        label_added.setdefault(l, []).append(c.vm_b.name)
    for l in old_labels - new_labels:
        label_removed.setdefault(l, []).append(c.vm_a.name)

# Global label set diff
all_labels_a = list(set([l for vm in vms_a for l in vm.labels]))
all_labels_b = list(set([l for vm in vms_b for l in vm.labels]))
ld = diff.sets(all_labels_a, all_labels_b)

print("=== Label Drift ===\n")

if ld.only_in_b:
    print("New labels: %s" % ", ".join(ld.only_in_b))
if ld.only_in_a:
    print("Removed labels: %s" % ", ".join(ld.only_in_a))

print("\nLabels applied to more VMs:")
for label, names in sorted(label_added.items(), key=lambda x: len(x[1]), reverse=True):
    print("  +%-20s  %d VMs" % (label, len(names)))

print("\nLabels removed from VMs:")
for label, names in sorted(label_removed.items(), key=lambda x: len(x[1]), reverse=True):
    print("  -%-20s  %d VMs" % (label, len(names)))
```

---

## Implementation Notes

1. **Complete VMs**: `Collection.virtual_machines()` and `Group.virtual_machines()` return VMs with all detail fields (disks, nics, issues, inspection concerns, applications, networks) pre-populated. Detail fields are direct attributes — no method calls or lazy-fetching needed.

2. **None handling**: Nullable fields (utilization `*float64` in Go) map to Starlark `None`. Scripts check `if vm.utilization_cpu_p95 != None`.

3. **Collection scoping**: All data access flows through `Collection` objects. The collection routes internally to the correct attached DuckDB database, matching how `Store2` / `RunCollection` already work.

4. **Read-only**: All builtins are read-only queries. No builtin mutates state. `report.save` writes to a sandboxed output directory only.

5. **CSV sanitization**: `report.csv` applies the same formula-injection prevention as `ExportService` (prefixing `=`, `+`, `@`, `\t`, `\r`, `-` with `'`).

6. **VirtualMachineList.get(id)**: Looks up a VM by ID within the current set. On a filtered VirtualMachineList, returns `None` if the ID exists in the collection but was excluded by the filter.
