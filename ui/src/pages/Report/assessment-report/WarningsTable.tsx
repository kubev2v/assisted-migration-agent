import React from 'react';

import { MigrationIssue } from '@generated/index';
import { Card, CardBody, CardTitle, Icon } from '@patternfly/react-core';
import { ExclamationTriangleIcon } from '@patternfly/react-icons';
import { t_global_icon_color_status_warning_default as globalWarningColor100 } from '@patternfly/react-tokens/dist/js/t_global_icon_color_status_warning_default';

import { ReportTable } from '../ReportTable';

interface WarningsTableProps {
  warnings: MigrationIssue[];
  isExportMode?: boolean;
}

export const WarningsTable: React.FC<WarningsTableProps> = ({
  warnings,
  isExportMode = false,
}) => {
  const tableHeight = isExportMode ? 'none !important' : '325px';
  return (
    <Card
      className={isExportMode ? 'dashboard-card-print' : 'dashboard-card'}
      id="warnings-table"
    >
      <CardTitle>
        <Icon style={{ color: globalWarningColor100.value }}>
          <ExclamationTriangleIcon />
        </Icon>{' '}
        Warnings
      </CardTitle>
      <CardBody style={{ padding: 0 }}>
        <div
          style={{
            maxHeight: tableHeight,
            overflowY: 'auto',
            overflowX: 'auto',
            padding: 2,
          }}
        >
          <ReportTable<MigrationIssue>
            data={warnings}
            columns={['Description', 'Total VMs']}
            fields={['assessment', 'count']}
            withoutBorder
          />
        </div>
      </CardBody>
    </Card>
  );
};
