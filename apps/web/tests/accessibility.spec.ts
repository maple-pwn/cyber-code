import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';

const assertA11y = async (page: Page) => {
  const results = await new AxeBuilder({ page }).analyze();
  expect(results.violations).toEqual([]);
};

test('priority workflow pages have no detectable accessibility violations', async ({ page }) => {
  await page.goto('/');
  await assertA11y(page);
  await page.getByLabel('任务目标').fill('评估 juice-shop.lab');
  await page.getByRole('button', { name: '创建任务' }).click();
  await assertA11y(page);
  await page.getByRole('button', { name: '确认范围' }).click();
  await expect(page.getByRole('button', { name: '仅允许一次' })).toBeVisible();
  await assertA11y(page);
  await page.getByRole('button', { name: '仅允许一次' }).click();
  await page.getByRole('button', { name: '发现' }).click();
  await assertA11y(page);
  await page.getByRole('button', { name: '报告' }).click();
  await assertA11y(page);
});

test('focus is visible and reduced motion removes meaningful transitions', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.goto('/');
  await page.keyboard.press('Tab');
  const focused = page.locator(':focus');
  await expect(focused).toBeVisible();
  expect(await focused.evaluate((element) => getComputedStyle(element).outlineStyle)).not.toBe('none');
  const duration = await page.locator('button').first().evaluate((element) => Number.parseFloat(getComputedStyle(element).transitionDuration) || 0);
  expect(duration).toBeLessThanOrEqual(0.00001);
});
