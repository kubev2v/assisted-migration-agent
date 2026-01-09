import type { AppDispatch } from '@shared/store';
import { fetchCollectorStatus } from '@shared/reducers/collectorSlice';
import { CollectorStatusStatusEnum } from '@generated/index';

export function isCollectorRunning(status: CollectorStatusStatusEnum): boolean {
  return (
    status === CollectorStatusStatusEnum.Connecting ||
    status === CollectorStatusStatusEnum.Connected ||
    status === CollectorStatusStatusEnum.Collecting
  );
}

class CollectorPollingService {
  private intervalId: ReturnType<typeof setInterval> | null = null;
  private dispatch: AppDispatch | null = null;

  init(dispatch: AppDispatch): void {
    this.dispatch = dispatch;
  }

  start(periodMs: number = 1500): void {
    if (this.intervalId) {
      return; // Already running
    }

    this.poll(); // Immediate first poll
    this.intervalId = setInterval(() => this.poll(), periodMs);
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

  private async poll(): Promise<void> {
    if (!this.dispatch) return;

    const result = await this.dispatch(fetchCollectorStatus());

    if (fetchCollectorStatus.fulfilled.match(result)) {
      const status = result.payload?.status;
      if (status && !isCollectorRunning(status)) {
        this.stop();
      }
    }

    if (fetchCollectorStatus.rejected.match(result)) {
      this.stop();
    }
  }
}

export const collectorPollingService = new CollectorPollingService();
