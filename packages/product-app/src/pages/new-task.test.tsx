import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { expect, test, vi } from 'vitest';

import { NewTaskPage } from './NewTaskPage';

test('allows only one task submission while creation is pending', async () => {
  let finish!: () => void;
  const pending = new Promise<void>((resolve) => { finish = resolve; });
  const onCreate = vi.fn(() => pending);
  const user = userEvent.setup();
  render(<NewTaskPage runtimes={[
    { id: 'demo', mode: 'demo', label: 'Demo', capabilities: ['deterministic'], available: true },
  ]} onCreate={onCreate} />);
  await user.type(screen.getByLabelText('任务目标'), 'Inspect juice-shop.lab');

  const submit = screen.getByRole('button', { name: '创建任务' });
  await user.click(submit);
  await user.click(submit);

  expect(onCreate).toHaveBeenCalledOnce();
  expect(submit).toBeDisabled();
  finish();
});
