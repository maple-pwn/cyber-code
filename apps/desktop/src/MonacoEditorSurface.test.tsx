import { render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';

const { configureMonaco, editorApi } = vi.hoisted(() => ({ configureMonaco: vi.fn(), editorApi: { create: vi.fn() } }));

vi.mock('@monaco-editor/react', () => ({
  default: ({ value }: { value: string }) => <div data-testid="monaco-editor">{value}</div>,
  loader: { config: configureMonaco },
}));
vi.mock('monaco-editor/esm/vs/editor/edcore.main.js', () => ({ editor: editorApi }));
vi.mock('monaco-editor/esm/vs/basic-languages/go/go.contribution.js', () => ({}));
vi.mock('monaco-editor/esm/vs/basic-languages/markdown/markdown.contribution.js', () => ({}));
vi.mock('monaco-editor/esm/vs/basic-languages/python/python.contribution.js', () => ({}));
vi.mock('monaco-editor/esm/vs/basic-languages/rust/rust.contribution.js', () => ({}));
vi.mock('monaco-editor/esm/vs/basic-languages/typescript/typescript.contribution.js', () => ({}));
vi.mock('monaco-editor/esm/vs/basic-languages/yaml/yaml.contribution.js', () => ({}));
vi.mock('monaco-editor/esm/vs/language/json/monaco.contribution.js', () => ({}));

import { MonacoEditorSurface } from './MonacoEditorSurface';

test('uses the bundled Monaco runtime instead of the network loader', () => {
  render(<MonacoEditorSurface path="/workspace/main.go" value="package main" readOnly={false} onChange={vi.fn()} />);

  expect(screen.getByRole('region', { name: '/workspace/main.go' })).toBeInTheDocument();
  expect(screen.getByTestId('monaco-editor')).toHaveTextContent('package main');
  expect(configureMonaco).toHaveBeenCalledWith({ monaco: expect.objectContaining({ editor: editorApi }) });
});
