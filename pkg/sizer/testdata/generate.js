#!/usr/bin/env node
// Generates reference.json from the original TypeScript sizer.
//
// Usage:
//   node testdata/generate.js /path/to/sizer/lib/dist
//
// The corpus below is written in the Go schema (see ../models.go). Requests are
// translated to the library's descriptor schema, the library computes the
// sizing, and the result is translated back to the Go response schema.
// Fixtures keep the library commit named in testdata/README.md.

const fs = require("fs");
const path = require("path");

const libPath = process.argv[2];
if (!libPath) {
  console.error("usage: node testdata/generate.js <path-to-cluster-sizer-lib-dist>");
  process.exit(2);
}

const {
  ClusterSizer,
  getNodeKubeletCPURequirements,
  getNodeKubeletMemoryRequirements,
} = require(libPath);

// ---------------------------------------------------------------- corpus

const worker = { name: "worker", cpu: 16, memory: 64, instanceName: "worker", numberOfDisks: 24, label: "worker" };
const small = { name: "small", cpu: 2, memory: 8, instanceName: "small", numberOfDisks: 24, label: "small" };
const large = { name: "large", cpu: 8, memory: 32, instanceName: "large", numberOfDisks: 24, label: "large" };

function workload(name, count, services, allowedMachineSets) {
  return { name, count, allowedMachineSets: allowedMachineSets || [], services };
}

function service(name, requiredCPU, requiredMemory, extra) {
  return Object.assign({ name, requiredCPU, requiredMemory, zones: 0, runsWith: [], avoid: [] }, extra || {});
}

const cases = [
  {
    name: "basic",
    checks: ["nodeCount", "totals", "consumption", "nodes"],
    request: {
      platform: "BareMetal",
      detailed: true,
      machineSets: [worker],
      workloads: [workload("workload", 1, [service("app", 2, 4)], ["worker"])],
    },
  },
  {
    name: "without-details",
    checks: ["nodeCount", "totals", "consumption"],
    request: {
      platform: "BareMetal",
      detailed: false,
      machineSets: [worker],
      workloads: [workload("workload", 1, [service("app", 2, 4)], ["worker"])],
    },
  },
  {
    name: "static-limits",
    checks: ["nodeCount", "totals", "consumption", "nodes"],
    request: {
      platform: "BareMetal",
      detailed: true,
      machineSets: [worker],
      workloads: [workload("workload", 1, [service("app", 2, 4, { limitCPU: 4, limitMemory: 8 })], ["worker"])],
    },
  },
  {
    name: "one-sided-limits",
    checks: ["nodeCount", "totals", "consumption", "nodes"],
    request: {
      platform: "BareMetal",
      detailed: true,
      machineSets: [worker],
      workloads: [workload("workload", 1, [service("app", 2, 4, { limitCPU: 6 })], ["worker"])],
    },
  },
  {
    name: "two-services-one-node",
    checks: ["nodeCount", "totals", "consumption", "nodes"],
    request: {
      platform: "BareMetal",
      detailed: true,
      machineSets: [worker],
      workloads: [
        workload("apps", 1, [
          service("api", 1, 2, { runsWith: ["db"] }),
          service("db", 2, 4),
        ], ["worker"]),
      ],
    },
  },
  {
    name: "count-3",
    checks: ["nodeCount", "totals", "consumption"],
    request: {
      platform: "BareMetal",
      detailed: false,
      machineSets: [large],
      workloads: [workload("workload", 3, [service("app", 1, 2)], ["large"])],
    },
  },
  {
    name: "runs-with-across-workloads",
    checks: ["totals", "consumption"],
    request: {
      platform: "BareMetal",
      detailed: false,
      machineSets: [large],
      workloads: [
        workload("api", 1, [service("api", 1, 2)], ["large"]),
        workload("db", 1, [service("db", 1, 2, { runsWith: ["api"] })], ["large"]),
      ],
    },
  },
  {
    name: "avoid-splits-services",
    checks: ["nodeCount", "totals", "consumption"],
    request: {
      platform: "BareMetal",
      detailed: false,
      machineSets: [large],
      workloads: [
        workload("apps", 1, [
          service("api", 1, 2, { avoid: ["db"] }),
          service("db", 1, 2),
        ], ["large"]),
      ],
    },
  },
  {
    name: "multi-machine-sets",
    checks: ["nodeCount", "totals", "consumption", "nodes"],
    request: {
      platform: "BareMetal",
      detailed: true,
      machineSets: [small, large],
      workloads: [workload("workload", 1, [service("app", 1, 2)], ["small"])],
    },
  },
  {
    name: "control-plane-allowed",
    checks: ["nodeCount", "totals", "consumption", "nodes"],
    request: {
      platform: "BareMetal",
      detailed: true,
      machineSets: [
        large,
        {
          name: "controlPlane",
          isControlPlane: true,
          cpu: 8,
          memory: 32,
          instanceName: "controlPlane",
          numberOfDisks: 24,
          label: "controlPlane",
          allowWorkloadScheduling: true,
        },
      ],
      workloads: [workload("workload", 1, [service("app", 1, 2)], ["controlPlane"])],
    },
  },
  {
    name: "control-plane-denied",
    checks: [],
    note:
      "The original places on a controlPlane machine set even when allowWorkloadScheduling is false, as long as the workload names it; the Go port rejects that placement.",
    request: {
      platform: "BareMetal",
      detailed: false,
      machineSets: [
        large,
        {
          name: "controlPlane",
          isControlPlane: true,
          cpu: 8,
          memory: 32,
          instanceName: "controlPlane",
          numberOfDisks: 24,
          label: "controlPlane",
          allowWorkloadScheduling: false,
        },
      ],
      workloads: [workload("workload", 1, [service("app", 1, 2)], ["controlPlane"])],
    },
  },
  {
    name: "no-fitting-machine-set",
    checks: ["error"],
    request: {
      platform: "BareMetal",
      detailed: false,
      machineSets: [large],
      workloads: [workload("workload", 1, [service("app", 100, 500)], ["large"])],
    },
  },
];

