import React, { useState, useMemo } from "react";
import {
  Button,
  Dropdown,
  DropdownGroup,
  DropdownItem,
  DropdownList,
  Label,
  LabelGroup,
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
import type { VMFilters } from "@shared/reducers/vmSlice";

interface VMTableProps {
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
  onVMClick?: (vm: VM) => void;
}

type SortableColumn = "name" | "vCenterState" | "diskSize" | "memory" | "issues";

const statusLabels: Record<string, string> = {
  green: "Migratable",
  yellow: "With warnings",
  red: "Not migratable",
};

// Disk size ranges in MB (displayed as TB)
const diskSizeRanges = [
  { label: "0-10 TB", min: 0, max: 10 * 1024 * 1024 },
  { label: "11-20 TB", min: 11 * 1024 * 1024, max: 20 * 1024 * 1024 },
  { label: "21-50 TB", min: 21 * 1024 * 1024, max: 50 * 1024 * 1024 },
  { label: "50+ TB", min: 50 * 1024 * 1024, max: undefined },
];

// Memory size ranges in MB (displayed as GB)
const memorySizeRanges = [
  { label: "0-4 GB", min: 0, max: 4 * 1024 },
  { label: "5-16 GB", min: 5 * 1024, max: 16 * 1024 },
  { label: "17-32 GB", min: 17 * 1024, max: 32 * 1024 },
  { label: "33-64 GB", min: 33 * 1024, max: 64 * 1024 },
  { label: "65-128 GB", min: 65 * 1024, max: 128 * 1024 },
  { label: "129-256 GB", min: 129 * 1024, max: 256 * 1024 },
  { label: "256+ GB", min: 256 * 1024, max: undefined },
];

const MB_IN_GB = 1024;
const MB_IN_TB = 1024 * 1024;

const formatDiskSize = (sizeInMB: number): string => {
  if (sizeInMB >= MB_IN_TB) {
    const sizeInTB = sizeInMB / MB_IN_TB;
    return `${sizeInTB.toFixed(sizeInTB % 1 === 0 ? 0 : 2)} TB`;
  }
  const sizeInGB = sizeInMB / MB_IN_GB;
  return `${sizeInGB.toFixed(sizeInGB % 1 === 0 ? 0 : 2)} GB`;
};

const formatMemorySize = (sizeInMB: number): string => {
  const sizeInGB = sizeInMB / MB_IN_GB;
  return `${sizeInGB.toFixed(sizeInGB % 1 === 0 ? 0 : 2)} GB`;
};

interface AppliedFilter {
  category: string;
  label: string;
  key: string;
}

// Helper to find disk range index from filter values
const findDiskRangeIndex = (min?: number, max?: number): number | null => {
  if (min === undefined) return null;
  const index = diskSizeRanges.findIndex(
    (r) => r.min === min && r.max === max
  );
  return index >= 0 ? index : null;
};

// Helper to find memory range index from filter values
const findMemoryRangeIndex = (min?: number, max?: number): number | null => {
  if (min === undefined) return null;
  const index = memorySizeRanges.findIndex(
    (r) => r.min === min && r.max === max
  );
  return index >= 0 ? index : null;
};

const VMTable: React.FC<VMTableProps> = ({
  vms,
  total,
  page,
  pageSize,
  loading,
  sort = [],
  filters = {},
  onPageChange,
  onPageSizeChange,
  onSortChange,
  onFilterChange,
  onVMClick,
}) => {
  // Search state (client-side only)
  const [searchValue, setSearchValue] = useState("");

  // Filter dropdown state
  const [isFilterOpen, setIsFilterOpen] = useState(false);

  // Local filter state for UI - initialized from filters prop
  const [selectedDiskRange, setSelectedDiskRange] = useState<number | null>(
    () => findDiskRangeIndex(filters.diskSizeMin, filters.diskSizeMax)
  );
  const [selectedMemoryRange, setSelectedMemoryRange] = useState<number | null>(
    () => findMemoryRangeIndex(filters.memorySizeMin, filters.memorySizeMax)
  );
  const [selectedStatuses, setSelectedStatuses] = useState<string[]>(
    () => filters.status || []
  );
  const [hasIssuesFilter, setHasIssuesFilter] = useState(
    () => (filters.minIssues ?? 0) > 0
  );

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

  // Build list of applied filters for chip display
  const appliedFilters = useMemo(() => {
    const filters: AppliedFilter[] = [];

    if (selectedDiskRange !== null) {
      filters.push({
        category: "Disk size",
        label: diskSizeRanges[selectedDiskRange].label,
        key: "diskSize",
      });
    }

    if (selectedMemoryRange !== null) {
      filters.push({
        category: "Memory",
        label: memorySizeRanges[selectedMemoryRange].label,
        key: "memorySize",
      });
    }

    selectedStatuses.forEach((status) => {
      filters.push({
        category: "Status",
        label: statusLabels[status] || status,
        key: `status-${status}`,
      });
    });

    if (hasIssuesFilter) {
      filters.push({
        category: "Issues",
        label: "Has issues",
        key: "hasIssues",
      });
    }

    return filters;
  }, [selectedDiskRange, selectedMemoryRange, selectedStatuses, hasIssuesFilter]);

  // Client-side search filter only
  const filteredVMs = useMemo(() => {
    return vms.filter((vm) => {
      if (searchValue && !vm.name.toLowerCase().includes(searchValue.toLowerCase())) {
        return false;
      }
      return true;
    });
  }, [vms, searchValue]);

  // Parse current sort state from props
  const { activeSortIndex, activeSortDirection } = useMemo(() => {
    if (sort.length === 0) {
      return { activeSortIndex: null, activeSortDirection: "asc" as const };
    }
    const [field, direction] = sort[0].split(":");
    const index = columns.findIndex((col) => col.key === field);
    return {
      activeSortIndex: index >= 0 ? index : null,
      activeSortDirection: (direction as "asc" | "desc") || "asc",
    };
  }, [sort, columns]);

  // Sort handler
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

  // Apply filters to parent
  const applyFilters = (updates: Partial<{
    diskRange: number | null;
    memoryRange: number | null;
    statuses: string[];
    hasIssues: boolean;
  }>) => {
    const diskRange = updates.diskRange !== undefined ? updates.diskRange : selectedDiskRange;
    const memoryRange = updates.memoryRange !== undefined ? updates.memoryRange : selectedMemoryRange;
    const statuses = updates.statuses !== undefined ? updates.statuses : selectedStatuses;
    const hasIssues = updates.hasIssues !== undefined ? updates.hasIssues : hasIssuesFilter;

    const newFilters: VMFilters = { ...filters };

    // Disk size range
    if (diskRange !== null) {
      const range = diskSizeRanges[diskRange];
      newFilters.diskSizeMin = range.min;
      newFilters.diskSizeMax = range.max;
    } else {
      delete newFilters.diskSizeMin;
      delete newFilters.diskSizeMax;
    }

    // Memory size range
    if (memoryRange !== null) {
      const range = memorySizeRanges[memoryRange];
      newFilters.memorySizeMin = range.min;
      newFilters.memorySizeMax = range.max;
    } else {
      delete newFilters.memorySizeMin;
      delete newFilters.memorySizeMax;
    }

    // Status filter
    if (statuses.length > 0) {
      newFilters.status = statuses;
    } else {
      delete newFilters.status;
    }

    // Issues filter
    if (hasIssues) {
      newFilters.minIssues = 1;
    } else {
      delete newFilters.minIssues;
    }

    onFilterChange(newFilters);
  };

  // Filter handlers
  const onDiskSizeSelect = (index: number) => {
    const newValue = selectedDiskRange === index ? null : index;
    setSelectedDiskRange(newValue);
    applyFilters({ diskRange: newValue });
  };

  const onMemorySizeSelect = (index: number) => {
    const newValue = selectedMemoryRange === index ? null : index;
    setSelectedMemoryRange(newValue);
    applyFilters({ memoryRange: newValue });
  };

  const onStatusSelect = (status: string) => {
    const newStatuses = selectedStatuses.includes(status)
      ? selectedStatuses.filter((s) => s !== status)
      : [...selectedStatuses, status];
    setSelectedStatuses(newStatuses);
    applyFilters({ statuses: newStatuses });
  };

  const onIssuesFilterToggle = () => {
    const newValue = !hasIssuesFilter;
    setHasIssuesFilter(newValue);
    applyFilters({ hasIssues: newValue });
  };

  // Remove individual filter
  const removeFilter = (filterKey: string) => {
    if (filterKey === "diskSize") {
      setSelectedDiskRange(null);
      applyFilters({ diskRange: null });
    } else if (filterKey === "memorySize") {
      setSelectedMemoryRange(null);
      applyFilters({ memoryRange: null });
    } else if (filterKey.startsWith("status-")) {
      const status = filterKey.replace("status-", "");
      const newStatuses = selectedStatuses.filter((s) => s !== status);
      setSelectedStatuses(newStatuses);
      applyFilters({ statuses: newStatuses });
    } else if (filterKey === "hasIssues") {
      setHasIssuesFilter(false);
      applyFilters({ hasIssues: false });
    }
  };

  // Clear all filters
  const clearAllFilters = () => {
    setSelectedDiskRange(null);
    setSelectedMemoryRange(null);
    setSelectedStatuses([]);
    setHasIssuesFilter(false);
    onFilterChange({});
  };

  // Selection handlers
  const onSelectVM = (vm: VM, isSelected: boolean) => {
    const newSelected = new Set(selectedVMs);
    if (isSelected) {
      newSelected.add(vm.id);
    } else {
      newSelected.delete(vm.id);
    }
    setSelectedVMs(newSelected);
  };

  // Render status cell with icon
  const renderStatus = (vm: VM) => {
    const state = vm.vCenterState;
    const hasIssues = vm.issueCount > 0;

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

            {/* Consolidated Filters Dropdown */}
            <ToolbarItem>
              <Dropdown
                isOpen={isFilterOpen}
                onSelect={() => {}}
                onOpenChange={setIsFilterOpen}
                toggle={(toggleRef: React.Ref<MenuToggleElement>) => (
                  <MenuToggle
                    ref={toggleRef}
                    onClick={() => setIsFilterOpen(!isFilterOpen)}
                    isExpanded={isFilterOpen}
                  >
                    <FilterIcon /> Filters
                  </MenuToggle>
                )}
              >
                <DropdownGroup label="Disk size">
                  <DropdownList>
                    {diskSizeRanges.map((range, index) => (
                      <DropdownItem
                        key={range.label}
                        onClick={() => onDiskSizeSelect(index)}
                        isSelected={selectedDiskRange === index}
                      >
                        {range.label}
                      </DropdownItem>
                    ))}
                  </DropdownList>
                </DropdownGroup>
                <DropdownGroup label="Memory size">
                  <DropdownList>
                    {memorySizeRanges.map((range, index) => (
                      <DropdownItem
                        key={range.label}
                        onClick={() => onMemorySizeSelect(index)}
                        isSelected={selectedMemoryRange === index}
                      >
                        {range.label}
                      </DropdownItem>
                    ))}
                  </DropdownList>
                </DropdownGroup>
                <DropdownGroup label="Status">
                  <DropdownList>
                    {Object.entries(statusLabels).map(([status, label]) => (
                      <DropdownItem
                        key={status}
                        onClick={() => onStatusSelect(status)}
                        isSelected={selectedStatuses.includes(status)}
                      >
                        {label}
                      </DropdownItem>
                    ))}
                  </DropdownList>
                </DropdownGroup>
                <DropdownGroup label="Issues">
                  <DropdownList>
                    <DropdownItem
                      onClick={onIssuesFilterToggle}
                      isSelected={hasIssuesFilter}
                    >
                      Has issues
                    </DropdownItem>
                  </DropdownList>
                </DropdownGroup>
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

        {/* Applied filters chips */}
        {appliedFilters.length > 0 && (
          <ToolbarContent>
            <ToolbarItem>
              <LabelGroup categoryName="Filters">
                {appliedFilters.map((filter) => (
                  <Label
                    key={filter.key}
                    onClose={() => removeFilter(filter.key)}
                  >
                    {filter.label}
                  </Label>
                ))}
              </LabelGroup>
            </ToolbarItem>
            <ToolbarItem>
              <span style={{ color: "var(--pf-t--global--text--color--subtle)" }}>
                {appliedFilters.length} filter{appliedFilters.length !== 1 ? "s" : ""} applied
              </span>
            </ToolbarItem>
            <ToolbarItem>
              <Button variant="link" onClick={clearAllFilters}>
                Clear all filters
              </Button>
            </ToolbarItem>
          </ToolbarContent>
        )}
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
                <Td dataLabel="Issues">{vm.issueCount}</Td>
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
