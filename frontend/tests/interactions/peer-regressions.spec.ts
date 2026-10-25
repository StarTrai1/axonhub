import { test, expect } from '@playwright/test';

type DeferredFiles = {
  finishRead: (name: string, value: string) => void;
  finishImage: (source: string) => void;
  hasImage: (source: string) => boolean;
  wasAborted: (name: string) => boolean;
};

declare global {
  interface Window { deferredFiles: DeferredFiles }
}

const pixel = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aX1cAAAAASUVORK5CYII=';

test.beforeEach(async ({ page }) => {
  // These fixtures are fully local; no management mutation can escape to a backend.
  await page.route('**/admin/graphql', (route) => route.abort());
});

for (const kind of ['free', 'strict']) {
  test(`autocomplete ${kind} leaves IME keys to composition`, async ({ page }) => {
    await page.goto('/tests/interactions/peer-regressions.html?case=composition');
    const input = page.getByTestId(`composition-${kind}`).getByRole('combobox');
    await input.click();
    const selected = page.locator('[cmdk-item][aria-selected="true"]');
    await expect(selected).toHaveText('First');
    for (const key of ['ArrowDown', 'ArrowUp', 'Enter', 'Escape']) {
      for (const legacy of [false, true]) {
        const allowed = await input.evaluate((element, { key, legacy }) => element.dispatchEvent(new KeyboardEvent('keydown', {
          key, bubbles: true, cancelable: true, isComposing: !legacy, keyCode: legacy ? 229 : 0,
        })), { key, legacy });
        // Radix captures Escape at the document before the input handler;
        // preventing its dismissal is required to keep the IME session open.
        expect(allowed, `${key}, legacy=${legacy}`).toBe(key !== 'Escape');
        await expect(selected).toHaveText('First');
        await expect(page.getByTestId(`composition-${kind}-value`)).toHaveText('');
      }
    }
    await expect(page.getByTestId('composition-key-count')).toHaveText('0');
    await input.press('ArrowDown');
    await expect(selected).toHaveText('Second');
    await input.press('Enter');
    await expect(page.getByTestId(`composition-${kind}-value`)).toHaveText('second');
  });
}

test('server-searched selection survives empty pages and restores its label on dismissal', async ({ page }) => {
  await page.goto('/tests/interactions/peer-regressions.html?case=server-select');
  const dialog = page.getByTestId('server-select-dialog');
  const input = page.getByRole('combobox', { name: 'User' });
  await expect(input).toHaveAccessibleDescription('Choose an existing account');
  await expect(input).toHaveAttribute('aria-expanded', 'false');
  await input.click();
  await expect(input).toHaveAttribute('aria-expanded', 'true');
  // The popover must remain in the dialog's focus and scroll scope.
  await dialog.getByRole('option', { name: 'Fixture User (my@example.com)' }).click();
  await expect(page.getByTestId('server-selection')).toHaveText('user-1');
  await expect(input).toHaveValue('Fixture User (my@example.com)');
  await expect(input).toHaveAttribute('aria-expanded', 'false');
  for (const dismiss of ['escape', 'blur']) {
    await input.click();
    await input.fill('no-such-user');
    await expect(dialog.getByRole('option')).toHaveCount(0);
    if (dismiss === 'escape') await input.press('Escape');
    else await dialog.getByRole('heading').click();
    await expect(input).toHaveValue('Fixture User (my@example.com)');
    await expect(page.getByTestId('server-selection')).toHaveText('user-1');
  }
  await page.getByTestId('clear-server-selection').click();
  await expect(input).toHaveValue('');
  await expect(page.getByTestId('server-selection')).toHaveText('');
});

