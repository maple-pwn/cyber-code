import { render, screen } from '@testing-library/react';
import { expect, test } from 'vitest';

import { ProductApp } from '@cyber/product-app';

import { createDesktopBootstrap } from './bootstrap';

test('desktop bootstrap defaults to Demo and capability-gates Local', async () => {
  const { store, runtimes } = createDesktopBootstrap({ speedMs: 0 });
  await store.connect();

  render(<ProductApp store={store} runtimes={runtimes} />);

  expect(screen.getByRole('radio', { name: 'Demo' })).toBeChecked();
  expect(screen.getByText('DEMO · deterministic · demo-only')).toBeInTheDocument();
  expect(screen.getByRole('radio', { name: 'Local' })).toBeDisabled();
  expect(screen.getAllByText(/real runtime capability/i)).toHaveLength(2);
  store.destroy();
});

test('desktop bootstrap restores only allowlisted routes and persists later navigation', () => {
  window.localStorage.setItem('cyber.desktop.route.v1', 'findings');
  const restored = createDesktopBootstrap({ speedMs: 0 });
  expect(restored.store.getSnapshot().route).toBe('findings');

  restored.store.navigate('reports');
  expect(window.localStorage.getItem('cyber.desktop.route.v1')).toBe('reports');
  restored.store.destroy();

  window.localStorage.setItem('cyber.desktop.route.v1', 'javascript:alert(1)');
  const rejected = createDesktopBootstrap({ speedMs: 0 });
  expect(rejected.store.getSnapshot().route).toBe('new-task');
  rejected.store.destroy();
  window.localStorage.clear();
});

test('desktop bootstrap does not restore the transient editor route without an active draft', () => {
  window.localStorage.setItem('cyber.desktop.route.v1', 'editor');

  const restored = createDesktopBootstrap({ speedMs: 0 });

  expect(restored.store.getSnapshot().route).toBe('findings');
  restored.store.destroy();
  window.localStorage.clear();
});