cases.push({
  name: "pack-unrelated-workloads",
  checks: ["nodeCount", "totals", "consumption", "nodes"],
  request: {
    platform: "BareMetal",
    detailed: true,
    machineSets: [large],
    workloads: [
      workload("api", 1, [service("api", 1, 2)], ["large"]),
      workload("db", 1, [service("db", 1, 2)], ["large"]),
    ],
  },
});

cases.push({
  name: "fits-by-memory",
  checks: [],
  note:
    "The original creates the first machine set named in usesMachines without rechecking fit, placing a 20 GB service on an 8 GB node; the Go port picks a machine set that fits.",
  request: {
    platform: "BareMetal",
    detailed: true,
    machineSets: [small, large],
    workloads: [workload("workload", 1, [service("app", 1, 20)], ["small", "large"])],
  },
});

cases.push({
  name: "two-machine-sets-used",
  checks: ["nodeCount", "totals", "consumption", "nodes"],
  request: {
    platform: "BareMetal",
    detailed: true,
    machineSets: [worker, large],
    workloads: [
      workload("api", 1, [service("api", 1, 2)], ["worker"]),
      workload("db", 1, [service("db", 1, 2)], ["large"]),
    ],
  },
});

cases.push({
  name: "limit-below-request",
  checks: ["nodeCount", "totals", "consumption", "nodes"],
  request: {
    platform: "BareMetal",
    detailed: true,
    machineSets: [worker],
    workloads: [workload("workload", 1, [service("app", 2, 4, { limitCPU: 0.5, limitMemory: 1 })], ["worker"])],
  },
});

cases.push({
  name: "control-plane-reserved",
  checks: ["nodeCount", "totals", "consumption", "nodes"],
  request: {
    platform: "BareMetal",
    detailed: true,
    machineSets: [
      {
        name: "controlPlane",
        isControlPlane: true,
        cpu: 8,
        memory: 32,
        instanceName: "controlPlane",
        numberOfDisks: 24,
        label: "controlPlane",
        allowWorkloadScheduling: true,
        controlPlaneReservedResources: { cpu: 0.5, memory: 2.25 },
      },
    ],
    workloads: [workload("workload", 1, [service("app", 1, 2)], ["controlPlane"])],
  },
});

