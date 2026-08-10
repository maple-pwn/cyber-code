import { render, screen, waitFor } from '@testing-library/react';
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

test('attaches browser files to a cyber-agent task', async () => {
  const onCreate = vi.fn().mockResolvedValue(undefined);
  const user = userEvent.setup();
  render(<NewTaskPage runtimes={[
    { id: 'cyber-agent-remote', mode: 'remote', label: 'Security Runtime', capabilities: ['security-runtime'], available: true },
  ]} onCreate={onCreate} />);
  await user.type(screen.getByLabelText('任务目标'), 'Assess the attached API');
  await user.upload(screen.getByLabelText('任务输入'), new File(['openapi: 3.0.0'], 'api.yaml', { type: 'application/yaml' }));
  expect(screen.getByText(/api\.yaml/)).toBeInTheDocument();
  await user.click(screen.getByRole('button', { name: '创建任务' }));
  await waitFor(() => expect(onCreate).toHaveBeenCalledWith(expect.objectContaining({
    inputs: [expect.objectContaining({ filename: 'api.yaml', mediaType: 'application/yaml' })],
  })));
});

test('accepts task inputs from a trusted desktop picker', async () => {
  const onCreate = vi.fn().mockResolvedValue(undefined);
  const pickInputs = vi.fn().mockResolvedValue([{ filename: 'logs.zip', mediaType: 'application/zip', bytes: new Uint8Array([1, 2]) }]);
  const user = userEvent.setup();
  render(<NewTaskPage runtimes={[
    { id: 'cyber-agent-local', mode: 'local', label: 'Security Runtime', capabilities: ['security-runtime'], available: true },
  ]} pickInputs={pickInputs} onCreate={onCreate} />);
  await user.click(screen.getByRole('button', { name: '从设备选择' }));
  expect(await screen.findByText(/logs\.zip/)).toBeInTheDocument();
});
