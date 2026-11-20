import { test, expect } from '@playwright/test';

for (const width of [390, 1280]) {
  test(`Channel schedule preserves local policies at ${width}px`, async ({ page }, testInfo) => {
    let policies: Record<string, unknown> | undefined;
    await page.route('**/admin/graphql', async (route) => {
      const request = route.request().postDataJSON();
      if (request.variables?.input?.policies) {
        policies = request.variables.input.policies;
        await route.fulfill({ json: { data: { updateChannel: null } } });
      } else await route.fulfill({ json: { data: {} } });
    });
    await page.setViewportSize({ width, height: 1000 });
    await page.goto('/tests/interactions/codex-channel.html?availability');
    await page.getByTestId('zh-locale').evaluate((element: HTMLElement) => element.click());
    await page.getByTestId('dark-theme').evaluate((element: HTMLElement) => element.click());
    const dialog = page.getByRole('dialog');
    const disable = page.getByTestId('availability-scheduled-disable-cron');
    const reenable = page.getByTestId('availability-scheduled-reenable-cron');
    await expect(disable).toHaveValue('0 22 * * *');
    await expect(reenable).toHaveValue('0 6 * * *');
    await disable.fill('0 23 * * *');
    await expect.poll(() => dialog.evaluate((node) => node.scrollWidth <= node.clientWidth + 1)).toBe(true);
    await reenable.scrollIntoViewIfNeeded();
    await page.screenshot({ animations: 'disabled', path: testInfo.outputPath(`codex-availability-${width}-dark.png`) });
    await dialog.getByRole('button', { name: /保存|Save/ }).click();
    await expect.poll(() => policies).toMatchObject({
      webSearch: 'native', imageGeneration: 'other_channels', codexMemory: 'only', codexSpeed: 'fast',
      scheduledDisable: { disableCron: '0 23 * * *', reenableCron: '0 6 * * *', disableTimezone: 'Asia/Shanghai' },
    });
  });
}
