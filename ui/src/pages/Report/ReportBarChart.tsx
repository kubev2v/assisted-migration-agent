import React from 'react';

import {
  Chart,
  ChartAxis,
  ChartBar,
  ChartGroup,
  ChartVoronoiContainer,
} from '@patternfly/react-charts';
import { Content } from '@patternfly/react-core';

type ChartBarDataEntry = {
  name: string;
  x: string;
  y: number;
};

function histogramToBarChartData(
  histogram: ReportBarChart.Histogram,
  name: string,
  units: string = '',
): ChartBarDataEntry[] {
  const { minValue, step, data } = histogram;
  return data.map((y, idx) => {
    const lo = step * idx + minValue;
    const hi = lo + step - 1;

    return {
      name,
      x: `${lo}-${hi}${units}`,
      y,
    };
  });
}

function getMax(histogram: ReportBarChart.Histogram): number {
  const [head, ..._] = histogram.data;
  return histogram.data.reduce((prev, next) => Math.max(prev, next), head);
}

// eslint-disable-next-line @typescript-eslint/no-namespace
export namespace ReportBarChart {
  export type Histogram = {
    data: number[];
    minValue: number;
    step: number;
  };

  export type Props = {
    histogram: Histogram;
    title: string;
  };
}

export function ReportBarChart(props: ReportBarChart.Props): React.ReactNode {
  const { title, histogram } = props;

  return (
    <>
      <Content style={{ textAlign: 'center' }}>
        <Content component="p">{title}</Content>
      </Content>
      <Chart
        name={title.toLowerCase().split(' ').join('-')}
        ariaDesc={title + ' chart'}
        ariaTitle={title + ' chart'}
        containerComponent={
          <ChartVoronoiContainer
            responsive
            labels={({ datum }) => `${datum.name}: ${datum.y}`}
            constrainToVisibleArea
          />
        }
        domain={{
          y: [0, getMax(histogram)],
        }}
        domainPadding={{ x: [10, 10] }}
        legendOrientation="horizontal"
        legendPosition="bottom-left"
        padding={75}
        width={600}
        height={250}
      >
        <ChartAxis />
        <ChartAxis dependentAxis showGrid />
        <ChartGroup>
          <ChartBar data={histogramToBarChartData(histogram, 'Count')} />
        </ChartGroup>
      </Chart>
    </>
  );
}
