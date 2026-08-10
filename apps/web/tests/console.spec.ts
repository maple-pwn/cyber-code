import { expect, test } from '@playwright/test';

test('primary interaction emits no browser console errors', async ({ page }) => {
  const errors: string[] = [];
  page.on('console', (message) => { if (message.type() === 'error') errors.push(message.text()); });
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto('/');
  await page.getByLabel('任务目标').fill('评估 juice-shop.lab');
  await page.getByRole('button', { name: '创建任务' }).click();
  await page.getByRole('button', { name: '确认范围' }).click();
  await page.getByRole('button', { name: '拒绝' }).click();
  await page.getByRole('button', { name: '报告' }).click();
  expect(errors).toEqual([]);
});
