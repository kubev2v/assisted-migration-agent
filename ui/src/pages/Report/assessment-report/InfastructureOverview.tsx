import React from 'react';
import Humanize from 'humanize-plus';

import {
  Infra,
  VMResourceBreakdown,
} from '@generated/index';
import {
  Card,
  CardBody,
  CardTitle,
  Gallery,
  GalleryItem,
} from '@patternfly/react-core';
import ClusterIcon from '@patternfly/react-icons/dist/esm/icons/cluster-icon';

interface Props {
  infra: Infra;
  cpuCores: VMResourceBreakdown;
  ramGB: VMResourceBreakdown;
}

export const InfrastructureOverview: React.FC<Props> = ({
  infra,
  cpuCores,
  ramGB,
}) => (
  <Gallery hasGutter minWidths={{ default: '30%' }}>
    <GalleryItem>
      <Card className="dashboard-card-border" id="hosts">
        <CardTitle>
          <ClusterIcon /> Hosts
        </CardTitle>
        <CardBody>{infra.totalHosts}</CardBody>
      </Card>
    </GalleryItem>
    <GalleryItem>
      <Card className="dashboard-card-border" id="cpu-cores">
        <CardTitle>
          <i className="fas fa-microchip" /> CPU Cores
        </CardTitle>
        <CardBody>{cpuCores.total}</CardBody>
      </Card>
    </GalleryItem>
    <GalleryItem>
      <Card className="dashboard-card-border" id="total-memory">
        <CardTitle>
          <i className="fas fa-memory" /> Total Memory
        </CardTitle>
        <CardBody>{Humanize.fileSize(ramGB.total * 1024 ** 3, 0)}</CardBody>
      </Card>
    </GalleryItem>
  </Gallery>
);
