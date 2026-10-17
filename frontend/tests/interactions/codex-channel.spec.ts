import { test, expect } from '@playwright/test';

for (const width of [1280, 768, 390]) {
  test(`Codex authorization aligns and search-only saves at ${width}px`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 1100 });
    let saved = {
      id: 'channel-preview', createdAt: '2026-10-17T00:00:00Z', updatedAt: '2026-10-17T00:00:00Z',
      name: 'Codex fixture', type: 'codex', status: 'enabled', baseURL: 'https://chatgpt.com/backend-api/codex#',
      credentials: { apiKey: JSON.stringify({ access_token: 'synthetic', refresh_token: 'synthetic' }) },
      policies: { webSearch: 'auto' }, supportedModels: ['gpt-6-sol', 'gpt-5.5'], defaultTestModel: 'gpt-6-sol', settings: { codexSearchOnly: false, codexSearchModel: '' },
    };
    const settings: unknown[] = [];
    await page.route('**/fixture/channel', (route) => route.fulfill({ json: saved }));
    await page.route('**/admin/graphql', async (route) => {
      const body = route.request().postDataJSON();
      if (body.operationName === 'UpdateChannel') {
        settings.push({ enabled: body.variables.input.settings.codexSearchOnly, model: body.variables.input.settings.codexSearchModel });
        saved = { ...saved, ...body.variables.input };
        await route.fulfill({ json: { data: { updateChannel: saved } } });
      } else {
        await route.fulfill({ json: { data: {
          queryChannels: { edges: [{ node: saved, cursor: 'fixture' }], totalCount: 1, pageInfo: { hasNextPage: false, hasPreviousPage: false, startCursor: 'fixture', endCursor: 'fixture' } },
          allChannelTags: [], channelDisabledAPIKeys: [], proxyPresets: [],
        } } });
      }
    });
    await page.goto('/tests/interactions/codex-channel.html');
    const section = page.getByTestId('codex-authorization-section');
    const tabs = section.getByRole('tablist');
    await expect(tabs).toBeVisible();
    const outer = await section.boundingBox();
    const inner = await tabs.boundingBox();
    expect(outer).not.toBeNull();
    expect(inner).not.toBeNull();
    expect(Math.abs(inner!.x - outer!.x)).toBeLessThan(2);
    expect(Math.abs(inner!.width - outer!.width)).toBeLessThan(2);
    await section.getByRole('tab', { name: 'auth.json', exact: true }).click();
    const input = page.getByTestId('codex-auth-json-input');
    await expect(input).toBeVisible();
    expect(await section.evaluate((element) => element.scrollWidth <= element.clientWidth + 1)).toBe(true);
    await section.getByRole('tab').first().click();
    const toggle = page.getByTestId('codex-search-only-toggle');
    await expect(toggle).toHaveAttribute('aria-pressed', 'false');
    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-pressed', 'true');
    const modelSelect = page.getByTestId('codex-search-model-select');
    await expect(modelSelect).toContainText('Follow request model');
    await modelSelect.click();
    await expect(page.getByRole('option', { name: 'gpt-5.5', exact: true })).toHaveCount(0);
    await page.getByRole('option', { name: 'gpt-6.1-sol', exact: true }).click();
    await expect(modelSelect).toContainText('gpt-6.1-sol');
    await page.screenshot({ path: testInfo.outputPath('codex-search-model.png') });
    const cards = page.getByTestId('codex-policy-cards');
    await cards.scrollIntoViewIfNeeded();
    await expect(page.getByTestId('web-search-policy').getByRole('radio')).toHaveCount(2);
    await expect(page.getByTestId('remote-compaction-policy').getByRole('radio')).toHaveCount(3);
    await expect(page.getByTestId('web-search-policy-native')).toBeChecked();
    const panels = cards.locator(':scope > div');
    const sizes = await panels.evaluateAll((elements) => elements.map((element) => ({ width: element.getBoundingClientRect().width, height: element.getBoundingClientRect().height, overflow: element.scrollWidth > element.clientWidth + 1 })));
    expect(sizes).toHaveLength(2);
    expect(Math.abs(sizes[0].width - sizes[1].width)).toBeLessThan(2);
    expect(sizes.some((size) => size.overflow)).toBe(false);
    expect(await cards.locator('label').evaluateAll((labels) => labels.every((label) => label.scrollWidth <= label.clientWidth + 1))).toBe(true);
    if (width === 1280) expect(Math.abs(sizes[0].height - sizes[1].height)).toBeLessThan(2);
    await page.screenshot({ path: testInfo.outputPath('codex-channel.png') });
    await page.getByTestId('channel-submit-button').click();
    await expect.poll(() => settings).toEqual([{ enabled: true, model: 'gpt-6.1-sol' }]);
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await page.getByTestId('reopen-codex').click();
    await expect(toggle).toHaveAttribute('aria-pressed', 'true');
    await expect(modelSelect).toContainText('gpt-6.1-sol');
    await modelSelect.click();
    await page.getByRole('option', { name: 'Follow request model', exact: true }).click();
    await toggle.click();
    await page.getByTestId('channel-submit-button').click();
    await expect.poll(() => settings).toEqual([{ enabled: true, model: 'gpt-6.1-sol' }, { enabled: false, model: '' }]);
  });
}
