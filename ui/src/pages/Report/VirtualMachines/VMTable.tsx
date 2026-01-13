import React, { useState, useMemo } from "react";
import {
  Button,
  Dropdown,
  DropdownItem,
  DropdownList,
  MenuToggle,
  MenuToggleElement,
  Pagination,
  SearchInput,
  Toolbar,
  ToolbarContent,
  ToolbarGroup,
  ToolbarItem,
} from "@patternfly/react-core";
import {
  Table,
  Thead,
  Tr,
  Th,
  Tbody,
  Td,
  ThProps,
} from "@patternfly/react-table";
import {
  FilterIcon,
  EllipsisVIcon,
  ExclamationTriangleIcon,
  ExclamationCircleIcon,
  CheckCircleIcon,
} from "@patternfly/react-icons";
import type { VM } from "@generated/index";

interface VMTableProps {
  vms: VM[];
  total: number;
  page: number;
  pageSize: number;
  pageCount: number;
  loading: boolean;
  sort?: string[];
  onPageChange: (page: number) => void;
  onPageSizeChange: (pageSize: number) => void;
  onSortChange: (sort: string[]) => void;
  onVMClick?: (vm: VM) => void;
}

type SortableColumn = "name" | "vCenterState" | "diskSize" | "memory" | "issues";

const statusLabels: Record<string, string> = {
  "green": "Migratable",
  "yellow": "With warnings",
  "red": "Not migratable",
};

const MB_IN_GB = 1024;
const MB_IN_TB = 1024 * 1024;

/**
 * Format disk size from MB to appropriate unit (GB or TB)
 * Shows TB for sizes >= 1TB, otherwise GB
 */
const formatDiskSize = (sizeInMB: number): string => {
  if (sizeInMB >= MB_IN_TB) {
    const sizeInTB = sizeInMB / MB_IN_TB;
    return `${sizeInTB.toFixed(sizeInTB % 1 === 0 ? 0 : 2)} TB`;
  }
  const sizeInGB = sizeInMB / MB_IN_GB;
  return `${sizeInGB.toFixed(sizeInGB % 1 === 0 ? 0 : 2)} GB`;
};

/**
 * Format memory size from MB to GB
 */
const formatMemorySize = (sizeInMB: number): string => {
  const sizeInGB = sizeInMB / MB_IN_GB;
  return `${sizeInGB.toFixed(sizeInGB % 1 === 0 ? 0 : 2)} GB`;
};

