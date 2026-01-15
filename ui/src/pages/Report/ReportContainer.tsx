import React, { useState, useEffect } from "react";
import {
  MenuToggle,
  MenuToggleElement,
  Select,
  SelectList,
  SelectOption,
  Stack,
  StackItem,
  Tab,
  Tabs,
  TabTitleText,
} from "@patternfly/react-core";
import { useAppSelector, useAppDispatch } from "@shared/store";
import { fetchVMs, setPage, setPageSize, setSort, setFilters } from "@shared/reducers";
import type { VMFilters } from "@shared/reducers/vmSlice";
import { Infra, InventoryData, VMs } from "@generated/index";
import { DataSharingAlert, DataSharingModal } from "@shared/components";
import { changeAgentMode } from "@shared/reducers/agentSlice";
import { AgentModeRequestModeEnum } from "@generated/index";
import Header from "./Header";
import Report from "./Report";
import { VirtualMachinesView } from "./VirtualMachines";
import { buildClusterViewModel, ClusterOption } from "./assessment-report/clusterView";

const ReportContainer: React.FC = () => {
  const dispatch = useAppDispatch();
  const { inventory } = useAppSelector((state) => state.collector);
  const { mode } = useAppSelector((state) => state.agent);
  const vmState = useAppSelector((state) => state.vm);
  const [activeTab, setActiveTab] = useState<string | number>(0);
  const [selectedClusterId, setSelectedClusterId] = useState<string>("all");
  const [isClusterSelectOpen, setIsClusterSelectOpen] = useState(false);
  const [isShareModalOpen, setIsShareModalOpen] = useState(false);
  const [isShareLoading, setIsShareLoading] = useState(false);

  // Fetch VMs when tab switches to Virtual Machines or on initial load
  useEffect(() => {
    if (activeTab === 1 && !vmState.initialized) {
      dispatch(fetchVMs());
    }
  }, [activeTab, vmState.initialized, dispatch]);

  const handlePageChange = (newPage: number) => {
    dispatch(setPage(newPage));
    dispatch(fetchVMs({ page: newPage }));
  };

  const handlePageSizeChange = (newPageSize: number) => {
    dispatch(setPageSize(newPageSize));
    dispatch(fetchVMs({ pageSize: newPageSize }));
  };

  const handleSortChange = (newSort: string[]) => {
    dispatch(setSort(newSort));
    dispatch(fetchVMs({ sort: newSort }));
  };

  const handleFilterChange = (newFilters: VMFilters) => {
    dispatch(setFilters(newFilters));
    dispatch(fetchVMs({ filters: newFilters }));
  };

  if (!inventory) {
    return null;
  }

  const infra = inventory?.vcenter?.infra as Infra | undefined;
  const vms = inventory?.vcenter?.vms as VMs | undefined;
  const clusters = inventory?.clusters as { [key: string]: InventoryData } | undefined;

  const clusterView = buildClusterViewModel({
    infra,
    vms,
    clusters,
    selectedClusterId,
  });

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

  const totalVMs = vms?.total ?? 0;
  const totalClusters = clusters ? Object.keys(clusters).length : 0;
  const isDataShared = mode === "connected";

  const handleShareClick = () => {
    setIsShareModalOpen(true);
  };

  const handleShareConfirm = async () => {
    setIsShareLoading(true);
    try {
      await dispatch(changeAgentMode(AgentModeRequestModeEnum.Connected));
      setIsShareModalOpen(false);
    } finally {
      setIsShareLoading(false);
    }
  };

  const handleShareCancel = () => {
    setIsShareModalOpen(false);
  };

  return (
    <div style={{ padding: "24px", width: "100%" }}>
      <Stack hasGutter>
        {/* Header */}
        <StackItem>
          <Header
            totalVMs={totalVMs}
            totalClusters={totalClusters}
            isConnected={true}
          />
        </StackItem>

        {/* Data Sharing Alert - shown when not shared */}
        {!isDataShared && (
          <StackItem>
            <DataSharingAlert onShare={handleShareClick} />
          </StackItem>
        )}

        {/* Cluster Selector */}
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
                style={{ minWidth: "250px" }}
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

        {/* Tabs */}
        <StackItem>
          <Tabs
            activeKey={activeTab}
            onSelect={(_event, tabIndex) => setActiveTab(tabIndex)}
          >
            <Tab eventKey={0} title={<TabTitleText>Overview</TabTitleText>}>
              <div
                style={{
                  marginTop: "24px",
                  width: "60%",
                  marginLeft: "auto",
                  marginRight: "auto",
                }}
              >
                <Report clusterView={clusterView} />
              </div>
            </Tab>
            <Tab eventKey={1} title={<TabTitleText>Virtual Machines</TabTitleText>}>
              <div style={{ marginTop: "24px" }}>
                <VirtualMachinesView
                  vms={vmState.vms}
                  total={vmState.total}
                  page={vmState.page}
                  pageSize={vmState.pageSize}
                  pageCount={vmState.pageCount}
                  loading={vmState.loading}
                  sort={vmState.sort}
                  filters={vmState.filters}
                  onPageChange={handlePageChange}
                  onPageSizeChange={handlePageSizeChange}
                  onSortChange={handleSortChange}
                  onFilterChange={handleFilterChange}
                />
              </div>
            </Tab>
          </Tabs>
        </StackItem>
      </Stack>

      <DataSharingModal
        isOpen={isShareModalOpen}
        onConfirm={handleShareConfirm}
        onCancel={handleShareCancel}
        isLoading={isShareLoading}
      />
    </div>
  );
};

ReportContainer.displayName = "ReportContainer";

export default ReportContainer;
