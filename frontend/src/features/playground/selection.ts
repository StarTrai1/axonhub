export type PlaygroundModelSource = 'channel' | 'model_gateway';

export type PlaygroundSelection = {
  modelSource: PlaygroundModelSource;
  selectedChannel: string;
  model: string;
};

export const defaultSelection: PlaygroundSelection = {
  modelSource: 'channel',
  selectedChannel: '',
  model: '',
};

const storageKey = (projectId: string, userId: string) => `axonhub_playground_selection_v2_${JSON.stringify([userId, projectId])}`;

export function readSelection(projectId: string | null, userId: string | null): PlaygroundSelection {
  if (!projectId || !userId) return { ...defaultSelection };

  try {
    // Legacy selections have no owner and must not be adopted by a new login.
    const stored = localStorage.getItem(storageKey(projectId, userId));
    if (!stored) return { ...defaultSelection };
    const value: unknown = JSON.parse(stored);
    if (
      typeof value !== 'object' ||
      value === null ||
      !('modelSource' in value) ||
      !('selectedChannel' in value) ||
      !('model' in value) ||
      (value.modelSource !== 'channel' && value.modelSource !== 'model_gateway') ||
      typeof value.selectedChannel !== 'string' ||
      typeof value.model !== 'string'
    ) {
      return { ...defaultSelection };
    }
    return {
      modelSource: value.modelSource,
      selectedChannel: value.selectedChannel,
      model: value.model,
    };
  } catch {
    return { ...defaultSelection };
  }
}

export function writeSelection(projectId: string | null, selection: PlaygroundSelection, userId: string | null): void {
  if (!projectId || !userId) return;
  try {
    localStorage.setItem(storageKey(projectId, userId), JSON.stringify(selection));
  } catch {
    // Storage can be disabled or full; the in-memory selection still works.
  }
}

export function resolveSelection(
  selection: PlaygroundSelection,
  channels: { value: string; models: string[] }[],
  gatewayModels: string[],
  canUseModelGateway: boolean
): PlaygroundSelection {
  const channel = channels.find((item) => item.value === selection.selectedChannel) ?? channels[0];
  const selectedChannel = channel?.value ?? '';
  const modelSource = selection.modelSource === 'model_gateway' && canUseModelGateway ? 'model_gateway' : 'channel';
  const models = modelSource === 'model_gateway' ? gatewayModels : (channel?.models ?? []);
  const model = models.includes(selection.model) ? selection.model : (models[0] ?? '');

  return { modelSource, selectedChannel, model };
}
