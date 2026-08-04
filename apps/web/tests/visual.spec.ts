import { expect, test, type Page, type TestInfo } from '@playwright/test';

const boxesOverlap = (a: { x: number; y: number; width: number; height: number }, b: { x: number; y: number; width: number; height: number }) =>
  a.x < b.x + b.width && a.x + a.width > b.x && a.y < b.y + b.height && a.y + a.height > b.y;

const expectInside = async (page: Page, childSelector: string, containerSelector: string) => {
  const result = await page.evaluate(({ childSelector, containerSelector }) => {
    const child = document.querySelector(childSelector)?.getBoundingClientRect();
    const container = document.querySelector(containerSelector)?.getBoundingClientRect();
    if (!child || !container) return null;
    return {
      child: { top: child.top, bottom: child.bottom },
      container: { top: container.top, bottom: container.bottom },
    };
  }, { childSelector, containerSelector });
  expect(result).not.toBeNull();
  expect(result!.child.top).toBeGreaterThanOrEqual(result!.container.top);
  expect(result!.child.bottom).toBeLessThanOrEqual(result!.container.bottom);
};

const capture = async (page: Page, testInfo: TestInfo, name: string) => {
  await page.mouse.move(0, 0);
  await expect(page).toHaveScreenshot(name, {
    animations: 'disabled',
    maxDiffPixelRatio: 0.01,
  });
  const main = page.locator('#main-content');
  const pixels = await main.screenshot({ animations: 'disabled' });
  expect(pixels.byteLength).toBeGreaterThan(8_000);
  const surface = page.locator('.task-creation-surface, .cyber-scope-review, .mission-command-deck, .page-findings, .page-reports').first();
  const box = await surface.boundingBox();
  const viewport = page.viewportSize();
  expect(box, `${testInfo.project.name}:${name} primary surface`).not.toBeNull();
  if (box && viewport) {
    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.y).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width).toBeLessThanOrEqual(viewport.width + 1);
  }
};

const useEnglish = async (page: Page) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'English' }).click();
};

test('Liquid Command golden path has stable desktop and phone visuals', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name === 'laptop');
  await useEnglish(page);
  await capture(page, testInfo, 'new-task.png');

  await page.getByLabel('Objective').fill('Assess juice-shop.lab');
  await page.getByRole('button', { name: 'Create task' }).click();
  await expect(page.getByRole('heading', { name: 'Scope Review' })).toBeVisible();
  await capture(page, testInfo, 'scope-review.png');

  if (testInfo.project.name === 'phone') {
    const scopeActions = page.locator('.cyber-scope-review .cyber-actions');
    await scopeActions.scrollIntoViewIfNeeded();
    const actionBox = await scopeActions.boundingBox();
    const navBox = await page.getByRole('navigation', { name: 'Primary' }).boundingBox();
    expect(actionBox && navBox && boxesOverlap(actionBox, navBox), JSON.stringify({ actionBox, navBox })).toBe(false);
  }

  await page.getByRole('button', { name: 'Confirm scope' }).click();
  await expect(page.getByRole('button', { name: 'Review parameters' })).toBeVisible();
  await expectInside(page, '.cyber-approval .cyber-actions', '.mission-stream-surface');
  await capture(page, testInfo, 'mission-waiting.png');
  await page.getByRole('button', { name: 'Review parameters' }).click();
  await expectInside(page, '.cyber-approval-header', '.mission-stream-surface');
  await capture(page, testInfo, 'mission-reviewing.png');
  await page.getByRole('button', { name: 'Confirm allow once' }).click();

  await page.getByRole('button', { name: 'Findings' }).click();
  await expect(page.getByRole('heading', { name: 'Findings' })).toBeVisible();
  await capture(page, testInfo, 'findings.png');
  await page.getByRole('button', { name: 'Reports' }).click();
  await expect(page.getByRole('heading', { name: 'Reports' })).toBeVisible();
  await capture(page, testInfo, 'reports.png');

  await page.getByRole('button', { name: 'Mission Control' }).click();
  await page.getByRole('button', { name: 'Disconnect' }).click();
  await expect(page.getByText('Offline, read-only').first()).toBeVisible();
  await capture(page, testInfo, 'mission-offline.png');
});

test('forced backdrop fallback stays opaque and readable', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium');
  await useEnglish(page);
  await page.evaluate(() => document.documentElement.classList.add('no-backdrop-filter'));
  const surface = page.getByTestId('new-task-surface');
  const computed = await surface.evaluate((element) => {
    const style = getComputedStyle(element);
    return { background: style.backgroundColor, backdrop: style.backdropFilter };
  });
  const alpha = Number(computed.background.match(/[\d.]+(?=\))/)?.[0] ?? 1);
  expect(alpha).toBeGreaterThanOrEqual(0.9);
  expect(computed.backdrop).toBe('none');
  await expect(page.getByRole('heading', { name: 'New authorized task' })).toBeVisible();
});
