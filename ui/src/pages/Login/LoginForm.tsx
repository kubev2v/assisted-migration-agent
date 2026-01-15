import { ReactNode, useState } from "react";
import {
    ActionGroup,
    Button,
    Flex,
    FlexItem,
    Form,
    FormGroup,
    TextInput,
} from "@patternfly/react-core";
import { Credentials } from "@models";

interface LoginFormProps {
    collect: (credentials: Credentials) => void;
    cancelCollection?: () => void;
    isLoading?: boolean;
    isDisabled?: boolean;
    dataSharingComponent: ReactNode;
    informationComponent: ReactNode;
    progressComponent?: ReactNode;
}

function LoginForm({
    collect,
    cancelCollection,
    isLoading = false,
    isDisabled = false,
    dataSharingComponent,
    informationComponent,
    progressComponent,
}: LoginFormProps) {
    const [url, setUrl] = useState("");
    const [username, setUsername] = useState("");
    const [password, setPassword] = useState("");

    const isFormValid =
        url.trim() !== "" && username.trim() !== "" && password.trim() !== "";

    const handleSubmit = (event: React.FormEvent) => {
        event.preventDefault();
        if (isFormValid && !isLoading && !isDisabled) {
            collect({ url, username, password });
        }
    };

    return (
        <Form onSubmit={handleSubmit}>
            <FormGroup label="vCenter URL" isRequired fieldId="vcenter-url">
                <TextInput
                    id="vcenter-url"
                    type="url"
                    value={url}
                    onChange={(_event, value) => setUrl(value)}
                    placeholder="https://vcenter_server_ip_address_or_fqdn"
                    isRequired
                    isDisabled={isDisabled || isLoading}
                />
            </FormGroup>

            <FormGroup label="Username" isRequired fieldId="username">
                <TextInput
                    id="username"
                    type="text"
                    value={username}
                    onChange={(_event, value) => setUsername(value)}
                    placeholder="su.do@redhat.com"
                    isRequired
                    isDisabled={isDisabled || isLoading}
                />
            </FormGroup>

            <FormGroup label="Password" isRequired fieldId="password">
                <TextInput
                    id="password"
                    type="password"
                    value={password}
                    onChange={(_event, value) => setPassword(value)}
                    placeholder="su.do@redhat.com"
                    isRequired
                    isDisabled={isDisabled || isLoading}
                />
            </FormGroup>

            <Flex direction={{ default: "column" }} gap={{ default: "gapMd" }}>
                <FlexItem>{dataSharingComponent}</FlexItem>
                <FlexItem style={{ marginTop: "10px", marginBottom: "10px" }}>
                    {informationComponent}
                </FlexItem>
            </Flex>

            <ActionGroup style={{ marginTop: 0 }}>
                <Flex
                    alignItems={{ default: "alignItemsCenter" }}
                    gap={{ default: "gapMd" }}
                    style={{ minHeight: "36px" }}
                >
                    <FlexItem>
                        <Button
                            variant="primary"
                            type="submit"
                            isLoading={isLoading}
                            isDisabled={!isFormValid || isDisabled || isLoading}
                        >
                            Create assessment report
                        </Button>
                    </FlexItem>
                    {isLoading && cancelCollection && (
                        <FlexItem>
                            <Button variant="link" onClick={cancelCollection}>
                                Cancel
                            </Button>
                        </FlexItem>
                    )}
                </Flex>
                {isLoading && progressComponent && (
                    <div style={{ marginTop: "16px" }}>
                        {progressComponent}
                    </div>
                )}
            </ActionGroup>
        </Form>
    );
}

export default LoginForm;
export type { LoginFormProps };
