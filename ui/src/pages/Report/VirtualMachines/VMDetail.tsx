import React, { useEffect } from "react";
import "../assessment-report/Dashboard.css";
import {
  Alert,
  Breadcrumb,
  BreadcrumbItem,
  Button,
  Card,
  CardBody,
  CardTitle,
  DescriptionList,
  DescriptionListDescription,
  DescriptionListGroup,
  DescriptionListTerm,
  Flex,
  FlexItem,
  Label,
  Spinner,
  Stack,
  StackItem,
  Title,
} from "@patternfly/react-core";
import { Table, Thead, Tr, Th, Tbody, Td } from "@patternfly/react-table";
import {
  ArrowLeftIcon,
  CheckCircleIcon,
  ExclamationTriangleIcon,
  InfoCircleIcon,
  CpuIcon,
  OutlinedHddIcon,
  CogIcon,
  StorageDomainIcon,
  NetworkIcon,
} from "@patternfly/react-icons";
import { useAppSelector, useAppDispatch } from "@shared/store";
import { fetchVMDetails, clearSelectedVM } from "@shared/reducers";

const MB_IN_GB = 1024;
const BYTES_IN_GB = 1024 * 1024 * 1024;

const formatMemorySize = (sizeInMB: number): string => {
  const sizeInGB = sizeInMB / MB_IN_GB;
  return `${sizeInGB.toFixed(sizeInGB % 1 === 0 ? 0 : 2)} GB`;
};

const formatCapacity = (bytes: number): string => {
  const gb = bytes / BYTES_IN_GB;
  if (gb >= 1024) {
    return `${(gb / 1024).toFixed(2)} TB`;
  }
  return `${gb.toFixed(2)} GB`;
};

interface VMDetailProps {
  vmId: string;
  onBack: () => void;
}

