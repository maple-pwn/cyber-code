import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, test, vi } from 'vitest';

import { RuntimeClient } from '@cyber/runtime-client';
import { ScenarioPlayer } from '@cyber/scenario-player';
import { createTranslator } from '@cyber/i18n';
import { initialProductState, project, validateEvent, type ReportState } from '@cyber/protocol';

import { ProductApp } from './ProductApp';
import { createAppStore } from './app-store';
import { ReportsPage } from './pages/ReportsPage';

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

  test('enters Mission Control while scope confirmation is still running', async () => {
    const user = userEvent.setup();
    const { store } = await renderApp();
    await user.type(screen.getByLabelText('任务目标'), '评估 juice-shop.lab');
    await user.click(screen.getByLabelText('本地授权实验室'));
    await user.keyboard('{Control>}{Enter}{/Control}');
    expect(await screen.findByRole('heading', { name: '范围审查' })).toBeInTheDocument();

    const originalDispatch = store.dispatch.bind(store);
    let releaseConfirmation!: () => void;
    vi.spyOn(store, 'dispatch').mockImplementation((command) => command.type === 'scope.confirm'
      ? new Promise<void>((resolve) => { releaseConfirmation = resolve; })
      : originalDispatch(command));

    await user.click(screen.getByRole('button', { name: '确认范围' }));
    expect(await screen.findByRole('heading', { name: 'Mission Control' })).toBeInTheDocument();
    releaseConfirmation();
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

  test('surfaces the cyber-agent Skill lifecycle capability in runtime selection', async () => {
    const player = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 0 });
    const store = createAppStore(new RuntimeClient(player), 'new-task');
    await store.connect();
    render(<ProductApp store={store} runtimes={[
      { id: 'demo', mode: 'demo', label: 'Demo', capabilities: ['deterministic'], available: true },
      { id: 'cyber-agent', mode: 'local', label: 'Security Runtime - cyber-agent', capabilities: ['security-runtime', 'skills.lifecycle.v1'], available: true },
    ]} />);

    expect(screen.getByRole('radio', { name: 'Security Runtime - cyber-agent' })).toBeEnabled();
    expect(screen.getByText('LOCAL · security-runtime · skills.lifecycle.v1')).toBeInTheDocument();
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

  test('uses the matching report draft event time in the report heading', () => {
    const report: ReportState = { id: 'report-timed', taskId: 'task-1', version: 1, status: 'draft', narrative: '# Assessment', recommendations: '', humanNotes: '', findings: [] };
    const draftedAt = '2026-08-10T08:16:04+08:00';
    const result = project(initialProductState(), validateEvent({
      schemaVersion: 1,
      eventId: 'event-report-timed',
      taskId: 'task-1',
      cursor: 1,
      occurredAt: draftedAt,
      type: 'report.drafted',
      source: { runtimeId: 'cyber-agent-local' },
      payload: { report },
    }));
    if (result.kind !== 'applied') throw new Error('expected report event to be applied');

    render(<ReportsPage product={result.state} t={createTranslator('en')} />);

    const expectedTime = new Intl.DateTimeFormat('en', { dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(draftedAt));
    expect(screen.getByRole('heading', { name: `Security assessment report · ${expectedTime}`, level: 2 })).toBeInTheDocument();
  });
});
