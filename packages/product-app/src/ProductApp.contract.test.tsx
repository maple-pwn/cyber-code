import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { expect, test, vi } from 'vitest';

import { RuntimeClient } from '@cyber/runtime-client';
import { ScenarioPlayer } from '@cyber/scenario-player';

import { ProductApp, createAppStore } from './index';

test('shared product app preserves Web route transitions and runtime commands', async () => {
  const source = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 0 });
  const store = createAppStore(new RuntimeClient(source), 'new-task');
  await store.connect();
  const dispatch = vi.spyOn(store, 'dispatch');
  const user = userEvent.setup();

  render(<ProductApp store={store} />);
  await user.type(screen.getByLabelText('任务目标'), '  评估 juice-shop.lab  ');
  await user.type(screen.getByLabelText('本地工作区'), '  /labs/juice-shop  ');
  await user.click(screen.getByRole('button', { name: '创建任务' }));

  expect(await screen.findByRole('heading', { name: '范围审查' })).toBeInTheDocument();
  expect(screen.getByText('Demo').closest('.scope-runtime-context')).toHaveTextContent('Demo scenario-local');
  expect(dispatch).toHaveBeenNthCalledWith(1, {
    type: 'task.create',
    objective: '评估 juice-shop.lab',
    runtimeId: 'scenario-local',
    workspace: '/labs/juice-shop',
  });

  await user.click(screen.getByRole('button', { name: '确认范围' }));
  expect(await screen.findByRole('heading', { name: 'Mission Control' })).toBeInTheDocument();
  expect(dispatch).toHaveBeenNthCalledWith(2, { type: 'scope.confirm', scopeId: 'scope-1' });
});
