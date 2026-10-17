import { expect, test } from '@playwright/test';

test('model icons retain the catalog and update after lazy loading', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto('/tests/interactions/model-icons.html');
  for (const name of ['OpenAI', 'Anthropic', 'Qwen', 'AionLabs', 'Dolphin']) {
    const icon = page.getByTestId(name).locator('svg').first();
    await expect(icon).toBeVisible();
    await expect(icon).toHaveCSS('width', '20px');
  }
  await expect(page.getByTestId('missing-icon')).toHaveText('-');
  const original = await page.getByTestId('changing-icon').innerHTML();
  await page.getByRole('button', { name: 'Change' }).click();
  await expect(page.getByTestId('changing-icon').locator('svg').first()).toBeVisible();
  await expect.poll(() => page.getByTestId('changing-icon').innerHTML()).not.toBe(original);
  expect(errors).toEqual([]);
});