const VMTable: React.FC<VMTableProps> = ({
  vms,
  total,
  page,
  pageSize,
  loading,
  sort = [],
  onPageChange,
  onPageSizeChange,
  onSortChange,
  onVMClick,
}) => {
  // Search state
  const [searchValue, setSearchValue] = useState("");

  // Filter state
  const [statusFilter, setStatusFilter] = useState<string[]>([]);
  const [isStatusFilterOpen, setIsStatusFilterOpen] = useState(false);

  // Selection state
  const [selectedVMs, setSelectedVMs] = useState<Set<string>>(new Set());

  // Row actions dropdown state
  const [openActionMenuId, setOpenActionMenuId] = useState<string | null>(null);

  // Column definitions
  const columns: { key: SortableColumn; label: string; sortable: boolean }[] = [
    { key: "name", label: "Name", sortable: true },
    { key: "vCenterState", label: "Status", sortable: true },
    { key: "diskSize", label: "Disk size", sortable: true },
    { key: "memory", label: "Memory size", sortable: true },
    { key: "issues", label: "Issues", sortable: true },
  ];

  // Filter and search VMs (client-side filtering within the current page for search only)
  const filteredVMs = useMemo(() => {
    return vms.filter((vm) => {
      // Search filter (client-side for current page only)
      if (searchValue && !vm.name.toLowerCase().includes(searchValue.toLowerCase())) {
        return false;
      }
      // Status filter (client-side for current page only)
      if (statusFilter.length > 0 && !statusFilter.includes(vm.vCenterState)) {
        return false;
      }
      return true;
    });
  }, [vms, searchValue, statusFilter]);

  // Parse current sort state from props
  const { activeSortIndex, activeSortDirection } = useMemo(() => {
    if (sort.length === 0) {
      return { activeSortIndex: null, activeSortDirection: "asc" as const };
    }
    // Use the first sort field for the UI indicator
    const [field, direction] = sort[0].split(":");
    const index = columns.findIndex((col) => col.key === field);
    return {
      activeSortIndex: index >= 0 ? index : null,
      activeSortDirection: (direction as "asc" | "desc") || "asc",
    };
  }, [sort, columns]);

  // Sort handler - triggers server-side sorting
  const getSortParams = (columnIndex: number): ThProps["sort"] => ({
    sortBy: {
      index: activeSortIndex ?? undefined,
      direction: activeSortDirection,
    },
    onSort: (_event, index, direction) => {
      const columnKey = columns[index].key;
      onSortChange([`${columnKey}:${direction}`]);
    },
    columnIndex,
  });

  // Selection handlers
  const isAllSelected = filteredVMs.length > 0 && filteredVMs.every((vm) => selectedVMs.has(vm.id));
  const isSomeSelected = filteredVMs.some((vm) => selectedVMs.has(vm.id));

  const onSelectAll = (isSelected: boolean) => {
    if (isSelected) {
      const newSelected = new Set(selectedVMs);
      filteredVMs.forEach((vm) => newSelected.add(vm.id));
      setSelectedVMs(newSelected);
    } else {
      const newSelected = new Set(selectedVMs);
      filteredVMs.forEach((vm) => newSelected.delete(vm.id));
      setSelectedVMs(newSelected);
    }
  };

  const onSelectVM = (vm: VM, isSelected: boolean) => {
    const newSelected = new Set(selectedVMs);
    if (isSelected) {
      newSelected.add(vm.id);
    } else {
      newSelected.delete(vm.id);
    }
    setSelectedVMs(newSelected);
  };

  // Status filter handlers
  const onStatusFilterSelect = (status: string) => {
    if (statusFilter.includes(status)) {
      setStatusFilter(statusFilter.filter((s) => s !== status));
    } else {
      setStatusFilter([...statusFilter, status]);
    }
  };

  const clearStatusFilter = () => {
    setStatusFilter([]);
  };

  // Render status cell with icon
  const renderStatus = (vm: VM) => {
    const state = vm.vCenterState;
    const hasIssues = vm.issues.length > 0;

    return (
      <span style={{ display: "flex", alignItems: "center", gap: "8px" }}>
        {state === "red" && (
          <ExclamationCircleIcon color="var(--pf-t--global--icon--color--status--danger--default)" />
        )}
        {state === "yellow" && (
          <ExclamationTriangleIcon color="var(--pf-t--global--icon--color--status--warning--default)" />
        )}
        {state === "green" && !hasIssues && (
          <CheckCircleIcon color="var(--pf-t--global--icon--color--status--success--default)" />
        )}
        {statusLabels[state] || state}
      </span>
    );
  };

  // Render issues column
  const renderIssues = (vm: VM) => {
    return vm.issues.length;
  };

  return (
    <div>
      {/* Toolbar */}
      <Toolbar>
        <ToolbarContent>
          <ToolbarGroup variant="filter-group">
            <ToolbarItem>
              <SearchInput
                placeholder="Find by name"
                value={searchValue}
                onChange={(_event, value) => setSearchValue(value)}
                onClear={() => setSearchValue("")}
                style={{ minWidth: "200px" }}
              />
            </ToolbarItem>

            <ToolbarItem>
              <Dropdown
                isOpen={isStatusFilterOpen}
                onSelect={() => {}}
                onOpenChange={setIsStatusFilterOpen}
                toggle={(toggleRef: React.Ref<MenuToggleElement>) => (
                  <MenuToggle
                    ref={toggleRef}
                    onClick={() => setIsStatusFilterOpen(!isStatusFilterOpen)}
                    isExpanded={isStatusFilterOpen}
                  >
                    <FilterIcon /> Status
                    {statusFilter.length > 0 && ` (${statusFilter.length})`}
                  </MenuToggle>
                )}
              >
                <DropdownList>
                  {Object.entries(statusLabels).map(([status, label]) => (
                    <DropdownItem
                      key={status}
                      onClick={() => onStatusFilterSelect(status)}
                      isSelected={statusFilter.includes(status)}
                    >
                      {label}
                    </DropdownItem>
                  ))}
                </DropdownList>
              </Dropdown>
            </ToolbarItem>
          </ToolbarGroup>

          <ToolbarGroup>
            <ToolbarItem>
              <Button variant="secondary" isDisabled={selectedVMs.size === 0}>
                Send to deep inspection
              </Button>
            </ToolbarItem>
          </ToolbarGroup>

          <ToolbarItem variant="pagination" align={{ default: "alignEnd" }}>
            <Pagination
              itemCount={total}
              perPage={pageSize}
              page={page}
              onSetPage={(_event, newPage) => onPageChange(newPage)}
              onPerPageSelect={(_event, newPerPage) => {
                onPageSizeChange(newPerPage);
              }}
              variant="top"
              isCompact
            />
          </ToolbarItem>
        </ToolbarContent>
      </Toolbar>

      {/* Table */}
      <Table aria-label="Virtual machines table" variant="compact">
        <Thead>
          <Tr>
            <Th screenReaderText="Select" />
            {columns.map((column, index) => (
              <Th
                key={column.key}
                sort={column.sortable ? getSortParams(index) : undefined}
              >
                {column.label}
              </Th>
            ))}
            <Th screenReaderText="Actions" />
          </Tr>
        </Thead>
        <Tbody>
          {loading ? (
            <Tr>
              <Td colSpan={columns.length + 2} style={{ textAlign: "center" }}>
                Loading...
              </Td>
            </Tr>
          ) : filteredVMs.length === 0 ? (
            <Tr>
              <Td colSpan={columns.length + 2} style={{ textAlign: "center" }}>
                No virtual machines found
              </Td>
            </Tr>
          ) : (
            filteredVMs.map((vm) => (
              <Tr key={vm.id}>
                <Td
                  select={{
                    rowIndex: 0,
                    onSelect: (_event, isSelected) => onSelectVM(vm, isSelected),
                    isSelected: selectedVMs.has(vm.id),
                  }}
                />
                <Td dataLabel="Name">
                  <Button variant="link" isInline onClick={() => onVMClick?.(vm)}>
                    {vm.name}
                  </Button>
                </Td>
                <Td dataLabel="Status">{renderStatus(vm)}</Td>
                <Td dataLabel="Disk size">{formatDiskSize(vm.diskSize)}</Td>
                <Td dataLabel="Memory size">{formatMemorySize(vm.memory)}</Td>
                <Td dataLabel="Issues">{renderIssues(vm)}</Td>
                <Td isActionCell>
                  <Dropdown
                    isOpen={openActionMenuId === vm.id}
                    onSelect={() => setOpenActionMenuId(null)}
                    onOpenChange={(isOpen) => setOpenActionMenuId(isOpen ? vm.id : null)}
                    toggle={(toggleRef: React.Ref<MenuToggleElement>) => (
                      <MenuToggle
                        ref={toggleRef}
                        variant="plain"
                        onClick={() =>
                          setOpenActionMenuId(openActionMenuId === vm.id ? null : vm.id)
                        }
                        isExpanded={openActionMenuId === vm.id}
                      >
                        <EllipsisVIcon />
                      </MenuToggle>
                    )}
                    popperProps={{ position: "right" }}
                  >
                    <DropdownList>
                      <DropdownItem key="inspect">Send to deep inspection</DropdownItem>
                      <DropdownItem key="details">View details</DropdownItem>
                    </DropdownList>
                  </Dropdown>
                </Td>
              </Tr>
            ))
          )}
        </Tbody>
      </Table>
    </div>
  );
};

VMTable.displayName = "VMTable";

export default VMTable;
