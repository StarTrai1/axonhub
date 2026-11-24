import { expect, test } from '@playwright/test';

for (const width of [390, 1280]) {
  test(`Codex key catalog shows picker models and speed tiers at ${width}px`, async ({ page }, testInfo) => {
    let requests = 0;
    await page.route('**/codex/models', async (route) => {
      expect(route.request().headers().authorization).toBe('Bearer synthetic-key');
      requests++;
      if (requests === 1) {
        await route.fulfill({ json: { models: [
          { slug: 'gpt-6.2-sol', visibility: 'list', service_tiers: [{ id: 'priority' }, { id: 'ultrafast' }] },
          { slug: 'hidden-fixture', visibility: 'hide' },
        ] } });
      } else {
        await route.fulfill({ status: 401, json: { error: { message: 'synthetic internal error' } } });
      }
    });
    await page.setViewportSize({ width, height: 800 });
    await page.goto('/tests/interactions/peer-regressions.html?case=codex-key-catalog&lang=zh-CN&theme=dark');
    const check = page.getByRole('button', { name: '验证模型访问' });
    await expect(check).toBeVisible();
    expect(requests).toBe(0);
    await check.click();
    await page.getByText('目录返回 2 个模型，1 个可在 Codex 中选择').click();
    await expect(page.getByText('gpt-6.2-sol')).toBeVisible();
    await expect(page.getByText('Fast · Ultrafast')).toBeVisible();
    await expect(page.getByText('hidden-fixture')).toHaveCount(0);
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    await page.getByRole('main').screenshot({ path: testInfo.outputPath(`codex-key-access-${width}.png`) });
    await check.click();
    await expect(page.getByText('网关拒绝了当前密钥。')).toBeVisible();
    await expect(page.getByText('gpt-6.2-sol')).toHaveCount(0);
    expect(requests).toBe(2);
  });
}
