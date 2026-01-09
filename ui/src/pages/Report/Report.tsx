import React, { useState } from "react";

import {
  Infra,
  Inventory,
  InventoryData,
  VMResourceBreakdown,
  VMs,
} from "@generated/index";
import {
  Bullseye,
  Content,
  MenuToggle,
  MenuToggleElement,
  Select,
  SelectList,
  SelectOption,
  StackItem,
} from "@patternfly/react-core";

import {
  buildClusterViewModel,
  ClusterOption,
} from "./assessment-report/clusterView";
import { Dashboard } from "./assessment-report/Dashboard";

interface ReportProps {
  inventory: Inventory;
}

const Report: React.FC<ReportProps> = ({ inventory }) => {
  const [selectedClusterId, setSelectedClusterId] = useState<string>("all");
  const [isClusterSelectOpen, setIsClusterSelectOpen] = useState(false);

  const infra = inventory?.vcenter?.infra as Infra | undefined;
  const vms = inventory?.vcenter?.vms as VMs | undefined;
  const clusters = inventory?.clusters as
    | { [key: string]: InventoryData }
    | undefined;

  const clusterView = buildClusterViewModel({
    infra,
    vms,
    clusters,
    selectedClusterId,
  });

  const hasClusterScopedData =
    Boolean(clusterView.viewInfra) &&
    Boolean(clusterView.viewVms) &&
    Boolean(clusterView.cpuCores) &&
    Boolean(clusterView.ramGB);

  const clusterSelectDisabled = clusterView.clusterOptions.length <= 1;

  const handleClusterSelect = (
    _event: React.MouseEvent<Element, MouseEvent> | undefined,
    value: string | number | undefined
  ): void => {
    if (typeof value === "string") {
      setSelectedClusterId(value);
    }
    setIsClusterSelectOpen(false);
  };

  return (
    <>
      <StackItem>
        <Select
          isScrollable
          isOpen={isClusterSelectOpen}
          selected={clusterView.selectionId}
          onSelect={handleClusterSelect}
          onOpenChange={(isOpen: boolean) => {
            if (!clusterSelectDisabled) setIsClusterSelectOpen(isOpen);
          }}
          toggle={(toggleRef: React.Ref<MenuToggleElement>) => (
            <MenuToggle
              ref={toggleRef}
              isExpanded={isClusterSelectOpen}
              onClick={() => {
                if (!clusterSelectDisabled) {
                  setIsClusterSelectOpen((prev) => !prev);
                }
              }}
              isDisabled={clusterSelectDisabled}
              style={{ minWidth: "422px" }}
            >
              {clusterView.selectionLabel}
            </MenuToggle>
          )}
        >
          <SelectList>
            {clusterView.clusterOptions.map((option: ClusterOption) => (
              <SelectOption key={option.id} value={option.id}>
                {option.label}
              </SelectOption>
            ))}
          </SelectList>
        </Select>
      </StackItem>

      <StackItem>
        {hasClusterScopedData ? (
          <Dashboard
            infra={clusterView.viewInfra as Infra}
            vms={clusterView.viewVms as VMs}
            cpuCores={clusterView.cpuCores as VMResourceBreakdown}
            ramGB={clusterView.ramGB as VMResourceBreakdown}
            clusters={clusterView.viewClusters}
            isAggregateView={clusterView.isAggregateView}
            clusterFound={clusterView.clusterFound}
          />
        ) : (
          <Bullseye style={{ width: "100%" }}>
            <Content>
              <Content component="p">
                {clusterView.isAggregateView
                  ? "This assessment does not have report data yet."
                  : "No data is available for the selected cluster."}
              </Content>
            </Content>
          </Bullseye>
        )}
      </StackItem>
    </>
  );
};

Report.displayName = "Report";

export default Report;
