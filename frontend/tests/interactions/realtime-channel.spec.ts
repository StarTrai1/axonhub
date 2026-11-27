import { test, expect } from '@playwright/test';

for (const width of [390, 1280]) {
  test(`Realtime settings persist with readable tooltips at ${width}px`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 1100 });
    let saved: Record<string, unknown> | undefined;
    await page.route('**/fixture/channel', (route) => route.fulfill({ json: saved }));
    await page.route('**/admin/graphql', async (route) => {
      const body = route.request().postDataJSON();
      if (body.operationName === 'UpdateChannel') {
        saved = { id: 'channel-preview', createdAt: '2026-10-17T00:00:00Z', updatedAt: '2026-10-17T00:00:00Z', status: 'enabled', ...body.variables.input };
        await route.fulfill({ json: { data: { updateChannel: saved } } });
      } else {
        await route.fulfill({ json: { data: { allChannelTags: [], channelDisabledAPIKeys: [], proxyPresets: [] } } });
      }
    });
    await page.goto('/tests/interactions/codex-channel.html?realtime');
    await page.getByTestId('zh-locale').evaluate((element) => (element as HTMLButtonElement).click());
    const timeout = page.getByTestId('channel-realtime-timeout');
    const rate = page.getByTestId('channel-realtime-client-events-per-second');
    await timeout.scrollIntoViewIfNeeded();
    await expect(timeout.getByRole('spinbutton')).toHaveValue('12000');
    await expect(rate.getByRole('spinbutton')).toHaveValue('512');
    await timeout.locator('label').hover();
    await page.waitForTimeout(800);
    await expect(page.getByRole('tooltip')).toHaveCount(0);
    await timeout.getByRole('button').hover();
    await expect(page.getByRole('tooltip')).toBeVisible();
    await page.mouse.move(0, 0);
    for (const field of [timeout, rate]) {
      expect(await field.evaluate((element) => element.scrollWidth <= element.clientWidth + 1)).toBe(true);
    }
    await page.screenshot({ animations: 'disabled', path: testInfo.outputPath(`codex-realtime-${width}-light.png`) });
    await page.getByTestId('dark-theme').evaluate((element) => (element as HTMLButtonElement).click());
    await page.screenshot({ animations: 'disabled', path: testInfo.outputPath(`codex-realtime-${width}-dark.png`) });
    await timeout.getByRole('spinbutton').fill('15000');
    await rate.getByRole('spinbutton').fill('1024');
    await page.getByTestId('channel-submit-button').click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await page.getByTestId('reopen-codex').click();
    await timeout.scrollIntoViewIfNeeded();
    await expect(timeout.getByRole('spinbutton')).toHaveValue('15000');
    await expect(rate.getByRole('spinbutton')).toHaveValue('1024');
  });
}