// Limits and ratios exercise the kubelet reservation tiers.
[1, 4, 8, 16, 128, 256].forEach((memory) => {
  cases.push({
    name: `kubelet-memory-${memory}`,
    checks: ["nodeCount", "totals", "consumption", "nodes"],
    request: {
      platform: "BareMetal",
      detailed: true,
      machineSets: [{ name: "worker", cpu: 16, memory, instanceName: "worker", numberOfDisks: 24, label: "worker" }],
      workloads: [workload("workload", 1, [service("app", 1, 1, { limitCPU: 2, limitMemory: 2 })], ["worker"])],
    },
  });
});

cases.push({
  name: "service-zones-ignored",
  checks: [],
  note:
    "The original spreads Service.Zones replicas over separate zones; the Go port ignores Service.Zones and reports a single zone.",
  request: {
    platform: "BareMetal",
    detailed: false,
    machineSets: [large],
    workloads: [workload("workload", 1, [service("app", 1, 2, { zones: 3 })], ["large"])],
  },
});

[1, 2, 4, 8].forEach((cpu) => {
  cases.push({
    name: `kubelet-cpu-${cpu}`,
    checks: ["nodeCount", "totals", "consumption", "nodes"],
    request: {
      platform: "BareMetal",
      detailed: true,
      machineSets: [{ name: "worker", cpu, memory: 64, instanceName: "worker", numberOfDisks: 24, label: "worker" }],
      workloads: [workload("workload", 1, [service("app", 0.5, 1, { limitCPU: 1, limitMemory: 2 })], ["worker"])],
    },
  });
});

cases.push({
  name: "count-3-with-limits",
  checks: ["totals", "consumption"],
  request: {
    platform: "BareMetal",
    detailed: false,
    machineSets: [large],
    workloads: [workload("workload", 3, [service("app", 2, 4, { limitCPU: 4, limitMemory: 8 })], ["large"])],
  },
});

// ---------------------------------------------------------------- translation

// toLibMachineSet converts a machine set from the Go schema to the library schema.
function toLibMachineSet(ms) {
  const out = {
    name: ms.name,
    cpu: ms.cpu,
    memory: ms.memory,
    instanceName: ms.instanceName || ms.name,
    numberOfDisks: ms.numberOfDisks || 24,
    onlyFor: [],
    label: ms.label || ms.name,
  };
  if (ms.allowWorkloadScheduling !== undefined) {
    out.allowWorkloadScheduling = ms.allowWorkloadScheduling;
  }
  if (ms.controlPlaneReservedResources) {
    out.controlPlaneReserved = {
      cpu: ms.controlPlaneReservedResources.cpu,
      memory: ms.controlPlaneReservedResources.memory,
    };
  }
  return out;
}

// toLibWorkload converts a workload from the Go schema to the library schema.
function toLibWorkload(w) {
  return {
    name: w.name,
    count: w.count,
    usesMachines: w.allowedMachineSets || [],
    services: (w.services || []).map((s) => ({
      name: s.name,
      requiredCPU: s.requiredCPU,
      requiredMemory: s.requiredMemory,
      limitCPU: s.limitCPU || undefined,
      limitMemory: s.limitMemory || undefined,
      zones: s.zones || 0,
      runsWith: s.runsWith || [],
      avoid: s.avoid || [],
    })),
  };
}

