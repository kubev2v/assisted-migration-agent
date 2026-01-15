import React from "react";
import { useAppSelector, useAppDispatch } from "@shared/store";
import { selectVM } from "@shared/reducers";
import type { VMFilters } from "@shared/reducers/vmSlice";
import VMTable from "./VMTable";
import VMDetail from "./VMDetail";
import type { VM } from "@generated/index";

interface VirtualMachinesViewProps {
  vms: VM[];
  total: number;
  page: number;
  pageSize: number;
  pageCount: number;
  loading: boolean;
  sort?: string[];
  filters?: VMFilters;
  onPageChange: (page: number) => void;
  onPageSizeChange: (pageSize: number) => void;
  onSortChange: (sort: string[]) => void;
  onFilterChange: (filters: VMFilters) => void;
}

const VirtualMachinesView: React.FC<VirtualMachinesViewProps> = ({
  vms,
  total,
  page,
  pageSize,
  pageCount,
  loading,
  sort,
  filters,
  onPageChange,
  onPageSizeChange,
  onSortChange,
  onFilterChange,
}) => {
  const dispatch = useAppDispatch();
  const { selectedVMId } = useAppSelector((state) => state.vm);

  const handleVMClick = (vm: VM) => {
    dispatch(selectVM(vm.id));
  };

  const handleBack = () => {
    // clearSelectedVM is called in VMDetail
  };

  if (selectedVMId) {
    return <VMDetail vmId={selectedVMId} onBack={handleBack} />;
  }

  return (
    <VMTable
      vms={vms}
      total={total}
      page={page}
      pageSize={pageSize}
      pageCount={pageCount}
      loading={loading}
      sort={sort}
      filters={filters}
      onPageChange={onPageChange}
      onPageSizeChange={onPageSizeChange}
      onSortChange={onSortChange}
      onFilterChange={onFilterChange}
      onVMClick={handleVMClick}
    />
  );
};

VirtualMachinesView.displayName = "VirtualMachinesView";

export default VirtualMachinesView;
