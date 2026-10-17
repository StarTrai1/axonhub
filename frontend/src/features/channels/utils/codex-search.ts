export const CODEX_SEARCH_MODELS = [
  'gpt-6.1-sol', 'gpt-6-astra', 'gpt-6-sol', 'gpt-6-luna',
  'gpt-5.6-sol', 'gpt-5.6-terra', 'gpt-5.6-luna',
] as const;

export function isCodexSearchModel(model: string): boolean {
  const parts = /^gpt-(\d+)(?:\.(\d+))?(?:-[a-z0-9][a-z0-9.-]*)?$/.exec(model.trim());
  if (!parts) return false;
  const major = Number(parts[1]);
  const minor = Number(parts[2] ?? 0);
  return Number.isSafeInteger(major) && (major > 5 || (major === 5 && minor >= 6));
}

export function codexSearchModelOptions(models: string[], selected: string): string[] {
  return [...new Set([...CODEX_SEARCH_MODELS, ...models, selected])].filter(isCodexSearchModel);
}
