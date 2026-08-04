import { render, screen } from '@testing-library/react';
import { expect, test } from 'vitest';

import { ProductApp } from '@cyber/product-app';

import { createDesktopBootstrap } from './bootstrap';

test('desktop bootstrap identifies the deterministic source as Demo and never Local', async () => {
  const { store, runtimes } = createDesktopBootstrap({ speedMs: 0 });
  await store.connect();

  render(<ProductApp store={store} runtimes={runtimes} />);

  expect(screen.getByRole('radio', { name: 'Demo' })).toBeChecked();
  expect(screen.getByText('deterministic · demo-only')).toBeInTheDocument();
  expect(screen.queryByText(/本地授权实验室|Local authorized lab/)).not.toBeInTheDocument();
  store.destroy();
});
