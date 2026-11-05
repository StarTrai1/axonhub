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
        settings.push({ enabled: body.variables.input.settings.codexSearchOnly, model: body.variables.input.settings.codexSearchModel, image: body.variables.input.settings.codexImageOnly, imageModel: body.variables.input.settings.codexImageModel, speed: body.variables.input.policies.codexSpeed, imagePolicy: body.variables.input.policies.imageGeneration, policy: body.variables.input.policies.webSearch });
        saved = { ...saved, ...body.variables.input };
        await route.fulfill({ json: { data: { updateChannel: saved } } });
      } else if (body.operationName === 'FetchModels') {
        await route.fulfill({ json: { data: { fetchModels: { models: [{ id: 'gpt-image-2.6-sunburst' }, { id: 'gpt-image-2.5-flare' }], error: null } } } });
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
    await expect(toggle).toHaveAttribute('aria-checked', 'false');
    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-checked', 'true');
    const modelSelect = page.getByTestId('codex-search-model-select');
    await expect(modelSelect).toContainText('Follow request model');
    await modelSelect.click();
    await expect(page.getByRole('option', { name: 'gpt-5.5', exact: true })).toHaveCount(0);
    await page.getByRole('option', { name: 'gpt-6.1-sol', exact: true }).click();
    await expect(modelSelect).toContainText('gpt-6.1-sol');
    const imageToggle = page.getByTestId('codex-image-only-toggle');
    await imageToggle.click();
    await page.getByTestId('channel-fetch-models').click();
    const imageModel = page.getByTestId('codex-image-model-select');
    await imageModel.scrollIntoViewIfNeeded();
    await imageModel.click();
    await page.getByRole('option', { name: 'gpt-image-2.6-sunburst', exact: true }).click();
    await expect(imageModel).toContainText('gpt-image-2.6-sunburst');
    for (const tool of ['search', 'image']) {
      await page.getByTestId(`codex-${tool}-only-label`).hover();
      await page.waitForTimeout(800);
      await expect(page.getByRole('tooltip')).toHaveCount(0);
      await page.getByTestId(`codex-${tool}-only-tip`).hover();
      await expect(page.getByRole('tooltip')).toBeVisible();
      await page.mouse.move(0, 0);
      await expect(page.getByRole('tooltip')).toHaveCount(0);
      const row = page.getByTestId(`codex-${tool}-only-setting`);
      expect(await row.evaluate((element) => element.scrollWidth <= element.clientWidth + 1)).toBe(true);
    }
    await expect(page.getByTestId('codex-tool-settings').locator('p')).toHaveCount(0);
    await page.screenshot({ path: testInfo.outputPath('codex-search-model.png') });
    const policies = page.getByTestId('codex-policy-settings');
    await policies.scrollIntoViewIfNeeded();
    const searchPolicy = page.getByTestId('web-search-policy');
    const compactPolicy = page.getByTestId('remote-compaction-policy');
    const speed = page.getByTestId('codex-speed-policy');
    const imagePolicy = page.getByTestId('image-generation-policy');
    await speed.click();
    await page.getByTestId('codex-speed-policy-fast').click();
    await expect(speed).toContainText('Fast');
    await speed.click();
    await page.getByTestId('codex-speed-policy-ultrafast').click();
    await expect(speed).toContainText('Ultrafast');
    await expect(imagePolicy).toContainText('Native images');
    await expect(searchPolicy).toContainText('Native search');
    await searchPolicy.click();
    await expect(page.getByRole('menuitemradio')).toHaveCount(3);
    await page.keyboard.press('Escape');
    await expect(searchPolicy).toBeFocused();
    await compactPolicy.click();
    await expect(page.getByRole('menuitemradio')).toHaveCount(3);
    await page.keyboard.press('Escape');
    const rows = policies.locator(':scope > div');
    const sizes = await rows.evaluateAll((elements) => elements.map((element) => ({ width: element.getBoundingClientRect().width, height: element.getBoundingClientRect().height, overflow: element.scrollWidth > element.clientWidth + 1 })));
    expect(sizes).toHaveLength(5);
    expect(sizes.some((size) => size.overflow)).toBe(false);
    expect(sizes.every((size) => size.height < 100)).toBe(true);
    expect(await policies.evaluate((element) => element.getBoundingClientRect().height)).toBeLessThan(360);
    expect((await searchPolicy.boundingBox())!.width).toBeLessThan(200);
    expect((await compactPolicy.boundingBox())!.width).toBeLessThan(200);
    await page.screenshot({ path: testInfo.outputPath('codex-channel.png') });
    await page.getByTestId('dark-theme').evaluate((element) => (element as HTMLButtonElement).click());
    await expect(page.locator('html')).toHaveClass(/dark/);
    await expect(page.locator('html')).toHaveClass(/claude/);
    await expect(page.getByRole('dialog')).toHaveCSS('background-color', 'oklch(0.22 0.002 106)');
    await page.screenshot({ path: testInfo.outputPath('codex-channel-dark.png') });
    await page.getByTestId('codex-image-only-setting').scrollIntoViewIfNeeded();
    await page.screenshot({ path: testInfo.outputPath('codex-tools-dark.png') });
    await page.getByTestId('channel-submit-button').click();
    await expect.poll(() => settings).toEqual([{ enabled: true, model: 'gpt-6.1-sol', image: true, imageModel: 'gpt-image-2.6-sunburst', speed: 'ultrafast', imagePolicy: 'native', policy: 'native' }]);
    expect(saved.supportedModels).toContain('gpt-image-2.6-sunburst');
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await page.getByTestId('reopen-codex').click();
    await expect(toggle).toHaveAttribute('aria-checked', 'true');
    await expect(modelSelect).toContainText('gpt-6.1-sol');
    await modelSelect.click();
    await page.getByRole('option', { name: 'Follow request model', exact: true }).click();
    await searchPolicy.scrollIntoViewIfNeeded();
    await searchPolicy.click();
    await page.getByTestId('web-search-policy-other_channels').click();
    await expect(toggle).toHaveAttribute('aria-checked', 'false');
    await expect(imageToggle).toHaveAttribute('aria-checked', 'true');
    await expect(imageModel).toContainText('gpt-image-2.6-sunburst');
    await expect(speed).toContainText('Ultrafast');
    await page.getByTestId('channel-submit-button').click();
    await expect.poll(() => settings).toEqual([{ enabled: true, model: 'gpt-6.1-sol', image: true, imageModel: 'gpt-image-2.6-sunburst', speed: 'ultrafast', imagePolicy: 'native', policy: 'native' }, { enabled: false, model: '', image: true, imageModel: 'gpt-image-2.6-sunburst', speed: 'ultrafast', imagePolicy: 'native', policy: 'other_channels' }]);
    expect(saved.supportedModels).toContain('gpt-image-2.6-sunburst');
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await page.getByTestId('reopen-codex').click();
    await searchPolicy.scrollIntoViewIfNeeded();
    await expect(searchPolicy).toContainText('Other channels');
    await expect(toggle).toHaveAttribute('aria-checked', 'false');
    await page.getByTestId('zh-locale').evaluate((element) => (element as HTMLButtonElement).click());
    await expect(searchPolicy).toContainText('使用其他渠道');
    await page.screenshot({ path: testInfo.outputPath('codex-channel-zh-dark.png') });
    await toggle.click();
    await page.getByTestId('codex-tool-settings').scrollIntoViewIfNeeded();
    await page.screenshot({ path: testInfo.outputPath('codex-tools-zh-dark.png') });
    await page.getByTestId('light-theme').evaluate((element) => (element as HTMLButtonElement).click());
    await page.screenshot({ path: testInfo.outputPath('codex-tools-zh-light.png') });
    await policies.scrollIntoViewIfNeeded();
    await page.screenshot({ path: testInfo.outputPath('codex-policies-zh-light.png') });
    await imagePolicy.click();
    await page.getByTestId('image-generation-policy-other_channels').click();
    await expect(imageToggle).toHaveAttribute('aria-checked', 'false');
    await expect(toggle).toHaveAttribute('aria-checked', 'true');
  });
}
