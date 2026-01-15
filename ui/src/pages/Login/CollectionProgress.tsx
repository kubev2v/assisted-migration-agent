import { Content } from '@patternfly/react-core';

interface CollectionProgressProps {
  percentage: number;
  statusText: string;
}

function CollectionProgress({ percentage, statusText }: CollectionProgressProps) {
  return (
    <Content component="small">
      {percentage}% done. {statusText}
    </Content>
  );
}

export default CollectionProgress;
