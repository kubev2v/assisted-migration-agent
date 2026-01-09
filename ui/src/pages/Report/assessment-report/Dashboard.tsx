import React from 'react';

import {
  Infra,
  InventoryData,
  VMResourceBreakdown,
  VMs,
} from '@generated/index';
import {
  Gallery,
  GalleryItem,
  Grid,
  GridItem,
  PageSection,
} from '@patternfly/react-core';

import { ClustersOverview } from './ClustersOverview';
import { CpuAndMemoryOverview } from './CpuAndMemoryOverview';
import { ErrorTable } from './ErrorTable';
import { HostsOverview } from './HostsOverview';
import { NetworkOverview } from './NetworkOverview';
import { OSDistribution } from './OSDistribution';
import { StorageOverview } from './StorageOverview';
import { VMMigrationStatus } from './VMMigrationStatus';
import { WarningsTable } from './WarningsTable';

import './Dashboard.css';

interface Props {
  infra: Infra;
  cpuCores: VMResourceBreakdown;
  ramGB: VMResourceBreakdown;
  vms: VMs;
  isExportMode?: boolean;
  exportAllViews?: boolean;
  clusters?: { [key: string]: InventoryData };
  isAggregateView?: boolean;
  clusterFound?: boolean;
}

export const Dashboard: React.FC<Props> = ({
  infra,
  cpuCores,
  ramGB,
  vms,
  isExportMode,
  exportAllViews,
  clusters,
  isAggregateView = true,
  clusterFound = true,
}) => {
  // Transform osInfo to include both count and supported fields, fallback to os with supported=true if osInfo is undefined
  const osData = vms.osInfo
    ? Object.entries(vms.osInfo).reduce(
        (acc, [osName, osInfo]) => {
          acc[osName] = {
            count: osInfo.count,
            supported: osInfo.supported,
            upgradeRecommendation: osInfo.upgradeRecommendation,
          };
          return acc;
        },
        {} as {
          [osName: string]: {
            count: number;
            supported: boolean;
            upgradeRecommendation: string;
          };
        },
      )
    : Object.entries(vms.os).reduce(
        (acc, [osName, count]) => {
          acc[osName] = {
            count: count,
            supported: true, // Default to supported when using fallback data
            upgradeRecommendation: '',
          };
          return acc;
        },
        {} as {
          [osName: string]: {
            count: number;
            supported: boolean;
            upgradeRecommendation: string;
          };
        },
      );

  // If a cluster was selected but not found, show a lightweight empty view.
  if (!clusterFound && !isAggregateView) {
    return (
      <PageSection hasBodyWrapper={false}>
        <Grid hasGutter>
          <GridItem span={12}>
            <div style={{ padding: '24px' }}>
              No data is available for the selected cluster.
            </div>
          </GridItem>
        </Grid>
      </PageSection>
    );
  }

  return (
    <PageSection hasBodyWrapper={false}>
      <Grid hasGutter>
        <GridItem span={12} data-export-block={isExportMode ? '2' : undefined}>
          <Gallery hasGutter minWidths={{ default: '40%' }}>
            <GalleryItem>
              <VMMigrationStatus
                data={{
                  migratable: vms.totalMigratable,
                  nonMigratable: vms.total - vms.totalMigratable,
                }}
                isExportMode={isExportMode}
              />
            </GalleryItem>
            <GalleryItem>
              <OSDistribution osData={osData} isExportMode={isExportMode} />
            </GalleryItem>
          </Gallery>
        </GridItem>
        <GridItem span={12} data-export-block={isExportMode ? '3' : undefined}>
          <Gallery hasGutter minWidths={{ default: '40%' }}>
            <GalleryItem>
              <CpuAndMemoryOverview
                isExportMode={isExportMode}
                exportAllViews={exportAllViews}
                cpuTierDistribution={vms.distributionByCpuTier}
                memoryTierDistribution={vms.distributionByMemoryTier}
                memoryTotalGB={ramGB?.total}
                cpuTotalCores={cpuCores?.total}
              />
            </GalleryItem>
            <GalleryItem>
              <StorageOverview
                DiskSizeTierSummary={vms.diskSizeTier}
                isExportMode={isExportMode}
                exportAllViews={exportAllViews}
                diskTypeSummary={vms.diskTypes}
              />
            </GalleryItem>
          </Gallery>
        </GridItem>

        {isAggregateView ? (
          <GridItem
            span={12}
            data-export-block={isExportMode ? '4' : undefined}
          >
            <Gallery hasGutter minWidths={{ default: '300px', md: '45%' }}>
              <GalleryItem>
                <ClustersOverview
                  vmsPerCluster={Object.values(clusters || {}).map(
                    (c) => c.vms?.total ?? 0,
                  )}
                  clustersPerDatacenter={infra.clustersPerDatacenter}
                  isExportMode={isExportMode}
                  exportAllViews={exportAllViews}
                  clusters={clusters}
                />
              </GalleryItem>
              <GalleryItem>
                <HostsOverview
                  hosts={infra.hosts}
                  isExportMode={isExportMode}
                  exportAllViews={exportAllViews}
                />
              </GalleryItem>
            </Gallery>
          </GridItem>
        ) : (
          <GridItem
            span={12}
            data-export-block={isExportMode ? '4' : undefined}
          >
            <Gallery hasGutter minWidths={{ default: '300px', md: '45%' }}>
              <GalleryItem>
                <HostsOverview
                  hosts={infra.hosts}
                  isExportMode={isExportMode}
                  exportAllViews={exportAllViews}
                />
              </GalleryItem>
              <GalleryItem>
                <NetworkOverview
                  infra={infra}
                  nicCount={vms.nicCount}
                  distributionByNicCount={vms.distributionByNicCount}
                  isExportMode={isExportMode}
                  exportAllViews={exportAllViews}
                />
              </GalleryItem>
            </Gallery>
          </GridItem>
        )}
        {isAggregateView && (
          <GridItem
            span={12}
            data-export-block={isExportMode ? '4a' : undefined}
          >
            <Gallery hasGutter minWidths={{ default: '300px', md: '45%' }}>
              <GalleryItem>
                <NetworkOverview
                  infra={infra}
                  nicCount={vms.nicCount}
                  distributionByNicCount={vms.distributionByNicCount}
                  isExportMode={isExportMode}
                  exportAllViews={exportAllViews}
                />
              </GalleryItem>
            </Gallery>
          </GridItem>
        )}
        <GridItem span={12} data-export-block={isExportMode ? '5' : undefined}>
          <Gallery hasGutter minWidths={{ default: '300px', md: '45%' }}>
            <GalleryItem>
              <WarningsTable
                warnings={vms.migrationWarnings}
                isExportMode={isExportMode}
              />
            </GalleryItem>
            <GalleryItem>
              <ErrorTable
                errors={vms.notMigratableReasons}
                isExportMode={isExportMode}
              />
            </GalleryItem>
          </Gallery>
        </GridItem>
      </Grid>
    </PageSection>
  );
};

Dashboard.displayName = 'Dashboard';

export default Dashboard;
