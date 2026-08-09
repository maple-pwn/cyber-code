import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, test } from 'vitest';

import { RuntimeClient } from '@cyber/runtime-client';
import { ScenarioPlayer } from '@cyber/scenario-player';

import { ProductApp } from '../ProductApp';
import { createAppStore } from '../app-store';

describe('authorized scope review flow', () => {
  test('renders runtime authority fields and widens only through a new proposal', async () => {
    const source = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 0 });
    const client = new RuntimeClient(source);
    const store = createAppStore(client, 'new-task');
    await store.connect();
    const user = userEvent.setup();
    render(<ProductApp store={store} />);

    expect(screen.getByTestId('new-task-surface')).toHaveClass('cyber-glass');
    expect(screen.getByLabelText('本地授权实验室')).toBeChecked();
    expect(screen.getByLabelText('远程授权实验室')).not.toBeChecked();
    expect(screen.getByText(/isolated/)).toBeInTheDocument();
    await user.type(screen.getByLabelText('任务目标'), '评估 juice-shop.lab');
    await user.click(screen.getByRole('button', { name: '创建任务' }));

    expect(await screen.findByTestId('scope-review-surface')).toHaveClass('cyber-glass');
    expect(await screen.findByText('authorized-operator')).toBeInTheDocument();
    expect(screen.getByText('scope-1')).toHaveClass('cyber-mono');
    expect(screen.getAllByText('juice-shop.lab')).toHaveLength(2);
    expect(screen.getByText('解析得出的目标')).toBeInTheDocument();
    expect(screen.getByText('/labs/juice-shop')).toBeInTheDocument();
    expect(screen.getByText(/passive-recon/)).toBeInTheDocument();
    expect(screen.getByText(/destructive, persistence/)).toBeInTheDocument();
    expect(screen.getByText('medium')).toBeInTheDocument();
    expect(screen.getByText('single-task')).toBeInTheDocument();
    expect(screen.getByText('破坏性操作已禁用')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: '修改范围' }));
    expect(await screen.findByText('scope-2')).toBeInTheDocument();
    expect(source.events().filter((event) => event.type === 'scope.proposed')).toHaveLength(2);

    await user.click(screen.getByRole('button', { name: '确认范围' }));
    expect(await screen.findByRole('heading', { name: 'Mission Control' })).toBeInTheDocument();
    expect(source.events().filter((event) => event.type === 'scope.confirmed').at(-1)?.payload)
      .toMatchObject({ scope: { id: 'scope-2' } });
  });
});
