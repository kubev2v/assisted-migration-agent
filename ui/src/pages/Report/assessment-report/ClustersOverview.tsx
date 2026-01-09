import React, { useMemo, useState } from 'react';

import { InventoryData } from '@generated/index';
import {
  Card,
  CardBody,
  CardTitle,
  Dropdown,
  DropdownItem,
  DropdownList,
  Flex,
  FlexItem,
  MenuToggle,
  MenuToggleElement,
} from '@patternfly/react-core';

import MigrationDonutChart from '../MigrationDonutChart';

import './ClustersOverview.css';

interface ClustersOverviewProps {
  vmsPerCluster: number[];
  clustersPerDatacenter: number[];
  isExportMode?: boolean;
  exportAllViews?: boolean;
  clusters?: { [key: string]: InventoryData };
}

type ViewMode = 'dataCenterDistribution' | 'vmByCluster' | 'cpuOverCommitment';

const VIEW_MODE_LABELS: Record<ViewMode, string> = {
  dataCenterDistribution: 'Cluster distribution by data center',
  vmByCluster: 'VM distribution by cluster',
  cpuOverCommitment: 'Cluster CPU over commitment',
};

// Extended palette to avoid repeating colors when we have >4 slices
const colorPalette = [
  '#0066cc',
  '#5e40be',
  '#b6a6e9',
  '#73c5c5',
  '#b98412',
  '#28a745',
  '#f0ad4e',
  '#d9534f',
  '#009596',
  '#6a6e73',
];

