import React from 'react';

import { ChartPie } from '@patternfly/react-charts';
import { Content } from '@patternfly/react-core';

type ChartBarDataEntry = {
  name: string;
  x: string;
  y: number;
};

function histogramToPieChartData(
  histogram: ReportPieChart.Histogram,
  legendLabel: string,
): ChartBarDataEntry[] {
  const { data } = histogram;
  return data
    .filter((y) => y > 0) // Filtrar valores mayores que 0
    .map((y, idx) => ({
      name: legendLabel,
      x: `${idx + 1} ${legendLabel}`, // Cambia esto según tus necesidades
      y,
    }))
    .sort((a, b) => a.y - b.y);
}

function getLegendData(
  histogram: ReportPieChart.Histogram,
  legendLabel: string,
): { name: string }[] {
  return histogramToPieChartData(histogram, '').map((d) => ({
    name: `${d.x} ${legendLabel}: ${d.y} VM`,
  }));
}

// eslint-disable-next-line @typescript-eslint/no-namespace
export namespace ReportPieChart {
  export type Histogram = {
    data: number[];
    minValue: number;
    step: number;
  };

  export type Props = {
    histogram: Histogram;
    title: string;
    legendLabel: string;
  };
}

export function ReportPieChart(props: ReportPieChart.Props): React.ReactNode {
  const { title, histogram, legendLabel } = props;
  return (
    <>
      <Content style={{ textAlign: 'center' }}>
        <Content component="p">{title}</Content>
      </Content>
      <ChartPie
        name={title.toLowerCase().split(' ').join('-')}
        ariaDesc={title + ' chart'}
        ariaTitle={title + ' chart'}
        constrainToVisibleArea
        data={histogramToPieChartData(histogram, legendLabel)}
        height={230}
        labels={({ datum }) => `${datum.x}: ${datum.y}`}
        legendData={getLegendData(histogram, legendLabel)}
        legendOrientation="vertical"
        legendPosition="right"
        padding={{
          bottom: 20,
          left: 20,
          right: 140, // Adjusted to accommodate legend
          top: 20,
        }}
        width={450}
        colorScale={[
          '#73BCF7',
          '#73C5C5',
          '#F9E0A2',
          '#BDE5B8',
          '#D2D2D2',
          '#F4B678',
          '#CBC1FF',
          '#FF7468',
          '#7CDBF3',
          '#E4F5BC',
        ]}
      />
    </>
  );
}
