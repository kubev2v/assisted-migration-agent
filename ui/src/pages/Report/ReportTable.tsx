import React from 'react';

import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table';

// eslint-disable-next-line @typescript-eslint/no-namespace
export namespace ReportTable {
  export type Props<DataList extends Array<unknown>> = {
    columns: string[];
    data: DataList;
    fields: Array<keyof DataList[0]>;
    style?: React.CSSProperties;
    withoutBorder?: boolean;
    caption?: string;
  };
}

export function ReportTable<DataItem>(
  props: ReportTable.Props<DataItem[]>,
  withoutBorder = false,
): React.ReactNode {
  const { columns, data, fields, style, caption } = props;

  return (
    <Table
      variant="compact"
      borders={true}
      style={{
        border: withoutBorder ? 'none' : '1px solid lightgray',
        borderRight: 'none',
        ...style,
      }}
    >
      {caption && (
        <caption
          style={{
            fontWeight: 'bold',
            fontSize: '14px',
            textAlign: 'left',
            padding: '8px 16px',
            color: '#151515',
          }}
        >
          {caption}
        </caption>
      )}
      <Thead>
        <Tr style={{ border: withoutBorder ? 'none' : '1px solid lightgray' }}>
          {columns.map((name, index) => (
            <Th
              key={index}
              hasRightBorder={!withoutBorder}
              style={{
                whiteSpace: 'normal',
                wordBreak: 'break-word',
                border: withoutBorder ? 'none' : '1px solid lightgray',
              }}
            >
              {name}
            </Th>
          ))}
        </Tr>
      </Thead>
      <Tbody>
        {data.map((item, idx) => (
          <Tr
            key={idx}
            style={{
              width: 100,
              border: withoutBorder ? 'none' : '1px solid lightgray',
            }}
          >
            {fields.map((f, fieldIdx) => (
              <Td key={fieldIdx} hasRightBorder={!withoutBorder}>
                {' '}
                {item[f] === '' || item[f] === undefined
                  ? '-'
                  : typeof item[f] === 'boolean'
                    ? item[f]
                      ? 'True'
                      : 'False'
                    : (item[f] as React.ReactNode)}
              </Td>
            ))}
          </Tr>
        ))}
      </Tbody>
    </Table>
  );
}

ReportTable.displayName = 'ReportTable';
