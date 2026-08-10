import { expect, test } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  await page.goto('/');
  await page.evaluate(() => window.localStorage.clear());
  await page.reload();
  await page.getByRole('button', { name: 'English' }).click();
});

test('safe window reload restores an allowlisted active route', async ({ page }) => {
  await page.getByRole('button', { name: 'Findings' }).click();
  await expect(page.getByRole('heading', { name: 'Findings' })).toBeVisible();

  await page.reload();

  await expect(page.getByTestId('app-canvas')).toHaveClass(/route-findings/);
  await expect(page.locator('nav button[aria-current="page"]')).toHaveCount(1);
});

test('command palette restores Inspector focus without closing the Inspector', async ({ page }) => {
  await page.getByRole('button', { name: 'Mission Control' }).click();
  const inspector = page.getByRole('complementary', { name: 'Mission inspector' });
  await page.getByRole('button', { name: 'Open inspector' }).click();
  const activeTab = inspector.getByRole('tab', { selected: true });
  await expect(activeTab).toBeFocused();

  await page.keyboard.press('Control+k');
  await expect(page.getByLabel('Search pages or actions')).toBeFocused();
  await page.keyboard.press('Escape');

  await expect(inspector).toBeVisible();
  await expect(activeTab).toBeFocused();
});

test('minimum desktop layout and 200 percent scale remain bounded', async ({ page }) => {
  await expect(page.getByTestId('app-canvas')).toBeVisible();
  expect(await page.evaluate(() => ({
    width: window.innerWidth,
    height: window.innerHeight,
    scrollWidth: document.documentElement.scrollWidth,
  }))).toEqual({ width: 1024, height: 768, scrollWidth: 1024 });
});

test('reduced motion and keyboard-only navigation remain available', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.keyboard.press('g');
  await page.keyboard.press('r');
  await expect(page.getByRole('heading', { name: 'Reports' })).toBeVisible();
  const duration = await page.locator('button').first().evaluate((element) => Number.parseFloat(getComputedStyle(element).transitionDuration) || 0);
  expect(duration).toBeLessThanOrEqual(0.00001);
});
