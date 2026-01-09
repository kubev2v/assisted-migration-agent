import { ReactNode } from "react";
import { Backdrop, Bullseye } from "@patternfly/react-core";

interface LayoutProps {
    children: ReactNode;
}

function Layout({ children }: LayoutProps) {
    return (
        <>
            <Backdrop style={{ zIndex: 0 }} />
            <Bullseye style={{ minHeight: "100vh" }}>{children}</Bullseye>
        </>
    );
}

export default Layout;
