import { expect, test, type Page } from '@playwright/test';

const boxesOverlap = (a: { x: number; y: number; width: number; height: number }, b: { x: number; y: number; width: number; height: number }) =>
  a.x < b.x + b.width && a.x + a.width > b.x && a.y < b.y + b.height && a.y + a.height > b.y;

const useEnglish = async (page: Page) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'English' }).click();
};

const createAuthorizedTask = async (page: Page) => {
  await page.getByLabel('Objective').fill('Assess juice-shop.lab');
  await page.getByLabel('Local authorized lab').check();
  await page.getByRole('button', { name: 'Create task' }).click();
  await expect(page.getByRole('heading', { name: 'Scope Review' })).toBeVisible();
  await page.getByRole('button', { name: 'Confirm scope' }).click();
  await expect(page.getByRole('heading', { name: 'Mission Control' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Review parameters' })).toBeVisible();
};

test.describe('desktop golden paths', () => {
  test('Allow produces a confirmed High Finding and frozen PDF report', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name === 'phone');
    await useEnglish(page);
    await createAuthorizedTask(page);
    await page.getByRole('button', { name: 'Review parameters' }).click();
    await page.getByRole('button', { name: 'Confirm allow once' }).click();
    await page.getByRole('button', { name: 'Findings' }).click();
    await expect(page.getByText(/^Confirmed ·/)).toBeVisible();
    await expect(page.getByText(/Severity: high/)).toBeVisible();
    await page.getByRole('button', { name: 'Reports' }).click();
    await expect(page.getByText('Verified impact')).toBeVisible();
    await page.getByLabel(/Analysis notes/).fill('Reviewed by operator');
    await page.getByRole('button', { name: 'Freeze report' }).click();
    await expect(page.getByText(/Report version 1/)).toBeVisible();
    await page.getByLabel('Export').selectOption('pdf');
    const download = page.waitForEvent('download');
    await page.getByRole('button', { name: 'Export' }).click();
    expect((await download).suggestedFilename()).toBe('report-1-v1.pdf');
  });

  test('Deny preserves an explicit verification limitation', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name === 'phone');
    await useEnglish(page);
    await createAuthorizedTask(page);
    await page.getByRole('button', { name: 'Deny' }).click();
    await page.getByRole('button', { name: 'Reports' }).click();
    await expect(page.getByText('Verification limitation')).toBeVisible();
    await expect(page.getByText(/Verification denied/)).toBeVisible();
  });

  test('disconnects into cached read-only state, reconnects, and explicitly takes control', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name === 'phone');
    await useEnglish(page);
    await createAuthorizedTask(page);
    const taskTitle = page.getByText('Authorized juice-shop.lab assessment');
    await expect(taskTitle).toBeVisible();
    await page.getByRole('button', { name: 'Disconnect' }).click();
    await expect(page.locator('#main-content').getByRole('status').getByText('Offline, read-only')).toBeVisible();
    await expect(taskTitle).toBeVisible();
    await expect(page.getByRole('button', { name: 'Pause' })).toBeDisabled();
    await page.getByRole('button', { name: 'Reconnect' }).click();
    await expect(page.locator('#main-content').getByRole('status').getByText('Connected')).toBeVisible();
    await page.getByRole('button', { name: 'Take control' }).click();
    await expect(page.getByText(/Lease revision: 1/)).toBeVisible();
  });

  test('supports language switching and a keyboard-only primary flow', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name === 'phone');
    await page.goto('/');
    await expect(page.getByRole('heading', { name: '新建授权任务' })).toBeVisible();
    await page.getByRole('button', { name: 'English' }).click();
    await page.getByLabel('Objective').fill('Assess juice-shop.lab');
    await page.getByLabel('Objective').press('Control+Enter');
    await expect(page.getByRole('heading', { name: 'Scope Review' })).toBeVisible();
    await page.getByRole('button', { name: 'Confirm scope' }).focus();
    await page.keyboard.press('Enter');
    await page.getByRole('button', { name: 'Review parameters' }).focus();
    await page.keyboard.press('Enter');
    await page.getByRole('button', { name: 'Confirm allow once' }).focus();
    await page.keyboard.press('Enter');
    await page.keyboard.press('g'); await page.keyboard.press('f');
    await expect(page.getByRole('heading', { name: 'Findings' })).toBeVisible();
    await page.keyboard.press('g'); await page.keyboard.press('r');
    await expect(page.getByRole('heading', { name: 'Reports' })).toBeVisible();
  });
});

test('phone keeps observation, approval, pause, and cancel while report editing requires desktop', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'phone');
  await useEnglish(page);
  await expect(page.getByLabel('Local workspace')).toBeHidden();
  await createAuthorizedTask(page);
  await expect(page.getByRole('button', { name: 'Pause' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Cancel task' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Review parameters' })).toBeVisible();
  await page.getByRole('button', { name: 'Review parameters' }).click();
  await expect(page.getByRole('button', { name: 'Confirm allow once' })).toBeVisible();
  const ribbonBox = await page.locator('.cyber-active-agent').boundingBox();
  const actionBox = await page.locator('.cyber-approval .cyber-actions').boundingBox();
  const navBox = await page.getByRole('navigation', { name: 'Primary' }).boundingBox();
  expect(ribbonBox && actionBox && boxesOverlap(ribbonBox, actionBox)).toBe(false);
  expect(ribbonBox && navBox && boxesOverlap(ribbonBox, navBox)).toBe(false);
  await page.getByRole('button', { name: 'Reports' }).click();
  await expect(page.getByText('This action requires a desktop viewport')).toBeVisible();
  await expect(page.getByLabel(/Analysis notes/)).toBeHidden();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});

test('laptop presents the Inspector as an overlay', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'laptop');
  await useEnglish(page);
  await createAuthorizedTask(page);
  const inspector = page.getByRole('complementary', { name: 'Mission inspector' });
  const trigger = page.getByRole('button', { name: 'Open inspector' });
  await expect(inspector).toBeHidden();
  await trigger.click();
  await expect(inspector).toBeVisible();
  await expect(inspector).toHaveCSS('position', 'fixed');
  await page.keyboard.press('Escape');
  await expect(inspector).toBeHidden();
  await expect(trigger).toBeFocused();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});
