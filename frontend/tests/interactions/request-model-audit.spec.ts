import { test, expect } from '@playwright/test';

for (const width of [1280, 768, 390]) {
  for (const theme of ['light', 'dark']) {
    test(`local quota failure without an execution remains readable at ${width}px ${theme}`, async ({ page }, testInfo) => {
      await page.setViewportSize({ width, height: 1000 });
      const errors: string[] = [];
      page.on('pageerror', (error) => errors.push(error.message));
      const empty = { edges: [], totalCount: 0, pageInfo: { hasNextPage: false, hasPreviousPage: false } };
      await page.route('**/admin/graphql', (route) => route.fulfill({ json: { data: {
        node: {
          id: '80853', modelID: 'gpt-6.1-terra', status: 'failed', source: 'api', stream: true,
          createdAt: '2026-11-07T18:03:37Z', updatedAt: '2026-11-07T18:03:39Z', format: 'openai/responses-ws',
          requestBody: { model: 'gpt-6.1-terra', input: [] },
          responseBody: { error: { type: 'quota_routing_error', code: 'quota_reserve_unavailable', message: 'current provider usage could not be verified for configured quota reserve on model gpt-6.1-terra' }, status_code: 503 },
        },
        requestExecutions: empty, usageLogs: empty,
      } } }));
      await page.goto(`/tests/interactions/request-model-audit.html?quota=1&theme=${theme}`);
      await page.getByRole('tab', { name: '响应', exact: true }).click();
      const alert = page.getByTestId('request-quota-error');
      await expect(alert).toContainText('错误信息');
      await expect(alert).toContainText('quota_reserve_unavailable');
      await expect(alert).toContainText('gpt-6.1-terra');
      await alert.scrollIntoViewIfNeeded();
      expect(await alert.evaluate((element) => element.scrollWidth <= element.clientWidth + 1)).toBe(true);
      await alert.screenshot({ path: testInfo.outputPath(`codex-quota-error-${theme}.png`) });
      expect(errors).toEqual([]);
    });
  }
}

test('request model cells render every audit and lifecycle state without crashing', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.route('**/admin/graphql', (route) => route.abort());
  await page.goto('/tests/interactions/request-model-audit.html');

  for (const [scenario, icon] of Object.entries({
    pending: 'question-mark', processing: 'question-mark', failed: 'alert-triangle',
    canceled: 'alert-triangle', unknown: 'question-mark', matched: 'check', mismatched: 'alert-triangle',
  })) {
    const row = page.getByTestId(`request-${scenario}`);
    await expect(row).toContainText('gpt-6-sol');
    const audit = row.getByTestId('upstream-model-audit');
    await expect(audit).toHaveAttribute('aria-label', /\S/);
    await expect(audit.locator(`svg.tabler-icon-${icon}`)).toBeVisible();
    await audit.hover();
    await expect(page.getByRole('tooltip')).toBeVisible();
    await page.keyboard.press('Escape');
    await page.mouse.move(1000, 700);
    await expect(page.getByRole('tooltip')).toBeHidden();
  }
  expect(errors).toEqual([]);
});
