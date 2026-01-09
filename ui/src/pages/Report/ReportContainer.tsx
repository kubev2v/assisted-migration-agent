import React from "react";
import { Stack } from "@patternfly/react-core";
import { useAppSelector } from "@shared/store";
import Header from "./Header";
import Report from "./Report";

const ReportContainer: React.FC = () => {
  const { inventory } = useAppSelector((state) => state.collector);

  if (!inventory) {
    return null;
  }

  return (
    <Stack hasGutter style={{ padding: "24px", width: "75%" }}>
      <Header />
      <Report inventory={inventory} />
    </Stack>
  );
};

ReportContainer.displayName = "ReportContainer";

export default ReportContainer;
