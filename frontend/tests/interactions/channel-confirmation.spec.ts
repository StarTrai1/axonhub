import { test, expect, type Page } from '@playwright/test';

async function setPreference(page: Page, enabled: boolean) {
  await page.getByTestId('open-settings').click();
  await page.getByTestId('skip-channel-status-confirmation').setChecked(enabled);
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel', exact: true }).click();
}

test('confirmation preference persists, applies to enabling and disabling, and can be turned off', async ({ page }) => {
  const mutations: unknown[] = [];
  await page.route('**/admin/graphql', async (route) => {
    const body = route.request().postDataJSON();
    if (body.operationName === 'UpdateChannelStatus') mutations.push(body.variables);
    await route.fulfill({ json: { data: { queryModels: [], updateChannelStatus: true } } });
  });
  await page.goto('/tests/interactions/channel-confirmation.html');
  const switches = page.getByTestId('channel-status-switch');
  await switches.nth(0).click();
  await expect(page.getByRole('alertdialog')).toBeVisible();
  expect(mutations).toHaveLength(0);
  await page.getByRole('alertdialog').getByRole('button', { name: 'Cancel', exact: true }).click();
  await setPreference(page, true);
  await page.reload();
  await switches.nth(0).click();
  await expect.poll(() => mutations).toEqual([{ id: 'channel-0', status: 'enabled' }]);
  await expect(page.getByRole('alertdialog')).toHaveCount(0);
  await expect(switches.nth(2)).toBeDisabled();
  await switches.nth(1).click();
  await expect.poll(() => mutations).toEqual([
    { id: 'channel-0', status: 'enabled' }, { id: 'channel-1', status: 'disabled' },
  ]);
  await expect(page.getByRole('alertdialog')).toHaveCount(0);
  await setPreference(page, false);
  await switches.nth(0).click();
  await expect(page.getByRole('alertdialog')).toBeVisible();
  await page.getByRole('alertdialog').getByRole('button', { name: 'Enable', exact: true }).click();
  await expect.poll(() => mutations.length).toBe(3);
  await switches.nth(1).click();
  await expect(page.getByRole('alertdialog')).toBeVisible();
  expect(mutations).toHaveLength(3);
});

for (const action of ['enable', 'disable'] as const) {
  test(`bulk ${action} keeps selection on failure and clears it after success without confirmation`, async ({ page }) => {
    const mutations: unknown[] = [];
    let fail = true;
    await page.route('**/admin/graphql', async (route) => {
      const body = route.request().postDataJSON();
      if (body.operationName === (action === 'enable' ? 'BulkEnableChannels' : 'BulkDisableChannels')) {
        mutations.push(body.variables);
        await route.fulfill({ json: fail ? { errors: [{ message: 'Offline enable failure' }] } : { data: { [action === 'enable' ? 'bulkEnableChannels' : 'bulkDisableChannels']: true } } });
        return;
      }
      await route.fulfill({ json: { data: { queryModels: [] } } });
    });
    await page.goto('/tests/interactions/channel-confirmation.html');
    await page.getByRole('checkbox').nth(1).check();
    await page.getByTestId(`channels-bulk-${action}`).click();
    await expect(page.getByRole('alertdialog')).toBeVisible();
    expect(mutations).toHaveLength(0);
    await page.getByRole('alertdialog').getByRole('button', { name: 'Cancel', exact: true }).click();
    await setPreference(page, true);
    await page.getByTestId(`channels-bulk-${action}`).click();
    await expect.poll(() => mutations.length).toBe(1);
    await expect(page.getByText('Offline enable failure', { exact: true })).toBeVisible();
    await expect(page.getByRole('checkbox').nth(1)).toBeChecked();
    await expect(page.getByRole('alertdialog')).toHaveCount(0);
    fail = false;
    await page.getByTestId(`channels-bulk-${action}`).click();
    await expect.poll(() => mutations.length).toBe(2);
    expect(mutations).toEqual([{ ids: ['channel-0'] }, { ids: ['channel-0'] }]);
    await expect(page.getByRole('checkbox').nth(1)).not.toBeChecked();
  });

}

for (const scenario of [{ row: 0, action: 'enable' }, { row: 1, action: 'disable' }]) {
  test(`direct single ${scenario.action} is disabled while pending and recovers after failure`, async ({ page }) => {
    let finish: (() => Promise<void>) | undefined;
    let attempts = 0;
    await page.route('**/admin/graphql', async (route) => {
      if (route.request().postDataJSON().operationName === 'UpdateChannelStatus') {
        attempts++;
        finish = () => route.fulfill({ json: { errors: [{ message: 'Offline enable failure' }] } });
        return;
      }
      await route.fulfill({ json: { data: { queryModels: [] } } });
    });
    await page.goto('/tests/interactions/channel-confirmation.html');
    await setPreference(page, true);
    const toggle = page.getByTestId('channel-status-switch').nth(scenario.row);
    await toggle.click();
    await expect(toggle).toBeDisabled();
    await expect.poll(() => attempts).toBe(1);
    await finish!();
    await expect(toggle).toBeEnabled();
    await expect(toggle).toBeChecked({ checked: scenario.row === 1 });
    await expect(page.getByText('Offline enable failure', { exact: true })).toBeVisible();
    await toggle.click();
    await expect.poll(() => attempts).toBe(2);
    await finish!();
  });

}

test('the previous enable preference also skips disable confirmation after upgrading', async ({ page }) => {
  const mutations: unknown[] = [];
  await page.addInitScript(() => localStorage.setItem('channels-skip-enable-confirmation', 'true'));
  await page.route('**/admin/graphql', async (route) => {
    const body = route.request().postDataJSON();
    if (body.operationName === 'UpdateChannelStatus') mutations.push(body.variables);
    await route.fulfill({ json: { data: { queryModels: [], updateChannelStatus: true } } });
  });
  await page.goto('/tests/interactions/channel-confirmation.html');
  await page.getByTestId('channel-status-switch').nth(1).click();
  await expect.poll(() => mutations).toEqual([{ id: 'channel-1', status: 'disabled' }]);
  await expect(page.getByRole('alertdialog')).toHaveCount(0);
});
