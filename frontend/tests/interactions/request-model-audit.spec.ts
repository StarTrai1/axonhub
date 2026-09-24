import { test, expect } from '@playwright/test';

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
    const audit = row.getByRole('img').last();
    await expect(audit).toHaveAttribute('aria-label', /\S/);
    await expect(audit.locator(`svg.tabler-icon-${icon}`)).toBeVisible();
    await audit.hover();
    await expect(page.getByRole('tooltip')).toBeVisible();
    await page.mouse.move(0, 0);
  }
  expect(errors).toEqual([]);
});
