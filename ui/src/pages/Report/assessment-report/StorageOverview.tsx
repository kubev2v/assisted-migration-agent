import React, { useMemo, useState } from 'react';

import {
  DiskSizeTierSummary,
  DiskTypeSummary,
} from '@generated/index';
import {
  Chart,
  ChartAxis,
  ChartBar,
  ChartThemeColor,
  ChartTooltip,
  ChartVoronoiContainer,
  getCustomTheme,
} from '@patternfly/react-charts/victory';
import {
  Card,
  CardBody,
  CardTitle,
  Content,
  Dropdown,
  DropdownItem,
  DropdownList,
  Flex,
  FlexItem,
  MenuToggle,
  MenuToggleElement,
} from '@patternfly/react-core';

import MigrationDonutChart from '../MigrationDonutChart';

interface StorageOverviewProps {
  DiskSizeTierSummary: { [key: string]: DiskSizeTierSummary };
  isExportMode?: boolean;
  exportAllViews?: boolean;
  diskTypeSummary?: { [key: string]: DiskTypeSummary };
}

type ViewMode = 'totalSize' | 'vmCount' | 'vmCountByDiskType';

const VIEW_MODE_LABELS: Record<ViewMode, string> = {
  totalSize: 'Total disk size by tier',
  vmCount: 'VM count by disk size tier',
  vmCountByDiskType: 'VM count by disk type',
};

const TIER_CONFIG: Record<
  string,
  { order: number; label: string; legendCategory: string }
> = {
  Easy: { order: 0, label: '0-10 TB', legendCategory: 'Easy' },
  Medium: { order: 1, label: '11-20 TB', legendCategory: 'Medium' },
  Hard: { order: 2, label: '21-50 TB', legendCategory: 'Hard' },
  White: { order: 3, label: '> 50 TB', legendCategory: 'White glove' },
};

type TierChartDatum = {
  name: string;
  count: number;
  countDisplay: string;
  legendCategory: string;
};

function buildTierChartData(
  summary: { [key: string]: DiskSizeTierSummary },
  tierConfig: Record<
    string,
    { order: number; label: string; legendCategory: string }
  >,
  selector: (tier: DiskSizeTierSummary) => {
    count: number;
    countDisplay: string;
  },
): TierChartDatum[] {
  if (!summary) return [];

  const getTierPrefix = (key: string): string | null => {
    for (const prefix of Object.keys(tierConfig)) {
      if (key.startsWith(prefix)) return prefix;
    }
    return null;
  };

  return Object.entries(summary)
    .sort(([keyA], [keyB]) => {
      const prefixA = getTierPrefix(keyA);
      const prefixB = getTierPrefix(keyB);
      const orderA = prefixA ? tierConfig[prefixA].order : 999;
      const orderB = prefixB ? tierConfig[prefixB].order : 999;
      return orderA - orderB;
    })
    .map(([key, tier]) => {
      const prefix = getTierPrefix(key);
      const display = prefix
        ? tierConfig[prefix]
        : { label: key, legendCategory: 'Unknown' };
      const { count, countDisplay } = selector(tier);
      return {
        name: display.label,
        count,
        countDisplay,
        legendCategory: display.legendCategory,
      };
    });
}

