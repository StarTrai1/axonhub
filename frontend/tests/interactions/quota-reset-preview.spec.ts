import { test, expect } from '@playwright/test';

for (const theme of ['light', 'dark']) {
  test(`Claude selected reset and metric colours remain readable in ${theme}`, async ({ page }, testInfo) => {
    const mutations: unknown[] = [];
    await page.route('**/admin/graphql', async (route) => {
      const body = route.request().postDataJSON();
      mutations.push(body.variables);
      await route.fulfill({ json: { data: { resetChannelQuotaNow: true } } });
    });
    await page.setViewportSize({ width: 480, height: 900 });
    await page.goto(`/tests/interactions/peer-regressions.html?case=quota-reset&lang=zh-CN&theme=${theme}`);
    const card = page.getByTestId('claude-quota-card');
    await expect(card).toContainText('完整额度重置');
    await expect(card).toContainText('Fable 周额度');
    const buttons = card.getByRole('button');
    await expect(buttons.nth(0)).toBeEnabled();
    await expect(buttons.nth(1)).toBeDisabled();
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    await page.screenshot({ animations: 'disabled', path: testInfo.outputPath(`codex-claude-reset-zh-${theme}.png`) });
    await buttons.nth(0).click();
    await expect.poll(() => mutations).toEqual([{ channelID: 'offline-channel', creditID: 'full' }]);
    await page.getByTestId('open-metric-colors').click();
    await expect(page.getByRole('dialog')).toBeVisible();
    await expect(page.getByRole('dialog')).toContainText('指标着色');
    await page.screenshot({ animations: 'disabled', path: testInfo.outputPath(`codex-metric-colors-zh-${theme}.png`) });
  });
}