// buildReference converts a library sizing result to the Go response schema.
function buildReference(sizing) {
  const nodes = sizing.nodes || [];
  const zones = sizing.zoneDetails || [];
  const servicesById = new Map((sizing.services || []).map((s) => [s.id, s]));
  const nodeServices = (node) =>
    (node.services || []).map((id) => servicesById.get(id)).filter(Boolean);

  const reference = {
    nodeCount: nodes.length,
    zones: zones.length,
    totalCPU: nodes.reduce((sum, n) => sum + n.cpuUnits, 0),
    totalMemory: nodes.reduce((sum, n) => sum + n.memory, 0),
    resourceConsumption: { cpu: 0, memory: 0 },
  };

  let limitCPU = 0;
  let limitMemory = 0;
  let hasLimits = false;
  nodes.forEach((node) =>
    nodeServices(node).forEach((s) => {
      reference.resourceConsumption.cpu += s.requiredCPU;
      reference.resourceConsumption.memory += s.requiredMemory;
      limitCPU += s.limitCPU || s.requiredCPU;
      limitMemory += s.limitMemory || s.requiredMemory;
      hasLimits = hasLimits || Boolean(s.limitCPU) || Boolean(s.limitMemory);
    })
  );

  if (hasLimits) {
    const availableCPU = nodes.reduce(
      (sum, n) => sum + (n.cpuUnits - getNodeKubeletCPURequirements(n.cpuUnits)),
      0
    );
    const availableMemory = nodes.reduce(
      (sum, n) => sum + (n.memory - getNodeKubeletMemoryRequirements(n.memory)),
      0
    );
    reference.resourceConsumption.limits = { cpu: limitCPU, memory: limitMemory };
    reference.resourceConsumption.overCommitRatio = {
      cpu: availableCPU > 0 ? limitCPU / availableCPU : 0,
      memory: availableMemory > 0 ? limitMemory / availableMemory : 0,
    };
  }

  reference.advanced = zones.map((zone, index) => ({
    zone: `Zone ${index + 1}`,
    nodes: (zone.nodes || []).map((nodeId) => {
      const node = nodes.find((n) => n.id === nodeId);
      const placed = nodeServices(node);
      const requestedCPU = placed.reduce((sum, s) => sum + s.requiredCPU, 0);
      const requestedMemory = placed.reduce((sum, s) => sum + s.requiredMemory, 0);
      const nodeHasLimits = placed.some((s) => Boolean(s.limitCPU) || Boolean(s.limitMemory));
      const nodeLimits = placed.reduce(
        (acc, s) => {
          acc.cpu += s.limitCPU || s.requiredCPU;
          acc.memory += s.limitMemory || s.requiredMemory;
          return acc;
        },
        { cpu: 0, memory: 0 }
      );
      const cpuAvailable = node.cpuUnits - getNodeKubeletCPURequirements(node.cpuUnits);
      const memoryAvailable = node.memory - getNodeKubeletMemoryRequirements(node.memory);

      return {
        name: `Node ${nodes.indexOf(node) + 1}`,
        machineSetName: node.machineSet,
        isControlPlane: Boolean(node.isControlPlane),
        cpu: {
          requested: requestedCPU,
          total: node.cpuUnits,
          limits: nodeHasLimits ? nodeLimits.cpu : 0,
          overCommitRatio: nodeHasLimits && cpuAvailable > 0 ? nodeLimits.cpu / cpuAvailable : 0,
        },
        memory: {
          requested: requestedMemory,
          total: node.memory,
          limits: nodeHasLimits ? nodeLimits.memory : 0,
          overCommitRatio: nodeHasLimits && memoryAvailable > 0 ? nodeLimits.memory / memoryAvailable : 0,
        },
        services: placed.map((s) => ({
          name: s.name,
          requiredCPU: s.requiredCPU,
          requiredMemory: s.requiredMemory,
          limitCPU: s.limitCPU || 0,
          limitMemory: s.limitMemory || 0,
          zones: s.zones || 0,
        })),
      };
    }),
  }));

  return reference;
}

// ---------------------------------------------------------------- generation

const fixtures = cases.map((entry) => {
  const libWorkloads = entry.request.workloads.map(toLibWorkload);
  const libMachineSets = entry.request.machineSets.map(toLibMachineSet);
  const fixture = { name: entry.name, checks: entry.checks, request: entry.request };
  if (entry.note) {
    fixture.note = entry.note;
  }

  try {
    const sizing = ClusterSizer.size(libWorkloads, entry.request.platform, libMachineSets);
    fixture.response = { success: true, data: buildReference(sizing) };
  } catch (error) {
    fixture.response = { success: false, error: String(error && error.message ? error.message : error) };
  }

  return fixture;
});

const output = path.join(__dirname, "reference.json");
fs.writeFileSync(output, JSON.stringify(fixtures, null, 2) + "\n");
console.log(`wrote ${output} with ${fixtures.length} cases`);