export const StorageOverview: React.FC<StorageOverviewProps> = ({
  DiskSizeTierSummary,
  isExportMode = false,
  exportAllViews = false,
  diskTypeSummary,
}) => {
  const [viewMode, setViewMode] = useState<ViewMode>('vmCount');
  const [isDropdownOpen, setIsDropdownOpen] = useState(false);

  const totals = useMemo(() => {
    if (!DiskSizeTierSummary) return { totalSize: 0, totalVMs: 0 };

    return Object.values(DiskSizeTierSummary).reduce(
      (acc, tier) => ({
        totalSize: acc.totalSize + tier.totalSizeTB,
        totalVMs: acc.totalVMs + tier.vmCount,
      }),
      { totalSize: 0, totalVMs: 0 },
    );
  }, [DiskSizeTierSummary]);

  const chartData = useMemo(() => {
    if (!DiskSizeTierSummary) return [];
    return buildTierChartData(DiskSizeTierSummary, TIER_CONFIG, (tier) => {
      const count = viewMode === 'totalSize' ? tier.totalSizeTB : tier.vmCount;
      return {
        count,
        countDisplay: viewMode === 'totalSize' ? `${count} TB` : `${count} VMs`,
      };
    });
  }, [DiskSizeTierSummary, viewMode]);

  const diskTypeChartData = useMemo(() => {
    if (!diskTypeSummary || Object.keys(diskTypeSummary).length === 0)
      return [];
    const preferredOrder = ['VMFS', 'NFS', 'vSAN', 'RDM', 'vVols'];
    const orderIndex = (name: string): number => {
      const idx = preferredOrder.indexOf(name);
      return idx === -1 ? Number.MAX_SAFE_INTEGER : idx;
    };
    return Object.entries(diskTypeSummary)
      .map(([diskTypeName, summary]) => ({
        name: diskTypeName,
        count: summary.vmCount,
        legendCategory: 'Disk types',
      }))
      .sort((a, b) => orderIndex(a.name) - orderIndex(b.name));
  }, [diskTypeSummary]);

  const chartDataForVmCount = useMemo(() => {
    if (!exportAllViews || !DiskSizeTierSummary) return [];
    return buildTierChartData(DiskSizeTierSummary, TIER_CONFIG, (tier) => {
      const count = tier.vmCount;
      return { count, countDisplay: `${count} VMs` };
    });
  }, [exportAllViews, DiskSizeTierSummary]);

  const chartDataForTotalSize = useMemo(() => {
    if (!exportAllViews || !DiskSizeTierSummary) return [];
    return buildTierChartData(DiskSizeTierSummary, TIER_CONFIG, (tier) => {
      const count = tier.totalSizeTB;
      return { count, countDisplay: `${count} TB` };
    });
  }, [exportAllViews, DiskSizeTierSummary]);

  const onDropdownToggle = (): void => {
    setIsDropdownOpen(!isDropdownOpen);
  };

  const maxDiskTypeCount = useMemo(() => {
    if (!diskTypeChartData || diskTypeChartData.length === 0) return 0;
    return diskTypeChartData.reduce(
      (max, item) => (item.count > max ? item.count : max),
      0,
    );
  }, [diskTypeChartData]);

  // Distinct colors per bar, matching the visual style used elsewhere
  const diskTypeBarColors = [
    '#0066cc',
    '#5e40be',
    '#b6a6e9',
    '#b98412',
    '#73C5C5',
  ];

  // Make bar chart width responsive to the number of categories to avoid
  // extreme spacing when there are only a few disk types (e.g., 1-2).
  const barChartWidth = useMemo(() => {
    const itemCount = Math.max(1, diskTypeChartData.length);
    const perItemWidth = 90; // bar + gap
    const minWidth = isExportMode ? 220 : 260;
    const maxWidth = 500;
    const calculated = itemCount * perItemWidth;
    return Math.min(maxWidth, Math.max(minWidth, calculated));
  }, [diskTypeChartData.length, isExportMode]);

  // Slightly thinner bars for very small datasets so they don't look oversized.
  const computedBarWidth = useMemo(() => {
    const count = diskTypeChartData.length;
    return count <= 2 ? 18 : 28;
  }, [diskTypeChartData.length]);

  // Add extra x padding for 1–2 categories so bars are visually centered.
  const domainPaddingX = useMemo<[number, number]>(() => {
    return diskTypeChartData.length <= 2 ? [30, 30] : [12, 12];
  }, [diskTypeChartData.length]);

  const onSelect = (
    _event: React.MouseEvent<Element, MouseEvent> | undefined,
    value: string | number | undefined,
  ): void => {
    if (
      value === 'totalSize' ||
      value === 'vmCount' ||
      value === 'vmCountByDiskType'
    ) {
      setViewMode(value);
    }
    setIsDropdownOpen(false);
  };

  const smallFontTheme = getCustomTheme(ChartThemeColor.multiUnordered, {
    axis: {
      style: {
        tickLabels: { fontSize: 8 },
        axisLabel: { fontSize: 8 },
      },
    },
    legend: {
      style: {
        labels: { fontSize: 8 },
      },
    },
  });

  return (
    <Card
      className={isExportMode ? 'dashboard-card-print' : 'dashboard-card'}
      id="storage-overview"
      style={{ overflow: 'hidden' }}
    >
      <CardTitle>
        <Flex
          justifyContent={{ default: 'justifyContentSpaceBetween' }}
          alignItems={{ default: 'alignItemsCenter' }}
          style={{ width: '100%' }}
        >
          <FlexItem>
            <i className="fas fa-database" /> Disks
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
                  <DropdownItem key="vmCount" value="vmCount">
                    VM count by disk size tier
                  </DropdownItem>
                  <DropdownItem
                    key="vmCountByDiskType"
                    value="vmCountByDiskType"
                  >
                    VM count by disk type
                  </DropdownItem>
                  <DropdownItem key="totalSize" value="totalSize">
                    Total disk size by tier
                  </DropdownItem>
                </DropdownList>
              </Dropdown>
            </FlexItem>
          )}
        </Flex>
      </CardTitle>
      <CardBody>
        {!isExportMode || !exportAllViews ? (
          viewMode === 'vmCountByDiskType' ? (
            <>
              <div
                style={{
                  display: 'flex',
                  justifyContent: 'center',
                }}
              >
                <div style={{ width: `${barChartWidth + 200}px` }}>
                  <Chart
                    ariaTitle="VM count by disk type"
                    ariaDesc="Vertical bar chart of VM counts grouped by disk type"
                    theme={smallFontTheme}
                    containerComponent={
                      <ChartVoronoiContainer
                        responsive
                        labels={({ datum }) =>
                          `${datum.x}: ${Number(datum.y)} VMs`
                        }
                        constrainToVisibleArea
                        labelComponent={
                          <ChartTooltip style={{ fontSize: 8 }} />
                        }
                      />
                    }
                    domain={{
                      y: [0, maxDiskTypeCount],
                    }}
                    domainPadding={{ x: domainPaddingX }}
                    padding={{ top: 10, bottom: 36, left: 20, right: 20 }}
                    height={isExportMode ? 180 : 220}
                    width={barChartWidth}
                  >
                    <ChartAxis />
                    <ChartAxis
                      tickValues={diskTypeChartData.map((d) => d.name)}
                      tickFormat={(x) => {
                        const item = diskTypeChartData.find(
                          (d) => d.name === x,
                        );
                        return item
                          ? [`${item.name}`, `(${item.count} VMs)`]
                          : x;
                      }}
                      style={{
                        axis: { stroke: 'none' },
                        tickLabels: { fontSize: 8 },
                      }}
                    />
                    <ChartBar
                      barWidth={computedBarWidth}
                      data={diskTypeChartData.map((d) => ({
                        x: d.name,
                        y: d.count,
                      }))}
                      style={{
                        data: {
                          fill: ({ index }: { index: number }) =>
                            diskTypeBarColors[index % diskTypeBarColors.length],
                        },
                        labels: { fontSize: 8 },
                      }}
                    />
                  </Chart>
                </div>
              </div>
              {!isExportMode && (
                <Content
                  component="small"
                  style={{
                    color: '#6a6e73',
                    marginLeft: '20px',
                  }}
                >
                  Totals may exceed the unique VM count because individual VMs
                  can have multiple disk types
                </Content>
              )}
            </>
          ) : (
            <MigrationDonutChart
              data={chartData}
              height={300}
              width={420}
              donutThickness={9}
              titleFontSize={34}
              title={
                viewMode === 'totalSize'
                  ? `${totals.totalSize.toFixed(2)} TB`
                  : `${totals.totalVMs} VMs`
              }
              subTitle={
                viewMode === 'totalSize'
                  ? `${totals.totalVMs} VMs`
                  : `${totals.totalSize.toFixed(2)} TB`
              }
              subTitleColor="#9a9da0"
              itemsPerRow={Math.ceil(chartData.length / 2)}
              labelFontSize={18}
              marginLeft={viewMode === 'totalSize' ? '42%' : '52%'}
              tooltipLabelFormatter={({ datum, percent }) =>
                `${datum.countDisplay}\n${percent.toFixed(1)}%`
              }
            />
          )
        ) : (
          <>
            <div style={{ marginBottom: '24px' }}>
              <div style={{ fontWeight: 600, marginBottom: 8 }}>
                {VIEW_MODE_LABELS['vmCountByDiskType']}
              </div>
              <div
                style={{
                  display: 'flex',
                  justifyContent: 'center',
                }}
              >
                <div style={{ width: `${barChartWidth + 200}px` }}>
                  <Chart
                    ariaTitle="VM count by disk type"
                    ariaDesc="Vertical bar chart of VM counts grouped by disk type"
                    theme={smallFontTheme}
                    containerComponent={
                      <ChartVoronoiContainer
                        responsive
                        labels={({ datum }) =>
                          `${datum.x}: ${Number(datum.y)} VMs`
                        }
                        constrainToVisibleArea
                        labelComponent={
                          <ChartTooltip style={{ fontSize: 8 }} />
                        }
                      />
                    }
                    domain={{
                      y: [0, maxDiskTypeCount],
                    }}
                    domainPadding={{ x: domainPaddingX }}
                    padding={{ top: 10, bottom: 36, left: 20, right: 20 }}
                    height={180}
                    width={barChartWidth}
                  >
                    <ChartAxis />
                    <ChartAxis
                      tickValues={diskTypeChartData.map((d) => d.name)}
                      tickFormat={(x) => {
                        const item = diskTypeChartData.find(
                          (d) => d.name === x,
                        );
                        return item
                          ? [`${item.name}`, `(${item.count} VMs)`]
                          : x;
                      }}
                      style={{
                        axis: { stroke: 'none' },
                        tickLabels: { fontSize: 8 },
                      }}
                    />
                    <ChartBar
                      barWidth={computedBarWidth}
                      data={diskTypeChartData.map((d) => ({
                        x: d.name,
                        y: d.count,
                      }))}
                      style={{
                        data: {
                          fill: ({ index }: { index: number }) =>
                            diskTypeBarColors[index % diskTypeBarColors.length],
                        },
                        labels: { fontSize: 8 },
                      }}
                    />
                  </Chart>
                </div>
              </div>
            </div>
            <div style={{ marginBottom: '24px' }}>
              <div style={{ fontWeight: 600, marginBottom: 8 }}>
                {VIEW_MODE_LABELS['vmCount']}
              </div>
              <MigrationDonutChart
                data={chartDataForVmCount}
                height={300}
                width={420}
                donutThickness={9}
                titleFontSize={34}
                title={`${totals.totalVMs} VMs`}
                subTitle={`${totals.totalSize.toFixed(2)} TB`}
                subTitleColor="#9a9da0"
                itemsPerRow={Math.ceil(chartDataForVmCount.length / 2)}
                labelFontSize={18}
                marginLeft="52%"
                tooltipLabelFormatter={({ datum, percent }) =>
                  `${datum.countDisplay}\n${percent.toFixed(1)}%`
                }
              />
            </div>
            <div>
              <div style={{ fontWeight: 600, marginBottom: 8 }}>
                {VIEW_MODE_LABELS['totalSize']}
              </div>
              <MigrationDonutChart
                data={chartDataForTotalSize}
                height={300}
                width={420}
                donutThickness={9}
                titleFontSize={34}
                title={`${totals.totalSize.toFixed(2)} TB`}
                subTitle={`${totals.totalVMs} VMs`}
                subTitleColor="#9a9da0"
                itemsPerRow={Math.ceil(chartDataForTotalSize.length / 2)}
                labelFontSize={18}
                marginLeft="42%"
                tooltipLabelFormatter={({ datum, percent }) =>
                  `${datum.countDisplay}\n${percent.toFixed(1)}%`
                }
              />
            </div>
          </>
        )}
      </CardBody>
    </Card>
  );
};