test('Markdown renders math and diagrams with the patched dependency graph', async ({ page }) => {
  await page.goto('/tests/interactions/peer-regressions.html?case=rendering');
  const response = page.getByTestId('rendering-response');
  await expect(response.getByRole('heading', { name: 'Formula' })).toBeVisible();
  await expect(response.locator('annotation[encoding="application/x-tex"]').first()).toHaveText('E = mc^2');
  await expect(response.locator('.katex').first()).toBeVisible();
  await expect(response.locator('svg[id^="mermaid"]').first()).toBeVisible();
  await expect(response.locator('script, [onerror]')).toHaveCount(0);
  expect(await page.evaluate(() => Reflect.get(window, 'renderingInjected'))).toBeUndefined();
});

test('Playground resets private drafts and settings when the user or project changes', async ({ page }) => {
  await page.route('**/admin/playground/**', (route) => route.abort());
  await page.goto('/tests/interactions/peer-regressions.html?case=playground');
  const draft = page.getByTestId('playground-draft');
  const system = page.getByTestId('playground-system-prompt');
  await expect(draft).toBeVisible();
  const initialSystem = await system.inputValue();
  for (const target of ['switch-playground-user', 'switch-playground-project']) {
    await draft.fill('Private draft belonging to the previous session');
    await system.fill('Private system instructions');
    await page.getByTestId(target).click();
    await expect(draft).toHaveValue('');
    await expect(system).toHaveValue(initialSystem);
  }
});

test('scientific notation reaches all pricing tier form values without truncating the exponent', async ({ page }) => {
  await page.goto('/tests/interactions/peer-regressions.html');
  const modelBounds = page.getByTestId('model-prices').locator('[data-testid="pricing-tier-upper-bound"]:enabled');
  await expect(modelBounds).toHaveCount(2);
  await modelBounds.nth(0).fill('1e6');
  await modelBounds.nth(1).fill('2.5e5');
  await page.getByTestId('scheduled-prices').locator('[data-testid="pricing-tier-upper-bound"]:enabled').fill('3e6');
  const model = JSON.parse(await page.getByTestId('model-values').innerText());
  const schedule = JSON.parse(await page.getByTestId('schedule-values').innerText());
  expect(model.prices[0].price.items[0].pricing.usageTiered.tiers[0].upTo).toBe(1000000);
  expect(model.prices[0].price.items[1].promptWriteCacheVariants[0].pricing.usageTiered.tiers[0].upTo).toBe(250000);
  expect(schedule.prices[0].price.schedule.overrides[0].items[0].pricing.usageTiered.tiers[0].upTo).toBe(3000000);
  await modelBounds.nth(0).fill('');
  const cleared = JSON.parse(await page.getByTestId('model-values').innerText());
  expect(cleared.prices[0].price.items[0].pricing.usageTiered.tiers.map((tier: { upTo: number | null }) => tier.upTo)).toEqual([null, null]);
});

