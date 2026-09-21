import { test, expect } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  await page.goto('/tests/interactions/fixture.html');
});

test('unmounting a dialog with an open menu releases navigation', async ({ page }) => {
  for (let attempt = 0; attempt < 3; attempt++) {
    await page.getByTestId('open-dialog').click();
    await page.getByTestId('nested-menu').click();
    await page.getByTestId('close-parent').click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect.poll(() => page.evaluate(() => getComputedStyle(document.body).pointerEvents)).not.toBe('none');
  }
  await page.getByTestId('navigation').click();
  await expect(page).toHaveURL(/#project$/);
});

test('menu to dialog handoff retains modality then restores pointer and keyboard navigation', async ({ page }) => {
  for (let attempt = 0; attempt < 3; attempt++) {
    await page.getByTestId('row-menu').click();
    await page.getByTestId('edit').click();
    await expect(page.getByRole('dialog')).toBeVisible();
    await page.getByTestId('close-dialog').click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect.poll(() => page.evaluate(() => getComputedStyle(document.body).pointerEvents)).not.toBe('none');
  }
  await page.getByTestId('row-menu').focus();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('menu')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('menu')).toHaveCount(0);
  await page.getByTestId('navigation').click();
  await expect(page.getByTestId('page')).toHaveText('project');
});

test('closed overlays release interaction even if CSS animations cannot finish', async ({ page }) => {
  await page.getByTestId('open-dialog').click();
  await expect(page.getByRole('dialog')).toBeVisible();
  // Models delayed animation completion on a suspended/background browser tab.
  await page.addStyleTag({ content: '[data-state="closed"] { animation-play-state: paused !important; }' });
  await page.getByTestId('close-dialog').click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect.poll(() => page.evaluate(() => getComputedStyle(document.body).pointerEvents)).not.toBe('none');
  await page.getByTestId('navigation').click();
  await expect(page).toHaveURL(/#project$/);
});
