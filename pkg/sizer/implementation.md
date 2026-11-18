# Implementation

This document describes how the sizer builds bundles, places them on nodes, and
reports resources. The public API is documented in [README.md](README.md).

## How it works

TL;DR: Sizer is computing the capacity of a cluster by trying to place bundles on the nodes.
A bundle is a group of services that satisfy `RunsWith` and `Avoid` rules defined in workloads.
It also carries the count value from workloads.
Each bundle contains placement rules that allow or deny the bundle on a node.
For example: a service in bundle A has an `Avoid` rule with a service from bundle B.
In order to respect this rule, the bundle A should not be placed on the same node with bundle B.
When bundle A is placed on node 1, the sizer adds a corresponding denying rule in bundle B for that same node.

1. Every service of a positive-count workload becomes a bundle on its own.
   Services in the same workload are not assumed to share a node.
2. `RunsWith` links join the whole bundles they connect. `Avoid` conflicts split
   a service back out into its own bundle.
3. Each bundle keeps its workload's `AllowedMachineSets` and `Count`, the
   services it holds, and cached CPU and memory totals. Once the bundle list is
   final, every bundle also records the bundles whose services it must avoid.
4. Placing a bundle means placing all of its copies, each on a different node.
   `Place` reuses an existing node when the bundle may use it and it still fits;
   otherwise `CreateAndPlace` creates a node from an eligible machine set and
   places the bundle there.
5. Placement walks the bundles in ID order and stops when every copy is placed,
   or returns a resource error when no eligible machine set can hold a bundle.

## Bundles

- A positive-count workload contributes one bundle per service.
- `RunsWith` joins whole bundles, within or across workloads.
- Service names must be unique across all workloads, including zero-count ones.
  `RunsWith` and `Avoid` reference services by name, so a duplicate name would
  make the rule ambiguous; `Calculate` rejects the request with
  `duplicate service name "<name>"`.
- Unknown names and self references are ignored.
- `Avoid` wins over a conflicting `RunsWith` link regardless of input order.
  Once bundles are merged, an `Avoid` conflict inside a bundle moves the later
  service into a separate bundle.
- A joined bundle uses the larger `Count`, so a smaller-count peer's services
  are replicated to match. Joined services cannot be split across bundles
  without breaking `RunsWith`, and per-service copy counts would dissolve the
  bundle as the unit of placement. A bundle created by an `Avoid` split keeps
  the split service's original workload `Count` and `AllowedMachineSets`.
- Joining merges allowed machine sets: an empty list (any set) absorbs the other;
  otherwise the merged bundle may use the union of both lists.
- Zero-count workloads are excluded before resolving relationships; negative
  counts are treated like zero.
- A bundle is not replicated per copy: it carries its workload `Count`, and
  placement expands that count into copies.

## Placement

### Node rules

Rules name nodes and are explicit. Before a bundle places its first copy, it
receives one rule for every existing node:

- allowed when the node's machine set is eligible and the node holds no service
  with a bidirectional `Avoid` conflict against the bundle;
- denied otherwise.

A missing rule never allows a node. After each placed copy the bundle also denies
that node, so copies cannot share it and explicit denies remain denied. Placement
propagates denies as well: when a bundle places a copy, every remaining bundle
that must avoid the placed services receives a deny rule for that node.

An empty allowed list permits any eligible machine set. Control-plane sets are
eligible only when `AllowWorkloadScheduling` is explicitly true, even when named
by a workload or when all supplied sets are control-plane sets.

### Reusing or creating a node

- `Place` tries existing allowed nodes in creation order.
- `CreateAndPlace` creates a node from the fitting eligible template with the
  most usable CPU.
- A bundle places its copies one at a time, until all copies are placed or a
  placement fails.
- There is no node budget: nodes are created as demand requires.

Capacity checks include the entire bundle plus kubelet reservations and, for
control planes, `ControlPlaneReservedResources`. Limits do not affect placement.

### Failure

Before placement, `Calculate` rejects workloads whose `AllowedMachineSets` names
an unknown set, or whose allowed sets all refuse workloads (including an empty
list when every set is a closed control plane).

At placement, `NotEnoughResourcesError` means a permitted machine set exists but
none is large enough. Either failure aborts the whole calculation: the remaining
bundles and copies are dropped and no partial response is returned.

## Resource reporting

The example in [README.md](README.md) creates one worker: physical totals are
8 CPU and 32 GB; service consumption is 1 CPU and 2 GB.

- `Detailed` controls the optional `Nodes` node details.
- Detailed requested resources and aggregate consumption count placed services,
  excluding scheduler reservations.
- Physical totals count created nodes.
- No active services produce zero nodes and an empty node list.
- Limits and overcommit ratios keep their reporting-only meaning.

### Limits and ratios

- A zero CPU or memory limit independently falls back to the corresponding
  request.
- Aggregate limits and ratios are present when any placed service supplies a
  nonzero limit; node limits and ratios follow the same rule per node.
- Ratios divide limits by physical capacity minus kubelet reservations.
- The calculator does not apply overcommit multipliers to requests.
- Resource arithmetic remains native `float64`, without rounding.

### Reservations

CPU kubelet reservation is 0.06, plus 0.01 for the second core, 0.005 for each of
the next two cores, and 0.0025 for each core above four.

Memory preserves the historical tiers:

- below 1 GB: reserve 0.255 GB;
- at 1 GB: reserve zero;
- above 1 through 4 GB: reserve 25% of (M−1);
- above 4 GB: the first tier is 1 GB, plus 20% of the next 4 GB, 10% of the next
  8 GB, 6% of the next 112 GB, and 2% above 128 GB.

These formulas intentionally retain the discontinuity above 4 GB.

## Out of scope

- disk constraints;
- zone spreading;
- VM batching;
- SMT adjustment;
- 80% headroom;
- failover-node addition.

Reference fixtures generated from the original TypeScript sizer live in
`testdata/reference.json`; `testdata/README.md` lists the asserted properties and
the known differences. Focused tests describe the current placement rules,
limits, immutability, and concurrent reuse.

## Known limitations

- An `Avoid` split only pulls out the later service. `RunsWith` is not re-resolved
  afterwards, so a remaining chain loses its link to the split service silently.

## Verification

Run from this module:

```sh
go test -race ./...
go vet ./...
go build ./...
```
