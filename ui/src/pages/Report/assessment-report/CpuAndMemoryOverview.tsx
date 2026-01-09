import React, { useMemo, useState } from 'react';

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

interface CpuAndMemoryOverviewProps {
  cpuTierDistribution?: Record<string, number>;
  memoryTierDistribution?: Record<string, number>;
  memoryTotalGB?: number;
  cpuTotalCores?: number;
  isExportMode?: boolean;
  exportAllViews?: boolean;
}

type ViewMode = 'memoryTiers' | 'vcpuTiers';

const VIEW_MODE_LABELS: Record<ViewMode, string> = {
  memoryTiers: 'VM distribution by memory size tier',
  vcpuTiers: 'VM distribution by vCPU count tier',
};

// Extended palette to avoid repeating colors when we have many slices
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

type DonutSlice = {
  name: string;
  count: number;
  countDisplay: string;
  legendCategory: string;
};

function buildLegend(categories: string[]): Record<string, string> {
  const legendMap: Record<string, string> = {};
  categories.forEach((cat, idx) => {
    legendMap[cat] = colorPalette[idx % colorPalette.length];
  });
  return legendMap;
}

function parseDistributionToSlices(
  distribution?: Record<string, number>,
): DonutSlice[] {
  if (!distribution || Object.keys(distribution).length === 0) return [];
  type Parsed = { label: string; min: number; order: number; count: number };
  const parsed: Parsed[] = Object.entries(distribution).map(
    ([label, count]) => {
      const normalized = label.trim().replace(/\s*–\s*/g, '-');
      const range = normalized.match(/^(\d+)\s*-\s*(\d+)$/);
      const plus = normalized.match(/^(\d+)\s*\+$/);
      const min = range ? Number(range[1]) : plus ? Number(plus[1]) : 0;
      const order = Number.isFinite(min) ? min : Number.MAX_SAFE_INTEGER;
      return { label, min, order, count: Number(count ?? 0) };
    },
  );
  const ordered = parsed
    .sort((a, b) => a.order - b.order)
    .filter((p) => p.count > 0);
  return ordered.map((p) => ({
    name: p.label,
    count: p.count,
    countDisplay: `${p.count} VMs`,
    legendCategory: p.label,
  }));
}

