import { test, expect } from '@playwright/test';

for (const width of [390, 1280]) {
  test(`Claude OAuth catalog stays readable and preserves refreshed credentials at ${width}px`, async ({ page }, testInfo) => {
    test.setTimeout(60000);
    await page.setViewportSize({ width, height: 1100 });
    let fallback = false;
    let submitted: Record<string, unknown> | undefined;
    await page.route('**/admin/graphql', async (route) => {
      const body = route.request().postDataJSON();
      if (body.operationName === 'FetchModels') {
        expect(body.variables.input.channelID).toBe('channel-preview');
        expect(body.variables.input.apiKey).toBeUndefined();
        return route.fulfill({ json: { data: { fetchModels: {
          models: [{ id: 'fixture-new' }], fallback, error: null,
          fallbackReason: fallback ? 'UNAVAILABLE' : null,
        } } } });
      }
      if (body.operationName === 'UpdateChannel') {
        submitted = body.variables.input;
        return route.fulfill({ json: { data: { updateChannel: {
          id: 'channel-preview', name: 'Claude OAuth', type: 'claudecode', status: 'enabled',
          createdAt: '2026-11-24T00:00:00Z', updatedAt: '2026-11-24T00:00:00Z',
          ...submitted, credentials: { isOAuth: true, oauth: { accessToken: 'synthetic-refreshed-token' } },
        } } } });
      }
      return route.fulfill({ json: { data: { allChannelTags: [], proxyPresets: [], channelDisabledAPIKeys: [], queryChannels: { edges: [{ node: { id: 'channel-preview', name: 'Claude OAuth', type: 'claudecode', status: 'enabled', baseURL: 'https://api.anthropic.com', createdAt: '2026-11-24T00:00:00Z', updatedAt: '2026-11-24T00:00:00Z', supportedModels: ['fixture-old'], defaultTestModel: 'fixture-old', credentials: { isOAuth: true, oauth: { accessToken: 'synthetic-refreshed-token' } }, settings: {} }, cursor: 'fixture' }], totalCount: 1, pageInfo: { hasNextPage: false, hasPreviousPage: false, startCursor: 'fixture', endCursor: 'fixture' } } } } });
    });
    await page.goto('/tests/interactions/codex-channel.html?claude-catalog');
    await page.getByTestId('zh-locale').evaluate((el) => (el as HTMLButtonElement).click());
    await page.getByTestId('dark-theme').evaluate((el) => (el as HTMLButtonElement).click());
    await page.getByTestId('channel-fetch-models').click();
    await expect(page.getByTestId('claude-catalog-source')).toHaveText('上游模型（1）');
    await page.getByTestId('catalog-model-row').getByRole('checkbox').click();
    await page.getByRole('button', { name: /确定选中.*1/ }).click();
    await page.getByTestId('claude-catalog-source').scrollIntoViewIfNeeded();
    await page.screenshot({ animations: 'disabled', path: testInfo.outputPath(`codex-claude-catalog-${width}-dark.png`) });
    const panel = page.getByTestId('channel-side-panel');
    expect(await panel.evaluate((el) => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
    await page.getByTestId('close-fetched-models').click();
    fallback = true;
    await page.getByTestId('channel-fetch-models').click();
    await expect(page.getByTestId('claude-catalog-source')).toHaveText('预设列表 · 获取失败，请重试');
    await expect(page.getByTestId('clean-fetched-models')).toBeDisabled();
    await page.getByTestId('close-fetched-models').click();
    await page.getByTestId('channel-submit-button').click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    expect(submitted?.supportedModels).toEqual(['fixture-old', 'fixture-new']);
    expect(submitted?.credentials).toBeUndefined();
  });
}
