import React, { useMemo } from 'react';

import { Host } from '@generated/index';
import {
  Card,
  CardBody,
  CardTitle,
  Flex,
  FlexItem,
} from '@patternfly/react-core';

import MigrationDonutChart from '../MigrationDonutChart';

type HostLike = {
  model?: unknown;
};

interface HostsOverviewProps {
  hosts?: Array<Host>;
  isExportMode?: boolean;
  exportAllViews?: boolean;
}

// Keep the same extended palette style used by other overview widgets
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

export const HostsOverview: React.FC<HostsOverviewProps> = ({
  hosts,
  isExportMode = false,
}) => {
  const { slices, legend, totalHosts } = useMemo(() => {
    const asArray: HostLike[] = Array.isArray(hosts)
      ? (hosts as HostLike[])
      : [];

    // Build counts per model, defaulting to "Unknown model" if not available
    const countsMap = asArray.reduce(
      (acc, h) => {
        const raw =
          typeof h?.model === 'string'
            ? h.model
            : typeof h?.model === 'number'
              ? String(h.model)
              : '';
        const name = raw && raw.trim() !== '' ? raw.trim() : 'Unknown model';
        acc[name] = (acc[name] || 0) + 1;
        return acc;
      },
      {} as Record<string, number>,
    );

    const entries = Object.entries(countsMap)
      .map(([model, count]) => ({ model, count }))
      .sort((a, b) => b.count - a.count);

    const totalHosts = asArray.length;
    const TOP_N = 5;
    const top = entries.slice(0, TOP_N);
    const rest = entries.slice(TOP_N);
    const restSum = rest.reduce((acc, e) => acc + e.count, 0);

    const slices = top.map((e) => ({
      name: e.model,
      count: e.count,
      countDisplay: `${e.count} hosts`,
      legendCategory: e.model,
    }));
    if (restSum > 0) {
      slices.push({
        name: 'Other models',
        count: restSum,
        countDisplay: `${restSum} hosts`,
        legendCategory: 'Other models',
      });
    }

    const legendCategories = slices.map((s) => s.legendCategory);
    const legendMap: Record<string, string> = {};
    legendCategories.forEach((cat, idx) => {
      legendMap[cat] = colorPalette[idx % colorPalette.length];
    });

    return { slices, legend: legendMap, totalHosts };
  }, [hosts]);

  return (
    <Card
      className={isExportMode ? 'dashboard-card-print' : 'dashboard-card'}
      id="hosts-overview"
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
                <i className="fas fa-server" /> Host distribution by model
              </div>
              {!isExportMode && (
                <div style={{ color: '#6a6e73', fontSize: '0.85rem' }}>
                  Top 5 models
                </div>
              )}
            </div>
          </FlexItem>
        </Flex>
      </CardTitle>
      <CardBody>
        <MigrationDonutChart
          data={slices}
          height={300}
          width={420}
          donutThickness={9}
          titleFontSize={34}
          legend={legend}
          legendWidth={680}
          title={`${totalHosts}`}
          subTitle="Hosts"
          subTitleColor="#9a9da0"
          itemsPerRow={2}
          labelFontSize={16}
          marginLeft="0%"
          tooltipLabelFormatter={({ datum, percent }) =>
            `${datum.countDisplay}\n${percent.toFixed(1)}%`
          }
        />
      </CardBody>
    </Card>
  );
};

HostsOverview.displayName = 'HostsOverview';

export default HostsOverview;
