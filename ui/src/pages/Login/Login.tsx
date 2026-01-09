import { useState } from "react";
import {
  Card,
  CardBody,
  CardHeader,
  Content,
  ContentVariants,
  Divider,
  Flex,
  FlexItem,
  Popover,
  Title,
} from "@patternfly/react-core";
import {
  InfoCircleIcon,
  OutlinedQuestionCircleIcon,
} from "@patternfly/react-icons";
import {
  Information,
  DataSharing,
  PrivacyNote,
  RedHatLogo,
} from "@shared/components";
import { CollectorStatusStatusEnum } from "@generated/index";
import { ApiError } from "@shared/reducers/collectorSlice";
import { Credentials } from "@models";
import VCenterLoginForm from "./VCenterLoginForm";
import CollectionProgress from "./VCenterLoginForm/CollectionProgress";

interface LoginProps {
  version?: string;
  isDataShared: boolean;
  isCollecting: boolean;
  status: CollectorStatusStatusEnum;
  error: ApiError | null;
  onCollect: (credentials: Credentials, isDataShared: boolean) => void;
  onCancel: () => void;
}

function Login({
  version,
  isDataShared: initialIsDataShared,
  isCollecting,
  status,
  error,
  onCollect,
  onCancel,
}: LoginProps) {
  const [isDataShared, setIsDataShared] = useState(initialIsDataShared);

  const getProgressInfo = () => {
    switch (status) {
      case CollectorStatusStatusEnum.Connecting:
        return { percentage: 25, statusText: "Connecting to vCenter..." };
      case CollectorStatusStatusEnum.Connected:
        return { percentage: 50, statusText: "Connected, starting collection..." };
      case CollectorStatusStatusEnum.Collecting:
        return { percentage: 75, statusText: "Collecting inventory data..." };
      default:
        return { percentage: 0, statusText: "" };
    }
  };

  const progressInfo = getProgressInfo();

  const handleCollect = (credentials: Credentials) => {
    onCollect(credentials, isDataShared);
  };

  return (
    <Card
      style={{
        maxWidth: "40rem",
        width: "100%",
        maxHeight: "90vh",
        overflowY: "auto",
        borderRadius: "8px",
      }}
    >
      <CardHeader>
        <Flex direction={{ default: "column" }} gap={{ default: "gapMd" }}>
          <FlexItem>
            <RedHatLogo />
          </FlexItem>

          <Flex
            justifyContent={{
              default: "justifyContentSpaceBetween",
            }}
          >
            <FlexItem>
              <Title headingLevel="h1" size="2xl">
                Migration assessment
              </Title>
            </FlexItem>
            <FlexItem>
              <Content component={ContentVariants.small}>
                Agent ver. {version}
              </Content>
            </FlexItem>
          </Flex>

          <FlexItem>
            <Flex
              gap={{ default: "gapSm" }}
              alignItems={{ default: "alignItemsCenter" }}
            >
              <FlexItem>
                <Content component={ContentVariants.p}>
                  Migration Discovery VM
                </Content>
              </FlexItem>
              <FlexItem>
                <Popover bodyContent="The Migration Discovery VM collects infrastructure data from your vCenter environment to generate a migration assessment report.">
                  <OutlinedQuestionCircleIcon style={{ color: "#000000" }} />
                </Popover>
              </FlexItem>
            </Flex>
          </FlexItem>

          <FlexItem>
            <Title headingLevel="h2" size="xl">
              vCenter login
            </Title>
          </FlexItem>

          <FlexItem>
            <Flex
              gap={{ default: "gapSm" }}
              alignItems={{
                default: "alignItemsFlexStart",
              }}
            >
              <FlexItem>
                <InfoCircleIcon style={{ color: "#007bff" }} />
              </FlexItem>
              <FlexItem>
                <strong>Access control</strong>
              </FlexItem>
              <Flex
                direction={{ default: "column" }}
                gap={{ default: "gapXs" }}
              >
                <FlexItem>
                  <Content component={ContentVariants.p}>
                    A VMware user account with read-only permissions is
                    sufficient for secure access during the discovery process.
                  </Content>
                </FlexItem>
              </Flex>
            </Flex>
          </FlexItem>
        </Flex>
      </CardHeader>

      <Divider
        style={
          {
            "--pf-v6-c-divider--Height": "8px",
            "--pf-v6-c-divider--BackgroundColor": "#f5f5f5",
          } as React.CSSProperties
        }
      />

      <CardBody>
        <VCenterLoginForm
          collect={handleCollect}
          cancelCollection={onCancel}
          isLoading={isCollecting}
          isDataShared={isDataShared}
          dataSharingComponent={
            <DataSharing
              variant="checkbox"
              isChecked={isDataShared}
              onChange={setIsDataShared}
              isDisabled={isCollecting}
            />
          }
          informationComponent={
            <Information error={error}>
              <PrivacyNote />
            </Information>
          }
          progressComponent={
            <CollectionProgress
              percentage={progressInfo.percentage}
              statusText={progressInfo.statusText}
            />
          }
        />
      </CardBody>
    </Card>
  );
}

export default Login;
