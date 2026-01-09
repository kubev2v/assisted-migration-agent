import React from 'react';

import { Datastore } from '@generated/index';
import {
  Card,
  CardBody,
  CardTitle,
  Icon,
  Progress,
} from '@patternfly/react-core';
import {
  CheckCircleIcon,
  ExclamationCircleIcon,
} from '@patternfly/react-icons';
import { t_global_color_status_success_default as globalSuccessColor100 } from '@patternfly/react-tokens/dist/js/t_global_color_status_success_default';
import { t_global_icon_color_status_danger_default as globalDangerColor100 } from '@patternfly/react-tokens/dist/js/t_global_icon_color_status_danger_default';

import { ReportTable } from '../ReportTable';

interface DatastoresProps {
  datastores: Datastore[];
  isExportMode: boolean;
}

export const Datastores: React.FC<DatastoresProps> = ({
  datastores,
  isExportMode = false,
}) => {
  const tableWidth = '100%';
  const tableHeight = isExportMode ? '100%' : '200px';
  return (
    <Card className={isExportMode ? 'dashboard-card-print' : 'dashboard-card'}>
      <CardTitle>
        <i className="fas fa-database" /> Datastores
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
          <ReportTable<
            Datastore & {
              usage: JSX.Element;
              hardwareAcceleratedMoveDisplay: JSX.Element;
            }
          >
            data={datastores.map((ds) => ({
              ...ds,
              usage: (
                <div style={{ minWidth: '10rem', flexGrow: 1 }}>
                  <Progress
                    value={(ds.freeCapacityGB / ds.totalCapacityGB) * 100}
                    size="sm"
                    aria-label="Disk usage"
                  />
                </div>
              ),
              hardwareAcceleratedMoveDisplay: ds.hardwareAcceleratedMove ? (
                <Icon size="md" isInline>
                  <CheckCircleIcon color={globalSuccessColor100.value} />
                </Icon>
              ) : (
                <Icon size="md" isInline>
                  <ExclamationCircleIcon color={globalDangerColor100.value} />
                </Icon>
              ),
            }))}
            columns={[
              'Type',
              'Vendor',
              'Protocol type',
              'Model',
              'Total',
              'Usage %',
            ]}
            fields={[
              'type',
              'vendor',
              'protocolType',
              'model',
              'totalCapacityGB',
              'usage',
            ]}
            style={{ width: tableWidth }}
            withoutBorder
          />
        </div>
      </CardBody>
    </Card>
  );
};
