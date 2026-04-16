export type FlushAction = 'none' | 'send-capture' | 'update-current-page';
export type PendingFlushDependency = 'none' | 'send';

export interface FlushDecisionInput {
  capturePrepared: boolean;
  hasArchived: boolean;
  sendInFlight: boolean;
  updateInFlight?: boolean;
  documentHidden?: boolean;
  currentPageId: number | null;
}

export function chooseFlushAction(input: FlushDecisionInput): FlushAction {
  if (input.sendInFlight || input.updateInFlight) {
    return 'none';
  }

  if (!input.hasArchived) {
    return input.capturePrepared ? 'send-capture' : 'none';
  }

  if (input.documentHidden) {
    return 'none';
  }

  return input.currentPageId !== null ? 'update-current-page' : 'none';
}

export function choosePendingFlushDependency(input: Pick<FlushDecisionInput, 'sendInFlight' | 'updateInFlight'>): PendingFlushDependency {
  if (input.sendInFlight) {
    return 'send';
  }

  return 'none';
}

export function shouldCommitMonitorUpdate(taskEpoch: number, currentEpoch: number, monitorPageId: number | null, currentPageId: number | null): boolean {
  return taskEpoch === currentEpoch && monitorPageId !== null && monitorPageId === currentPageId;
}

export function shouldClearAsyncState<T>(currentPromise: Promise<T> | null, settledPromise: Promise<T>): boolean {
  return currentPromise === settledPromise;
}
