import { useEffect, useRef, useCallback } from "react";
import { Spinner } from "@patternfly/react-core";
import { Login } from "@pages/Login";
import { Report } from "@pages/Report";
import { Layout } from "@shared/components";
import { useAppDispatch, useAppSelector } from "@shared/store";
import { fetchAgentStatus, changeAgentMode } from "@shared/reducers/agentSlice";
import { fetchInventory, fetchCollectorStatus, startCollection, stopCollection, resetCollector } from "@shared/reducers/collectorSlice";
import { collectorPollingService, isCollectorRunning } from "@shared/services";
import { AgentStatusModeEnum, AgentModeRequestModeEnum, CollectorStatusStatusEnum } from "@generated/index";
import { Credentials } from "@models";

function App() {
    const dispatch = useAppDispatch();
    const pollingStarted = useRef(false);

    const { mode, initialized: agentInitialized } = useAppSelector(
        (state) => state.agent
    );
    const { inventory, status, error, initialized: collectorInitialized } = useAppSelector(
        (state) => state.collector
    );

    const isLoading = !agentInitialized || !collectorInitialized;
    const isDataShared = mode === AgentStatusModeEnum.Connected;
    const isCollecting = isCollectorRunning(status);

    const handleCollect = useCallback(async (credentials: Credentials, dataShared: boolean) => {
        dispatch(startCollection({
            url: credentials.url,
            username: credentials.username,
            password: credentials.password,
        }));
        collectorPollingService.start(1500);

        // Fire and forget after collection starts - errors won't affect collection
        // TODO: agent's status should be polled just like the inventory and rendered somewhere
        if (dataShared) {
            try {
                await dispatch(changeAgentMode(AgentModeRequestModeEnum.Connected));
            } catch {
                // Ignore agent mode errors
            }
        }
    }, [dispatch]);

    const handleCancel = useCallback(() => {
        collectorPollingService.stop();
        dispatch(stopCollection());
    }, [dispatch]);

    // Init polling service
    useEffect(() => {
        collectorPollingService.init(dispatch);
    }, [dispatch]);

    // Reset collector and fetch initial data
    useEffect(() => {
        dispatch(resetCollector()).catch(() => {});
        dispatch(fetchAgentStatus());
        dispatch(fetchCollectorStatus());
        dispatch(fetchInventory());
    }, [dispatch]);

    // Start polling if collector was already running on load
    useEffect(() => {
        if (collectorInitialized && isCollecting && !pollingStarted.current) {
            pollingStarted.current = true;
            collectorPollingService.start(1500);
        }
    }, [collectorInitialized, isCollecting]);

    // Fetch inventory when collection completes
    useEffect(() => {
        if (status === CollectorStatusStatusEnum.Collected) {
            dispatch(fetchInventory());
        }
    }, [status, dispatch]);

    if (isLoading) {
        return (
            <Layout>
                <Spinner size="xl" />
            </Layout>
        );
    }

    const hasInventory = inventory && inventory.vcenter_id;

    return (
        <Layout>
            {hasInventory ? <Report /> : (
                <Login
                    version="v1.0.0"
                    isDataShared={isDataShared}
                    isCollecting={isCollecting}
                    status={status}
                    error={error}
                    onCollect={handleCollect}
                    onCancel={handleCancel}
                />
            )}
        </Layout>
    );
}

export default App;
