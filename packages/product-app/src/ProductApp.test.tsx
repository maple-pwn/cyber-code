import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, test, vi } from 'vitest';

import { RuntimeClient } from '@cyber/runtime-client';
import { ScenarioPlayer } from '@cyber/scenario-player';

import { ProductApp } from './ProductApp';
import { createAppStore } from './app-store';

const renderApp = async () => {
  const player = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 0 });
  const client = new RuntimeClient(player);
  const store = createAppStore(client, 'new-task');
  await store.connect();
  return { ...render(<ProductApp store={store} />), store };
};

describe('App shell', () => {
  test('runs empty task through scope review into Mission Control', async () => {
    const user = userEvent.setup();
    await renderApp();

    await user.type(screen.getByLabelText('任务目标'), '评估 juice-shop.lab');
    await user.click(screen.getByLabelText('本地授权实验室'));
    await user.keyboard('{Control>}{Enter}{/Control}');

    expect(await screen.findByRole('heading', { name: '范围审查' })).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: '确认范围' }));
    expect(await screen.findByRole('heading', { name: 'Mission Control' })).toBeInTheDocument();
  });

  test('disables unavailable sources and explains how to enable them', async () => {
    const player = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 0 });
    const store = createAppStore(new RuntimeClient(player), 'new-task');
    await store.connect();
    render(<ProductApp store={store} runtimes={[
      { id: 'demo', mode: 'demo', label: 'Demo', capabilities: ['deterministic'], available: true },
      {
        id: 'local', mode: 'local', label: 'Local', capabilities: ['real-runtime'], available: false,
        setupStatus: 'Install cyber-code and configure the desktop runtime path.',
      },
    ]} />);

    expect(screen.getByRole('radio', { name: 'Local' })).toBeDisabled();
    expect(screen.getByText('Install cyber-code and configure the desktop runtime path.')).toBeInTheDocument();
    expect(screen.getByText('Demo')).toBeInTheDocument();
    store.destroy();
  });

  test('supports command palette and timeout-safe g navigation chords', async () => {
    const user = userEvent.setup();
    await renderApp();

    await user.keyboard('{Control>}k{/Control}');
    const dialog = screen.getByRole('dialog', { name: '打开命令面板' });
    expect(dialog).toBeInTheDocument();
    await user.type(screen.getByLabelText('搜索页面或操作'), '报告');
    await user.click(within(dialog).getByRole('button', { name: '报告' }));
    expect(screen.getByRole('heading', { name: '报告' })).toBeInTheDocument();

    await user.keyboard('gn');
    expect(screen.getByRole('heading', { name: '新建授权任务' })).toBeInTheDocument();
  });

  test('does not write console errors during shell interactions', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined);
    const user = userEvent.setup();
    await renderApp();
    await user.keyboard('{Control>}k{/Control}{Escape}');
    expect(error).not.toHaveBeenCalled();
    error.mockRestore();
  });

  test('renders a compact icon rail with accessible route names', async () => {
    await renderApp();
    const navigation = screen.getByRole('navigation', { name: 'Primary' });
    expect(within(navigation).getByRole('button', { name: '新建任务' })).toHaveAttribute('aria-current', 'page');
    expect(within(navigation).getAllByTestId('nav-icon')).toHaveLength(5);
    expect(screen.getByTestId('app-canvas')).toHaveClass('app-canvas');
  });

  test('restores Inspector focus after closing the command palette', async () => {
    const user = userEvent.setup();
    await renderApp();
    await user.click(screen.getByRole('button', { name: '任务控制' }));

    const trigger = screen.getByRole('button', { name: '打开任务检查器' });
    await user.click(trigger);
    const inspectorTab = screen.getByRole('tab', { name: 'Agent 0' });
    expect(inspectorTab).toHaveFocus();

    await user.keyboard('{Control>}k{/Control}');
    expect(screen.getByRole('dialog', { name: '打开命令面板' })).toBeInTheDocument();
    expect(screen.getByLabelText('搜索页面或操作')).toHaveFocus();
    await user.keyboard('{Escape}');

    expect(screen.queryByRole('dialog', { name: '打开命令面板' })).not.toBeInTheDocument();
    await waitFor(() => expect(inspectorTab).toHaveFocus());
  });
});
