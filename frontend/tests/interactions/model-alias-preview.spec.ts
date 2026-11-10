import { test, expect } from '@playwright/test';

for (const width of [1280, 768, 390]) {
  test(`Model aliases retain their upstream grouping at ${width}px`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 1000 });
    const longAlias = 'relay/production/compatible/gpt-6-sol-ultrafast-long-model-alias';
    await page.route('**/admin/graphql', async (route) => {
      const body = route.request().postDataJSON();
      if (body.operationName === 'PreviewChannelModelEntries') {
        expect(Object.keys(body.variables.input)).toEqual(['supportedModels', 'settings']);
        await route.fulfill({ json: { data: { previewChannelModelEntries: [
          { requestModel: 'gpt-6-sol-fast', actualModel: 'gpt-6-sol', source: 'mapping' },
          { requestModel: longAlias, actualModel: 'gpt-6-sol', source: 'prefix' },
          { requestModel: 'gpt-5.5', actualModel: 'gpt-5.5', source: 'direct' },
        ] } } });
      } else {
        await route.fulfill({ json: { data: { allChannelTags: [], channelDisabledAPIKeys: [], proxyPresets: [] } } });
      }
    });
    await page.goto('/tests/interactions/codex-channel.html?models=1');
    const search = page.getByPlaceholder('Search models or aliases...');
    await search.fill('ultrafast');
    const aliases = page.getByTestId('model-aliases');
    await expect(aliases).toHaveCount(1);
    await expect(aliases).toContainText('gpt-6-sol-fast');
    await expect(aliases).toContainText(longAlias);
    await expect(aliases).toContainText('Original name is not exposed');
    await expect(aliases.locator('..')).toContainText('gpt-6-sol');
    expect(await aliases.evaluate((element) => element.scrollWidth <= element.clientWidth + 1)).toBe(true);
    await page.screenshot({ animations: 'disabled', path: testInfo.outputPath('codex-aliases-en-light.png') });
    await page.getByTestId('zh-locale').evaluate((element) => (element as HTMLButtonElement).click());
    await expect(aliases).toContainText('原始名称未对外暴露');
    await page.getByTestId('dark-theme').evaluate((element) => (element as HTMLButtonElement).click());
    await page.screenshot({ animations: 'disabled', path: testInfo.outputPath('codex-aliases-zh-dark.png') });
  });
}
