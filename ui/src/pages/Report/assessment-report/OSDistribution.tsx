import React from 'react';

import {
  Card,
  CardBody,
  CardTitle,
  Content,
  ContentVariants,
  Flex,
  FlexItem,
  Icon,
} from '@patternfly/react-core';
import { InfoCircleIcon } from '@patternfly/react-icons';

import MigrationChart from '../MigrationChart';

interface OSDistributionProps {
  osData: {
    [osName: string]: {
      count: number;
      supported: boolean;
      upgradeRecommendation: string;
    };
  };
  isExportMode?: boolean;
}

export const OSDistribution: React.FC<OSDistributionProps> = ({
  osData,
  isExportMode = false,
}) => {
  const hasUpgradeRecommendation = Object.values(osData).some(
    (o) => o.upgradeRecommendation && o.upgradeRecommendation.trim() !== '',
  );
  return (
    <Card
      className={isExportMode ? 'dashboard-card-print' : 'dashboard-card'}
      id="os-distribution"
    >
      <CardTitle>
        <i className="fas fa-database" /> Operating Systems
      </CardTitle>
      <CardBody>
        {hasUpgradeRecommendation ? (
          <Flex
            alignItems={{ default: 'alignItemsCenter' }}
            spaceItems={{ default: 'spaceItemsSm' }}
            style={{ marginBottom: '8px' }}
          >
            <FlexItem>
              <Icon>
                <InfoCircleIcon color="#6a6ec8" />
              </Icon>
            </FlexItem>
            <FlexItem>
              <Content
                component={ContentVariants.p}
                style={{ fontWeight: 500 }}
              >
                OS must be upgraded to be supported
              </Content>
            </FlexItem>
          </Flex>
        ) : null}
        <OSBarChart osData={osData} isExportMode={isExportMode} />
      </CardBody>
    </Card>
  );
};

interface OSBarChartProps {
  osData: {
    [osName: string]: {
      count: number;
      supported: boolean;
      upgradeRecommendation?: string;
    };
  };
  isExportMode?: boolean;
}

export const OSBarChart: React.FC<OSBarChartProps> = ({
  osData,
  isExportMode,
}) => {
  const dataEntries = Object.entries(osData).filter(([os]) => os.trim() !== '');

  const sorted = dataEntries.sort(([, a], [, b]) => b.count - a.count);

  const chartData = sorted.map(([os, osInfo]) => ({
    name: os,
    count: osInfo.count,
    legendCategory: osInfo.supported
      ? 'Supported by Red Hat'
      : 'Not supported by Red Hat',
    infoText: osInfo.upgradeRecommendation,
  }));

  // Define custom colors: green for supported, red for not supported
  const customLegend = {
    'Supported by Red Hat': '#28a745', // Green
    'Not supported by Red Hat': '#d9534f', // Red
  };

  const tableHeight = isExportMode ? 'auto !important' : '350px';
  return (
    <MigrationChart
      data={chartData}
      legend={customLegend}
      maxHeight={tableHeight}
      barHeight={12}
    />
  );
};
