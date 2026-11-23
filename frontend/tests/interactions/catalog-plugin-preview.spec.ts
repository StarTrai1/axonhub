import { test, expect } from '@playwright/test';

for (const width of [390, 1280]) {
  test(`Catalog selected prices remain readable and manual prices stay fixed at ${width}px`, async ({ page }, testInfo) => {
    let applied: Array<{ modelID: string; prices: Array<{ field: string; action: string; expected: number }> }> = [];
    const offer = { canonical: 'example/model', provider: 'example', modelID: 'model', name: '示例模型', developer: 'example', identity: 'fixture', type: 'chat', rates: [
      { field: 'input', base: 1.25, tiers: [] }, { field: 'output', base: 3, tiers: [] },
    ] };
    const row = { modelID: 'model', name: '示例模型', members: [{ id: 'model', identity: 'fixture' }], routes: [],
      version: 'saved', mergeTarget: '', mergeTargetOccupied: false, archived: false, changed: true, offers: [offer], preferred: offer,
      prices: [{ field: 'input', value: 9, follow: false, canonical: '', provider: '', modelID: '' }, { field: 'output', value: 2, follow: true, canonical: offer.canonical, provider: 'example', modelID: 'model' }] };
    await page.route('**/admin/graphql', async (route) => {
      const body = route.request().postDataJSON();
      if (body.query.includes('mutation ApplyCatalogImport')) {
        applied = body.variables.selections;
        return route.fulfill({ json: { data: { applyCatalogImport: [{ modelID: 'model', status: 'applied' }] } } });
      }
      return route.fulfill({ json: { data: { catalogImportPreview: { providers: ['example'], source: 'upstream', upstreamURL: 'https://example.test/catalog', revision: 'fixture-v1', rows: [row] } } } });
    });
    await page.setViewportSize({ width, height: 1000 });
    await page.goto('/tests/interactions/peer-regressions.html?case=catalog-import&lang=zh-CN&theme=dark');
    const dialog = page.getByRole('dialog');
    await dialog.getByTestId('catalog-import-expand-model').click();
    await dialog.getByRole('checkbox', { name: 'model', exact: true }).check();
    await expect.poll(() => dialog.evaluate((node) => node.scrollWidth <= node.clientWidth + 1)).toBe(true);
    await page.screenshot({ animations: 'disabled', path: testInfo.outputPath(`codex-catalog-${width}-dark.png`) });
    await dialog.getByTestId('catalog-import-apply').click();
    await expect.poll(() => applied.length).toBe(1);
    expect(applied[0].prices).toEqual([expect.objectContaining({ field: 'output', action: 'FOLLOW', expected: 3 })]);
  });

  test(`Plugin schedule edits stay scoped and readable at ${width}px`, async ({ page }, testInfo) => {
    const updates: unknown[] = [];
    await page.route('**/admin/graphql', async (route) => {
      const body = route.request().postDataJSON();
      if (body.query.includes('mutation')) updates.push(body.variables);
      await route.fulfill({ json: { data: { pluginTasks: [{ key: 'daily-cleanup', cron: '0 6 * * *', timezone: 'Asia/Shanghai', paused: false, overridden: false,
        nextRunAt: '2026-11-24T22:00:00Z', runs: [{ id: 'run-1', startedAt: '2026-11-23T22:00:00Z', status: 'success', scheduledAt: '2026-11-23T22:00:00Z' }] }], updatePluginTask: {} } } });
    });
    await page.setViewportSize({ width, height: 1000 });
    await page.goto('/tests/interactions/peer-regressions.html?case=plugin-tasks&lang=zh-CN&theme=dark');
    const panel = page.getByTestId('plugin-task-preview');
    await expect(panel.getByText('daily-cleanup')).toBeVisible();
    await panel.locator('summary').first().click();
    await panel.locator('input[name=cron]').fill('0 7 * * *');
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    await page.screenshot({ animations: 'disabled', path: testInfo.outputPath(`codex-plugin-task-${width}-dark.png`) });
    await panel.getByRole('button', { name: /保存/ }).click();
    await expect.poll(() => updates).toContainEqual({ id: 'offline-plugin', key: 'daily-cleanup', paused: false, cron: '0 7 * * *', timezone: 'Asia/Shanghai' });
  });
}
