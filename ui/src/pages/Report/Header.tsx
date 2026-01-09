import React from "react";
import { Content, StackItem, Title } from "@patternfly/react-core";

const Header: React.FC = () => {
  return (
    <>
      <StackItem>
        <Title headingLevel="h1" size="2xl">
          Migration Assessment Report
        </Title>
      </StackItem>

      <StackItem>
        <Content component="p">
          Presenting the information we were able to fetch from the discovery
          process
        </Content>
      </StackItem>
    </>
  );
};

Header.displayName = "Header";

export default Header;