export const ClustersOverview: React.FC<ClustersOverviewProps> = ({
  vmsPerCluster,
  clustersPerDatacenter,
  isExportMode = false,
  exportAllViews = false,
  clusters,
}) => {
  const [viewMode, setViewMode] = useState<ViewMode>('vmByCluster');
  const [isDropdownOpen, setIsDropdownOpen] = useState(false);

  const { chartData, legend, title, subTitle } = useMemo(() => {
    const buildLegend = (categories: string[]): Record<string, string> => {
      const legendMap: Record<string, string> = {};
      categories.forEach((cat, idx) => {
        legendMap[cat] = colorPalette[idx % colorPalette.length];
      });
      return legendMap;
    };

    if (viewMode === 'cpuOverCommitment') {
      const entries = clusters ? Object.entries(clusters) : [];

      const parseCpuOverCommitment = (raw: unknown): number => {
        if (raw == null) return NaN;
        if (typeof raw === 'number') return raw;
        if (typeof raw === 'string') {
          const match = raw.match(/^\s*\d+\s*:\s*(\d+(?:\.\d+)?)\s*$/);
          if (match) return Number(match[1]);
          const asNum = Number(raw);
          return Number.isFinite(asNum) ? asNum : NaN;
        }
        return NaN;
      };

      const withValue = entries
        .map(([clusterName, data]) => {
          const raw = (
            (data as InventoryData)?.infra as unknown as {
              cpuOverCommitment?: unknown;
            }
          )?.cpuOverCommitment;
          const value = parseCpuOverCommitment(raw);
          return { clusterName, value };
        })
        .filter((item) => Number.isFinite(item.value) && item.value >= 0)
        .sort((a, b) => b.value - a.value);

      const TOP_N = 5;
      const top = withValue.slice(0, TOP_N);

      const formatRatio = (r: number): string => {
        const formatted = r % 1 === 0 ? r.toFixed(0) : r.toFixed(2);
        return `${formatted.replace(/(?:\.0+|(\.\d*[1-9]))0+$/, '$1')}`;
      };

      const slices = top.map((item, idx) => ({
        name: formatRatio(item.value),
        count: item.value,
        countDisplay: formatRatio(item.value),
        legendCategory: `Cluster ${idx + 1}`,
      }));
      const legendCategories = slices.map((s) => s.legendCategory);
      return {
        chartData: slices,
        legend: buildLegend(legendCategories),
        title: '',
        subTitle: '',
      };
    }

    if (viewMode === 'vmByCluster') {
      const counts = Array.isArray(vmsPerCluster) ? [...vmsPerCluster] : [];
      const totalVMs = counts.reduce((acc, n) => acc + n, 0);
      const TOP_N = 4;
      const ranked = counts
        .map((count, index) => ({ count, index }))
        .sort((a, b) => b.count - a.count);
      const top = ranked.slice(0, TOP_N);
      const rest = ranked.slice(TOP_N);
      const restSum = rest.reduce((acc, r) => acc + r.count, 0);

      const slices = top.map((item, i) => {
        const name = `Cluster ${i + 1}`;
        return {
          name,
          count: item.count,
          countDisplay: `${item.count} VMs`,
          legendCategory: name,
        };
      });
      if (restSum > 0) {
        slices.push({
          name: 'Rest of clusters',
          count: restSum,
          countDisplay: `${restSum} VMs`,
          legendCategory: 'Rest of clusters',
        });
      }

      const legendCategories = slices.map((s) => s.legendCategory);
      return {
        chartData: slices,
        legend: buildLegend(legendCategories),
        title: `${totalVMs}`,
        subTitle: 'VMs',
      };
    }

    const counts = Array.isArray(clustersPerDatacenter)
      ? [...clustersPerDatacenter]
      : [];
    const totalClusters = counts.reduce((acc, n) => acc + n, 0);

    const TOP_N = 4;
    const ranked = counts
      .map((count, index) => ({ count, index }))
      .sort((a, b) => b.count - a.count);
    const top = ranked.slice(0, TOP_N);
    const rest = ranked.slice(TOP_N);
    const restSum = rest.reduce((acc, r) => acc + r.count, 0);

    const slices = top.map((item, i) => {
      const name = `Data center ${i + 1}`;
      return {
        name,
        count: item.count,
        countDisplay: `${item.count} clusters`,
        legendCategory: name,
      };
    });
    if (restSum > 0) {
      slices.push({
        name: 'Rest of datacenters',
        count: restSum,
        countDisplay: `${restSum} clusters`,
        legendCategory: 'Rest of datacenters',
      });
    }

    const legendCategories = slices.map((s) => s.legendCategory);
    return {
      chartData: slices,
      legend: buildLegend(legendCategories),
      title: `${totalClusters}`,
      subTitle: 'Clusters',
    };
  }, [viewMode, vmsPerCluster, clustersPerDatacenter, clusters]);

  const vmByClusterData = useMemo(() => {
    if (!exportAllViews) return null;
    const buildLegend = (categories: string[]): Record<string, string> => {
      const legendMap: Record<string, string> = {};
      categories.forEach((cat, idx) => {
        legendMap[cat] = colorPalette[idx % colorPalette.length];
      });
      return legendMap;
    };
    const counts = Array.isArray(vmsPerCluster) ? [...vmsPerCluster] : [];
    const totalVMs = counts.reduce((acc, n) => acc + n, 0);
    const TOP_N = 4;
    const ranked = counts
      .map((count, index) => ({ count, index }))
      .sort((a, b) => b.count - a.count);
    const top = ranked.slice(0, TOP_N);
    const rest = ranked.slice(TOP_N);
    const restSum = rest.reduce((acc, r) => acc + r.count, 0);
    const slices = top.map((item, i) => {
      const name = `Cluster ${i + 1}`;
      return {
        name,
        count: item.count,
        countDisplay: `${item.count} VMs`,
        legendCategory: name,
      };
    });
    if (restSum > 0) {
      slices.push({
        name: 'Rest of clusters',
        count: restSum,
        countDisplay: `${restSum} VMs`,
        legendCategory: 'Rest of clusters',
      });
    }
    const legendCategories = slices.map((s) => s.legendCategory);
    return {
      chartData: slices,
      legend: buildLegend(legendCategories),
      title: `${totalVMs}`,
      subTitle: 'VMs',
    };
  }, [exportAllViews, vmsPerCluster]);

  const dataCenterDistributionData = useMemo(() => {
    if (!exportAllViews) return null;
    const buildLegend = (categories: string[]): Record<string, string> => {
      const legendMap: Record<string, string> = {};
      categories.forEach((cat, idx) => {
        legendMap[cat] = colorPalette[idx % colorPalette.length];
      });
      return legendMap;
    };
    const counts = Array.isArray(clustersPerDatacenter)
      ? [...clustersPerDatacenter]
      : [];
    const totalClusters = counts.reduce((acc, n) => acc + n, 0);
    const TOP_N = 4;
    const ranked = counts
      .map((count, index) => ({ count, index }))
      .sort((a, b) => b.count - a.count);
    const top = ranked.slice(0, TOP_N);
    const rest = ranked.slice(TOP_N);
    const restSum = rest.reduce((acc, r) => acc + r.count, 0);
    const slices = top.map((item, i) => {
      const name = `Data center ${i + 1}`;
      return {
        name,
        count: item.count,
        countDisplay: `${item.count} clusters`,
        legendCategory: name,
      };
    });
    if (restSum > 0) {
      slices.push({
        name: 'Rest of datacenters',
        count: restSum,
        countDisplay: `${restSum} clusters`,
        legendCategory: 'Rest of datacenters',
      });
    }
    const legendCategories = slices.map((s) => s.legendCategory);
    return {
      chartData: slices,
      legend: buildLegend(legendCategories),
      title: `${totalClusters}`,
      subTitle: 'Clusters',
    };
  }, [exportAllViews, clustersPerDatacenter]);

  const cpuOverCommitmentData = useMemo(() => {
    if (!exportAllViews) return null;
    const entries = clusters ? Object.entries(clusters) : [];
    const parseCpuOverCommitment = (raw: unknown): number => {
      if (raw == null) return NaN;
      if (typeof raw === 'number') return raw;
      if (typeof raw === 'string') {
        const match = raw.match(/^\s*\d+\s*:\s*(\d+(?:\.\d+)?)\s*$/);
        if (match) return Number(match[1]);
        const asNum = Number(raw);
        return Number.isFinite(asNum) ? asNum : NaN;
      }
      return NaN;
    };
    const withValue = entries
      .map(([clusterName, data]) => {
        const raw = (
          (data as InventoryData)?.infra as unknown as {
            cpuOverCommitment?: unknown;
          }
        )?.cpuOverCommitment;
        const value = parseCpuOverCommitment(raw);
        return { clusterName, value };
      })
      .filter((item) => Number.isFinite(item.value) && item.value >= 0)
      .sort((a, b) => b.value - a.value);
    const TOP_N = 5;
    const top = withValue.slice(0, TOP_N);
    const formatRatio = (r: number): string => {
      const formatted = r % 1 === 0 ? r.toFixed(0) : r.toFixed(2);
      return `${formatted.replace(/(?:\.0+|(\.\d*[1-9]))0+$/, '$1')}`;
    };
    const slices = top.map((item, idx) => ({
      name: formatRatio(item.value),
      count: item.value,
      countDisplay: formatRatio(item.value),
      legendCategory: `Cluster ${idx + 1}`,
    }));
    const legendCategories = slices.map((s) => s.legendCategory);
    const legendMap: Record<string, string> = {};
    legendCategories.forEach((cat, idx) => {
      legendMap[cat] = colorPalette[idx % colorPalette.length];
    });
    return {
      chartData: slices,
      legend: legendMap,
    };
  }, [exportAllViews, clusters]);

  const onDropdownToggle = (): void => {
    setIsDropdownOpen(!isDropdownOpen);
  };

  const onSelect = (
    _event: React.MouseEvent<Element, MouseEvent> | undefined,
    value: string | number | undefined,
  ): void => {
    if (
      value === 'dataCenterDistribution' ||
      value === 'vmByCluster' ||
      value === 'cpuOverCommitment'
    ) {
      setViewMode(value);
    }
    setIsDropdownOpen(false);
  };

  return (
    <Card
      className={isExportMode ? 'dashboard-card-print' : 'dashboard-card'}
      id="clusters-overview"
      data-export-block={isExportMode ? '3.1' : undefined}
      style={{ overflow: 'hidden' }}
    >
      <CardTitle>
        <Flex
          justifyContent={{ default: 'justifyContentSpaceBetween' }}
          alignItems={{ default: 'alignItemsCenter' }}
          style={{ width: '100%' }}
        >
          <FlexItem>
            <div>
              <div>
                <i className="fas fa-database" /> Clusters
              </div>
              {!isExportMode && (
                <div style={{ color: '#6a6e73', fontSize: '0.85rem' }}>
                  {viewMode === 'dataCenterDistribution'
                    ? 'Top 5 datacenters'
                    : 'Top 5 clusters'}
                </div>
              )}
            </div>
          </FlexItem>
          {!isExportMode && (
            <FlexItem>
              <Dropdown
                isOpen={isDropdownOpen}
                onSelect={onSelect}
                onOpenChange={(isOpen: boolean) => setIsDropdownOpen(isOpen)}
                toggle={(toggleRef: React.Ref<MenuToggleElement>) => (
                  <MenuToggle
                    ref={toggleRef}
                    onClick={onDropdownToggle}
                    isExpanded={isDropdownOpen}
                    style={{ minWidth: '250px' }}
                  >
                    {VIEW_MODE_LABELS[viewMode]}
                  </MenuToggle>
                )}
              >
                <DropdownList>
                  <DropdownItem key="vmByCluster" value="vmByCluster">
                    VM distribution by cluster
                  </DropdownItem>
                  <DropdownItem
                    key="cpuOverCommitment"
                    value="cpuOverCommitment"
                  >
                    Cluster CPU over commitment
                  </DropdownItem>
                  <DropdownItem
                    key="dataCenterDistribution"
                    value="dataCenterDistribution"
                  >
                    Cluster distribution by data center
                  </DropdownItem>
                </DropdownList>
              </Dropdown>
            </FlexItem>
          )}
        </Flex>
      </CardTitle>
      <CardBody>
        {isExportMode && exportAllViews ? (
          <>
            {vmByClusterData && (
              <div style={{ marginBottom: '24px' }}>
                <div style={{ fontWeight: 600, marginBottom: 8 }}>
                  {VIEW_MODE_LABELS['vmByCluster']}
                </div>
                <MigrationDonutChart
                  data={vmByClusterData.chartData}
                  height={300}
                  width={420}
                  donutThickness={9}
                  titleFontSize={34}
                  legend={vmByClusterData.legend}
                  title={vmByClusterData.title}
                  subTitle={vmByClusterData.subTitle}
                  subTitleColor="#9a9da0"
                  itemsPerRow={Math.ceil(vmByClusterData.chartData.length / 2)}
                  labelFontSize={18}
                  marginLeft="12%"
                  tooltipLabelFormatter={({ datum, percent }) =>
                    `${datum.countDisplay}\n${percent.toFixed(1)}%`
                  }
                />
              </div>
            )}
            {dataCenterDistributionData && (
              <div>
                <div style={{ fontWeight: 600, marginBottom: 8 }}>
                  {VIEW_MODE_LABELS['dataCenterDistribution']}
                </div>
                <MigrationDonutChart
                  data={dataCenterDistributionData.chartData}
                  height={300}
                  width={420}
                  donutThickness={9}
                  titleFontSize={34}
                  legend={dataCenterDistributionData.legend}
                  title={dataCenterDistributionData.title}
                  subTitle={dataCenterDistributionData.subTitle}
                  subTitleColor="#9a9da0"
                  itemsPerRow={Math.ceil(
                    dataCenterDistributionData.chartData.length / 2,
                  )}
                  labelFontSize={17}
                  marginLeft="0%"
                  tooltipLabelFormatter={({ datum, percent }) =>
                    `${datum.countDisplay}\n${percent.toFixed(1)}%`
                  }
                />
              </div>
            )}
            {cpuOverCommitmentData && (
              <div style={{ marginTop: '24px' }}>
                <div style={{ fontWeight: 600, marginBottom: 8 }}>
                  {VIEW_MODE_LABELS['cpuOverCommitment']}
                </div>
                {cpuOverCommitmentData.chartData.length === 0 ? (
                  <div style={{ color: '#6a6e73', textAlign: 'center' }}>
                    This inventory has no cpuOverCommitment information.
                  </div>
                ) : (
                  <>
                    <div className="cpu-overcommit__boxes">
                      {cpuOverCommitmentData.chartData.map((item, idx) => (
                        <div
                          key={`cpu-box-export-${idx}`}
                          className="cpu-overcommit__box"
                          style={{
                            background:
                              cpuOverCommitmentData.legend[item.legendCategory],
                          }}
                        >
                          {item.countDisplay}
                        </div>
                      ))}
                    </div>
                    <div className="cpu-overcommit__legend">
                      {cpuOverCommitmentData.chartData.map((item, idx) => (
                        <div
                          key={`cpu-legend-export-${idx}`}
                          className="cpu-overcommit__legend-item"
                        >
                          <span
                            className="cpu-overcommit__legend-swatch"
                            style={{
                              background:
                                cpuOverCommitmentData.legend[
                                  item.legendCategory
                                ],
                            }}
                          />
                          <span className="cpu-overcommit__legend-text">
                            {item.legendCategory} ({item.countDisplay})
                          </span>
                        </div>
                      ))}
                    </div>
                  </>
                )}
              </div>
            )}
          </>
        ) : viewMode === 'cpuOverCommitment' ? (
          <>
            {chartData.length === 0 ? (
              <div style={{ color: '#6a6e73', textAlign: 'center' }}>
                This inventory has no cpuOverCommitment information.
              </div>
            ) : (
              <>
                <div className="cpu-overcommit__boxes">
                  {chartData.map((item, idx) => (
                    <div
                      key={`cpu-box-${idx}`}
                      className="cpu-overcommit__box"
                      style={{ background: legend[item.legendCategory] }}
                    >
                      {item.countDisplay}
                    </div>
                  ))}
                </div>
                <div className="cpu-overcommit__legend">
                  {chartData.map((item, idx) => (
                    <div
                      key={`cpu-legend-${idx}`}
                      className="cpu-overcommit__legend-item"
                    >
                      <span
                        className="cpu-overcommit__legend-swatch"
                        style={{ background: legend[item.legendCategory] }}
                      />
                      <span className="cpu-overcommit__legend-text">
                        {item.legendCategory} ({item.countDisplay})
                      </span>
                    </div>
                  ))}
                </div>
              </>
            )}
          </>
        ) : (
          <MigrationDonutChart
            data={chartData}
            height={300}
            width={420}
            donutThickness={9}
            titleFontSize={34}
            legend={legend}
            title={title}
            subTitle={subTitle}
            subTitleColor="#9a9da0"
            itemsPerRow={Math.ceil(chartData.length / 2)}
            labelFontSize={viewMode === 'vmByCluster' ? 18 : 17}
            marginLeft={viewMode === 'vmByCluster' ? '12%' : '0%'}
            tooltipLabelFormatter={({ datum, percent }) =>
              `${datum.countDisplay}\n${percent.toFixed(1)}%`
            }
          />
        )}
      </CardBody>
    </Card>
  );
};
