import {
  Infra,
  InventoryData,
  VMResourceBreakdown,
  VMs,
} from '@generated/index';

export type ClusterSelection = 'all' | string;

export type ClusterOption = { id: string; label: string };

export type ClusterViewModel = {
  viewInfra?: Infra;
  viewVms?: VMs;
  viewClusters?: { [key: string]: InventoryData };
  cpuCores?: VMResourceBreakdown;
  ramGB?: VMResourceBreakdown;
  nicCount?: VMResourceBreakdown;
  isAggregateView: boolean;
  selectionId: ClusterSelection;
  selectionLabel: string;
  clusterOptions: ClusterOption[];
  clusterFound: boolean;
};

export const getClusterOptions = (clusters?: {
  [key: string]: InventoryData;
}): ClusterOption[] => {
  const keys = clusters ? Object.keys(clusters) : [];
  return [
    { id: 'all', label: 'All clusters' },
    ...keys.map((key) => ({ id: key, label: key })),
  ];
};

/**
 * Build a view model for the selected cluster.
 *
 * - When "all" is selected, return aggregate data.
 * - When a cluster is selected but data is missing, return an empty view (no infra/vms)
 *   so the UI can show a non-blocking empty state instead of falling back to aggregates.
 * - If the selected cluster no longer exists in the map, fall back to "all".
 */
export const buildClusterViewModel = ({
  infra,
  vms,
  clusters,
  selectedClusterId = 'all',
}: {
  infra?: Infra;
  vms?: VMs;
  clusters?: { [key: string]: InventoryData };
  selectedClusterId?: ClusterSelection;
}): ClusterViewModel => {
  const options = getClusterOptions(clusters);
  const clusterExists =
    selectedClusterId === 'all'
      ? true
      : Boolean(
          clusters &&
          Object.prototype.hasOwnProperty.call(clusters, selectedClusterId),
        );
  const effectiveSelection =
    selectedClusterId === 'all' || clusterExists ? selectedClusterId : 'all';

  if (effectiveSelection === 'all') {
    return {
      viewInfra: infra,
      viewVms: vms,
      cpuCores: vms?.cpuCores,
      ramGB: vms?.ramGB,
      nicCount: vms?.nicCount,
      viewClusters: clusters,
      isAggregateView: true,
      selectionId: 'all',
      selectionLabel: 'All clusters',
      clusterOptions: options,
      clusterFound: true,
    };
  }

  const clusterData = clusters ? clusters[effectiveSelection] : undefined;
  const clusterInfra = clusterData?.infra as Infra | undefined;
  const clusterVms = clusterData?.vms as VMs | undefined;
  const selectionLabel = clusterData ? effectiveSelection : 'Missing cluster';

  return {
    viewInfra: clusterInfra,
    viewVms: clusterVms,
    cpuCores: clusterVms?.cpuCores,
    ramGB: clusterVms?.ramGB,
    nicCount: clusterVms?.nicCount,
    viewClusters: clusterData
      ? { [effectiveSelection]: clusterData }
      : undefined,
    isAggregateView: false,
    selectionId: effectiveSelection,
    selectionLabel,
    clusterOptions: options,
    clusterFound: Boolean(clusterData),
  };
};
