import { ReactNode } from 'react';
import { Alert, AlertVariant } from '@patternfly/react-core';
import type { ApiError } from '@shared/reducers/collectorSlice';

interface InformationProps {
  error: ApiError | null;
  children: ReactNode;
}

function Information({ error, children }: InformationProps) {
  if (error) {
    const title = error.code ? `Error ${error.code}` : 'Error';
    return (
      <Alert variant={AlertVariant.danger} isInline title={title}>
        {error.message}
      </Alert>
    );
  }

  return <>{children}</>;
}

export default Information;