test.describe('superseded file operations', () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      const reads: Array<{ name: string; aborted: () => boolean; finish: (value: string) => void }> = [];
      const images: DeferredImage[] = [];
      class DeferredReader {
        result: string | null = null;
        onload: ((event: { target: DeferredReader }) => unknown) | null = null;
        aborted = false;
        read(file: File) {
          const deliver = this.onload;
          reads.push({
            name: file.name, aborted: () => this.aborted,
            // Deliver even an already queued callback after cancellation.
            finish: (value) => { this.result = value; deliver?.({ target: this }); },
          });
        }
        readAsDataURL(file: File) { this.read(file); }
        readAsText(file: File) { this.read(file); }
        abort() { this.aborted = true; }
      }
      class DeferredImage extends EventTarget {
        width = 1;
        height = 1;
        naturalWidth = 1;
        complete = false;
        source = '';
        onload: (() => unknown) | null = null;
        set src(value: string) { this.source = value; images.push(this); }
        get src() { return this.source; }
      }
      window.FileReader = DeferredReader as unknown as typeof FileReader;
      window.Image = DeferredImage as unknown as typeof Image;
      window.deferredFiles = {
        finishRead: (name, value) => {
          const read = reads.findLast((item) => item.name === name);
          if (!read) throw new Error('Missing pending read: ' + name);
          read.finish(value);
        },
        finishImage: (source) => {
          for (const image of images.filter((item) => item.source === source && !item.complete)) {
            image.complete = true;
            image.onload?.();
            image.dispatchEvent(new Event('load'));
          }
        },
        hasImage: (source) => images.some((item) => item.source === source),
        wasAborted: (name) => reads.findLast((item) => item.name === name)?.aborted() ?? false,
      };
    });
    await page.goto('/tests/interactions/peer-regressions.html?case=uploads');
    await expect(page.getByTestId('brand-logo-upload')).toBeAttached();
  });

  test('text preview ignores completion from the previous file', async ({ page }) => {
    await page.getByTestId('next-file').click();
    await page.evaluate(() => window.deferredFiles.finishRead('second.txt', 'current file contents'));
    await expect(page.getByTestId('text-preview')).toContainText('current file contents');
    await page.evaluate(() => window.deferredFiles.finishRead('first.txt', 'stale file contents'));
    await expect(page.getByTestId('text-preview')).not.toContainText('stale file contents');
    expect(await page.evaluate(() => window.deferredFiles.wasAborted('first.txt'))).toBe(true);
    await page.getByTestId('unmount-uploads').click();
    expect(await page.evaluate(() => window.deferredFiles.wasAborted('second.txt'))).toBe(true);
  });

  test('logo selection and removal invalidate both file reads and pending image decoding', async ({ page }) => {
    const input = page.getByTestId('brand-logo-upload');
    await input.setInputFiles({ name: 'first.png', mimeType: 'image/png', buffer: Buffer.from('offline') });
    await page.evaluate((value) => window.deferredFiles.finishRead('first.png', value), pixel + '#first');
    await input.setInputFiles({ name: 'second.png', mimeType: 'image/png', buffer: Buffer.from('offline') });
    await page.evaluate((value) => {
      window.deferredFiles.finishRead('second.png', value);
      window.deferredFiles.finishImage(value);
    }, pixel + '#second');
    const preview = page.getByTestId('brand').locator('img');
    await expect(preview).toHaveAttribute('src', pixel + '#second');
    await page.evaluate((value) => window.deferredFiles.finishImage(value), pixel + '#first');
    await expect(preview).toHaveAttribute('src', pixel + '#second');
    await input.setInputFiles({ name: 'third.png', mimeType: 'image/png', buffer: Buffer.from('offline') });
    await page.evaluate((value) => window.deferredFiles.finishRead('third.png', value), pixel + '#third');
    await page.getByTestId('brand-logo-remove').click();
    await page.evaluate((value) => window.deferredFiles.finishImage(value), pixel + '#third');
    await expect(preview).toHaveCount(0);
    await input.setInputFiles({ name: 'pending.png', mimeType: 'image/png', buffer: Buffer.from('offline') });
    await page.getByTestId('unmount-uploads').click();
    expect(await page.evaluate(() => window.deferredFiles.wasAborted('pending.png'))).toBe(true);
  });

  test('avatar keeps the newest selection when an old read finishes late', async ({ page }) => {
    const input = page.getByTestId('avatar-upload');
    await input.setInputFiles({ name: 'old-avatar.png', mimeType: 'image/png', buffer: Buffer.from('offline') });
    await input.setInputFiles({ name: 'new-avatar.png', mimeType: 'image/png', buffer: Buffer.from('offline') });
    await page.evaluate((value) => window.deferredFiles.finishRead('new-avatar.png', value), pixel + '#avatar-new');
    await expect.poll(() => page.evaluate((value) => window.deferredFiles.hasImage(value), pixel + '#avatar-new')).toBe(true);
    await page.evaluate((value) => window.deferredFiles.finishImage(value), pixel + '#avatar-new');
    await page.evaluate((value) => window.deferredFiles.finishRead('old-avatar.png', value), pixel + '#avatar-old');
    await expect(page.getByTestId('profile').locator('img')).toHaveAttribute('src', pixel + '#avatar-new');
    expect(await page.evaluate(() => window.deferredFiles.wasAborted('old-avatar.png'))).toBe(true);
  });
});
