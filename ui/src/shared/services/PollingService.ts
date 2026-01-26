import type { AppDispatch } from "@shared/store";
import { fetchCollectorStatus } from "@shared/reducers/collectorSlice";
import { fetchAgentStatus } from "@shared/reducers/agentSlice";
import { CollectorStatusStatusEnum } from "@generated/index";

export function isCollectorRunning(status: CollectorStatusStatusEnum): boolean {
  return (
    status === CollectorStatusStatusEnum.Connecting ||
    status === CollectorStatusStatusEnum.Connected ||
    status === CollectorStatusStatusEnum.Collecting ||
    status === CollectorStatusStatusEnum.Parsing
  );
}

type PollingAction = () => Promise<boolean>; // returns true to STOP

class PollingService {
  private intervalId: ReturnType<typeof setInterval> | null = null;

  constructor(private action: PollingAction) {}

  start(periodMs: number = 1500): void {
    if (this.intervalId) return;

    const poll = async () => {
      try {
        const stop = await this.action();
        if (stop) {
          this.stop();
        }
      } catch {
        this.stop();
      }
    };

    poll();
    this.intervalId = setInterval(poll, periodMs);
  }

  stop(): void {
    if (this.intervalId) {
      clearInterval(this.intervalId);
      this.intervalId = null;
    }
  }

  isRunning(): boolean {
    return this.intervalId !== null;
  }
}

export function createCollectorPollingService(dispatch: AppDispatch): PollingService {
  return new PollingService(async () => {
    const result = await dispatch(fetchCollectorStatus());

    if (fetchCollectorStatus.fulfilled.match(result)) {
      const status = result.payload?.status;
      if (status && !isCollectorRunning(status)) {
        return true;
      }
    }

    if (fetchCollectorStatus.rejected.match(result)) {
      return true;
    }

    return false;
  });
}

export function createAgentPollingService(dispatch: AppDispatch): PollingService {
  return new PollingService(async () => {
    const result = await dispatch(fetchAgentStatus());

    if (fetchAgentStatus.rejected.match(result)) {
      return true;
    }

    return false;
  });
}