export const CpuAndMemoryOverview: React.FC<CpuAndMemoryOverviewProps> = ({
  cpuTierDistribution,
  memoryTierDistribution,
  memoryTotalGB,
  cpuTotalCores,
  isExportMode = false,
  exportAllViews = false,
}) => {
  const [viewMode, setViewMode] = useState<ViewMode>('memoryTiers');
  const [isDropdownOpen, setIsDropdownOpen] = useState(false);

  const memorySlices = useMemo(() => {
    const base = parseDistributionToSlices(memoryTierDistribution);
    return base.map((s) => ({
      ...s,
      name: /gb$/i.test(s.name.trim()) ? s.name : `${s.name} GB`,
    }));
  }, [memoryTierDistribution]);
  const vcpuSlices = useMemo(() => {
    const base = parseDistributionToSlices(cpuTierDistribution);
    return base.map((s) => ({
      ...s,
      name: /cores?$/i.test(s.name.trim()) ? s.name : `${s.name} cores`,
    }));
  }, [cpuTierDistribution]);

  const activeSlices = viewMode === 'memoryTiers' ? memorySlices : vcpuSlices;
  const legend = useMemo(
    () => buildLegend(activeSlices.map((s) => s.legendCategory)),
    [activeSlices],
  );

  const totals = useMemo(() => {
    const sumCounts = (slices: DonutSlice[]): number =>
      slices.reduce((acc, s) => acc + (Number(s.count) || 0), 0);
    return {
      totalVMs:
        viewMode === 'memoryTiers'
          ? sumCounts(memorySlices)
          : sumCounts(vcpuSlices),
    };
  }, [viewMode, memorySlices, vcpuSlices]);

  const memoryLegend = useMemo(
    () => buildLegend(memorySlices.map((s) => s.legendCategory)),
    [memorySlices],
  );
  const vcpuLegend = useMemo(
    () => buildLegend(vcpuSlices.map((s) => s.legendCategory)),
    [vcpuSlices],
  );

  const onDropdownToggle = (): void => {
    setIsDropdownOpen(!isDropdownOpen);
  };

  const onSelect = (
    _event: React.MouseEvent<Element, MouseEvent> | undefined,
    value: string | number | undefined,
  ): void => {
    if (value === 'memoryTiers' || value === 'vcpuTiers') {
      setViewMode(value);
    }
    setIsDropdownOpen(false);
  };

  return (
    <Card
      className={isExportMode ? 'dashboard-card-print' : 'dashboard-card'}
      id="cpu-memory-overview"
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
                <i className="fas fa-microchip" /> CPU &amp; memory
              </div>
              {!isExportMode && (
                <div style={{ color: '#6a6e73', fontSize: '0.85rem' }}>
                  {viewMode === 'memoryTiers'
                    ? 'Memory size tiers'
                    : 'vCPU count tiers'}
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
                    style={{ minWidth: '290px' }}
                  >
                    {VIEW_MODE_LABELS[viewMode]}
                  </MenuToggle>
                )}
              >
                <DropdownList>
                  <DropdownItem key="memoryTiers" value="memoryTiers">
                    VM distribution by memory size tier
                  </DropdownItem>
                  <DropdownItem key="vcpuTiers" value="vcpuTiers">
                    VM distribution by vCPU count tier
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
            <div style={{ marginBottom: '24px' }}>
              <div style={{ fontWeight: 600, marginBottom: 8 }}>
                {VIEW_MODE_LABELS['memoryTiers']}
              </div>
              <MigrationDonutChart
                data={memorySlices}
                height={300}
                width={420}
                donutThickness={9}
                titleFontSize={34}
                legend={memoryLegend}
                title={`${memorySlices.reduce(
                  (acc, s) => acc + (Number(s.count) || 0),
                  0,
                )} VMs`}
                subTitle={
                  typeof memoryTotalGB === 'number'
                    ? `${memoryTotalGB} GB`
                    : undefined
                }
                subTitleColor="#9a9da0"
                itemsPerRow={Math.ceil(memorySlices.length / 2)}
                labelFontSize={18}
                marginLeft="0%"
                tooltipLabelFormatter={({ datum, percent }) =>
                  `${datum.countDisplay}\n${percent.toFixed(1)}%`
                }
              />
            </div>
            <div>
              <div style={{ fontWeight: 600, marginBottom: 8 }}>
                {VIEW_MODE_LABELS['vcpuTiers']}
              </div>
              <MigrationDonutChart
                data={vcpuSlices}
                height={300}
                width={420}
                donutThickness={9}
                titleFontSize={34}
                legend={vcpuLegend}
                title={`${vcpuSlices.reduce(
                  (acc, s) => acc + (Number(s.count) || 0),
                  0,
                )} VMs`}
                subTitle={
                  typeof cpuTotalCores === 'number'
                    ? `${cpuTotalCores.toLocaleString()} Cores`
                    : undefined
                }
                subTitleColor="#9a9da0"
                itemsPerRow={Math.ceil(vcpuSlices.length / 2)}
                labelFontSize={18}
                marginLeft="52%"
                tooltipLabelFormatter={({ datum, percent }) =>
                  `${datum.countDisplay}\n${percent.toFixed(1)}%`
                }
              />
            </div>
          </>
        ) : (
          <MigrationDonutChart
            data={activeSlices}
            height={300}
            width={420}
            donutThickness={9}
            titleFontSize={34}
            legend={legend}
            title={`${totals.totalVMs} VMs`}
            subTitle={
              viewMode === 'memoryTiers'
                ? typeof memoryTotalGB === 'number'
                  ? `${memoryTotalGB} GB`
                  : undefined
                : typeof cpuTotalCores === 'number'
                  ? `${cpuTotalCores.toLocaleString()} Cores`
                  : undefined
            }
            subTitleColor="#9a9da0"
            itemsPerRow={Math.ceil(activeSlices.length / 2)}
            labelFontSize={18}
            marginLeft="52%"
            tooltipLabelFormatter={({ datum, percent }) =>
              `${datum.countDisplay}\n${percent.toFixed(1)}%`
            }
          />
        )}
      </CardBody>
    </Card>
  );
};

CpuAndMemoryOverview.displayName = 'CpuAndMemoryOverview';

export default CpuAndMemoryOverview;