const VMDetail: React.FC<VMDetailProps> = ({ vmId, onBack }) => {
  const dispatch = useAppDispatch();
  const {
    selectedVMDetails: vm,
    detailLoading,
    detailError,
  } = useAppSelector((state) => state.vm);

  useEffect(() => {
    dispatch(fetchVMDetails(vmId));
  }, [dispatch, vmId]);

  const handleBack = () => {
    dispatch(clearSelectedVM());
    onBack();
  };

  if (detailLoading) {
    return (
      <div
        style={{ display: "flex", justifyContent: "center", padding: "48px" }}
      >
        <Spinner size="xl" />
      </div>
    );
  }

  if (detailError) {
    return (
      <Alert variant="danger" title="Failed to load VM details">
        {detailError.message}
      </Alert>
    );
  }

  if (!vm) {
    return null;
  }

  const renderStatusLabel = () => {
    const hasIssues = vm.issues && vm.issues.length > 0;
    if (!hasIssues) {
      return (
        <Label color="green" icon={<CheckCircleIcon />}>
          Migratable
        </Label>
      );
    }
    return (
      <Label color="orange" icon={<ExclamationTriangleIcon />}>
        With warnings
      </Label>
    );
  };

  const renderPowerState = () => {
    switch (vm.powerState) {
      case "poweredOn":
        return <Label color="green">Powered On</Label>;
      case "poweredOff":
        return <Label color="grey">Powered Off</Label>;
      case "suspended":
        return <Label color="orange">Suspended</Label>;
      default:
        return <Label>{vm.powerState}</Label>;
    }
  };

  return (
    <Stack hasGutter>
      <StackItem>
        <Breadcrumb>
          <BreadcrumbItem>
            <Button
              variant="link"
              onClick={handleBack}
              icon={<ArrowLeftIcon />}
            >
              Back to Virtual Machines
            </Button>
          </BreadcrumbItem>
        </Breadcrumb>
      </StackItem>

      <StackItem>
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
          }}
        >
          <div>
            <Title headingLevel="h1" size="2xl">
              {vm.name}
            </Title>
          </div>
          <div style={{ display: "flex", gap: "8px" }}>
            {renderPowerState()}
            {renderStatusLabel()}
          </div>
        </div>
      </StackItem>

      <StackItem>
        <Flex
          gap={{ default: "gapMd" }}
          alignItems={{ default: "alignItemsStretch" }}
        >
          <FlexItem flex={{ default: "flex_1" }}>
            <Card isFullHeight className="dashboard-card-border">
              <CardTitle>
                <InfoCircleIcon /> General
              </CardTitle>
              <CardBody>
                <DescriptionList isCompact>
                  <DescriptionListGroup>
                    <DescriptionListTerm>Datacenter</DescriptionListTerm>
                    <DescriptionListDescription>
                      {vm.datacenter || "—"}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>Cluster</DescriptionListTerm>
                    <DescriptionListDescription>
                      {vm.cluster || "—"}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>Host</DescriptionListTerm>
                    <DescriptionListDescription>
                      {vm.host || "—"}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>UUID</DescriptionListTerm>
                    <DescriptionListDescription>
                      {vm.uuid || "—"}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>Firmware</DescriptionListTerm>
                    <DescriptionListDescription>
                      {vm.firmware || "—"}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>VMware Tools</DescriptionListTerm>
                    <DescriptionListDescription>
                      {vm.toolsStatus || "—"}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                </DescriptionList>
              </CardBody>
            </Card>
          </FlexItem>

          <FlexItem flex={{ default: "flex_1" }}>
            <Card isFullHeight className="dashboard-card-border">
              <CardTitle>
                <CpuIcon /> Compute
              </CardTitle>
              <CardBody>
                <DescriptionList isCompact>
                  <DescriptionListGroup>
                    <DescriptionListTerm>Power State</DescriptionListTerm>
                    <DescriptionListDescription>
                      {vm.powerState}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>Connection State</DescriptionListTerm>
                    <DescriptionListDescription>
                      {vm.connectionState}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>vCPUs</DescriptionListTerm>
                    <DescriptionListDescription>
                      {vm.cpuCount}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>Cores/Socket</DescriptionListTerm>
                    <DescriptionListDescription>
                      {vm.coresPerSocket}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>Memory</DescriptionListTerm>
                    <DescriptionListDescription>
                      {formatMemorySize(vm.memoryMB)}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>CPU Affinity</DescriptionListTerm>
                    <DescriptionListDescription>
                      {vm.cpuAffinity && vm.cpuAffinity.length > 0
                        ? vm.cpuAffinity.join(", ")
                        : "None"}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                </DescriptionList>
              </CardBody>
            </Card>
          </FlexItem>

          <FlexItem flex={{ default: "flex_1" }}>
            <Card isFullHeight className="dashboard-card-border">
              <CardTitle>
                <OutlinedHddIcon /> Guest OS
              </CardTitle>
              <CardBody>
                <DescriptionList isCompact>
                  <DescriptionListGroup>
                    <DescriptionListTerm>OS</DescriptionListTerm>
                    <DescriptionListDescription>
                      {vm.guestName || "—"}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>Guest ID</DescriptionListTerm>
                    <DescriptionListDescription>
                      {vm.guestId || "—"}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>Hostname</DescriptionListTerm>
                    <DescriptionListDescription>
                      {vm.hostName || "—"}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>IP Address</DescriptionListTerm>
                    <DescriptionListDescription>
                      {vm.ipAddress || "—"}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                </DescriptionList>
              </CardBody>
            </Card>
          </FlexItem>

          <FlexItem flex={{ default: "flex_1" }}>
            <Card isFullHeight className="dashboard-card-border">
              <CardTitle>
                <CogIcon /> Features
              </CardTitle>
              <CardBody>
                <DescriptionList isCompact>
                  <DescriptionListGroup>
                    <DescriptionListTerm>Template</DescriptionListTerm>
                    <DescriptionListDescription>
                      {vm.isTemplate ? "Yes" : "No"}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>Fault Tolerance</DescriptionListTerm>
                    <DescriptionListDescription>
                      {vm.faultToleranceEnabled ? "Enabled" : "Disabled"}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>
                      Nested Virtualization
                    </DescriptionListTerm>
                    <DescriptionListDescription>
                      {vm.nestedHVEnabled ? "Enabled" : "Disabled"}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                </DescriptionList>
              </CardBody>
            </Card>
          </FlexItem>

          <FlexItem flex={{ default: "flex_1" }}>
            <Card isFullHeight className="dashboard-card-border">
              <CardTitle>
                <NetworkIcon /> Network
              </CardTitle>
              <CardBody>
                {vm.nics.length === 0 ? (
                  <span
                    style={{
                      color: "var(--pf-t--global--text--color--subtle)",
                    }}
                  >
                    No network adapters
                  </span>
                ) : (
                  <Table
                    aria-label="NICs table"
                    variant="compact"
                    borders={false}
                  >
                    <Thead>
                      <Tr>
                        <Th
                          style={{
                            paddingLeft: 0,
                            paddingTop: 0,
                            fontSize: "var(--pf-t--global--font--size--sm)",
                          }}
                        >
                          Network
                        </Th>
                        <Th
                          style={{
                            paddingTop: 0,
                            fontSize: "var(--pf-t--global--font--size--sm)",
                          }}
                        >
                          MAC
                        </Th>
                      </Tr>
                    </Thead>
                    <Tbody>
                      {vm.nics.map((nic, index) => (
                        <Tr key={index}>
                          <Td style={{ paddingLeft: 0}}>
                            {nic.network || "—"}
                          </Td>
                          <Td>{nic.mac || "—"}</Td>
                        </Tr>
                      ))}
                    </Tbody>
                  </Table>
                )}
              </CardBody>
            </Card>
          </FlexItem>
        </Flex>
      </StackItem>

      <StackItem>
        <Card className="dashboard-card-border">
          <CardTitle>
            <StorageDomainIcon /> Storage
          </CardTitle>
          <CardBody>
            {vm.disks.length === 0 ? (
              <div>No disks attached</div>
            ) : (
              <Table aria-label="Disks table" variant="compact">
                <Thead>
                  <Tr>
                    <Th>File</Th>
                    <Th>Size</Th>
                    <Th>Bus</Th>
                    <Th>Mode</Th>
                    <Th>Shared</Th>
                    <Th>RDM</Th>
                  </Tr>
                </Thead>
                <Tbody>
                  {vm.disks.map((disk, index) => (
                    <Tr key={index}>
                      <Td>{disk.file || "—"}</Td>
                      <Td>
                        {disk.capacity ? formatCapacity(disk.capacity) : "—"}
                      </Td>
                      <Td>{disk.bus || "—"}</Td>
                      <Td>{disk.mode || "—"}</Td>
                      <Td>{disk.shared ? "Yes" : "No"}</Td>
                      <Td>{disk.rdm ? "Yes" : "No"}</Td>
                    </Tr>
                  ))}
                </Tbody>
              </Table>
            )}
          </CardBody>
        </Card>
      </StackItem>

      <StackItem>
        <Card className="dashboard-card-border">
          <CardTitle>
            <ExclamationTriangleIcon /> Issues
          </CardTitle>
          <CardBody>
            {vm.issues && vm.issues.length > 0 ? (
              <Stack hasGutter>
                {vm.issues.map((issue, index) => (
                  <StackItem key={index}>
                    <Alert variant="warning" isInline isPlain title={issue} />
                  </StackItem>
                ))}
              </Stack>
            ) : (
              <span
                style={{ color: "var(--pf-t--global--text--color--subtle)" }}
              >
                No issues found
              </span>
            )}
          </CardBody>
        </Card>
      </StackItem>
    </Stack>
  );
};

VMDetail.displayName = "VMDetail";

export default VMDetail;
