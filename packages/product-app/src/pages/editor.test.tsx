import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { axe } from 'vitest-axe';
import { describe, expect, test, vi } from 'vitest';

import { createTranslator } from '@cyber/i18n';
import { initialProductState, type EditorDraftState } from '@cyber/protocol';

import type { AppStore } from '../app-store';
import { EditorPage } from './EditorPage';

const baseSha256 = '2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824';
const draft: EditorDraftState = { id: 'draft-1', path: '/workspace/app.go', scopeId: 'scope-1', ownerClientId: 'client-1', leaseRevision: 1, baseSha256, baseByteLength: 5, encoding: 'utf-8', evidenceReferences: [{ findingId: 'finding-1', evidenceId: 'evidence-1', startLine: 4, endLine: 8 }], status: 'open', nextRevision: 1 };

describe('EditorPage', () => {
  test('waits for the opened draft projection before reading ephemeral content', async () => {
    const readEditorDraft = vi.fn().mockResolvedValue({ draftId: draft.id, revision: 0, baseSha256, data: 'aGVsbG8=', byteLength: 5, encoding: 'utf-8' });
    const baseProduct = { ...initialProductState(), task: { id: 'task-1', title: 'Edit', status: 'running' }, controlLease: { clientId: 'client-1', revision: 1 } };
    const store = { readEditorDraft } as unknown as AppStore;
    const { rerender } = render(<EditorPage product={baseProduct} draftId={draft.id} store={store} t={createTranslator('zh-CN')} onBack={vi.fn()} />);

    expect(screen.getByRole('status')).toHaveTextContent('正在读取文件');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(readEditorDraft).not.toHaveBeenCalled();

    rerender(<EditorPage product={{ ...baseProduct, editorDrafts: { [draft.id]: draft } }} draftId={draft.id} store={store} t={createTranslator('zh-CN')} onBack={vi.fn()} />);
    expect(await screen.findByRole('textbox', { name: draft.path })).toHaveValue('hello');
    expect(readEditorDraft).toHaveBeenCalledTimes(1);
  });

  test('loads ephemeral content and dispatches a bounded save command', async () => {
    const dispatch = vi.fn().mockResolvedValue(undefined);
    const readEditorDraft = vi.fn().mockResolvedValue({ draftId: draft.id, revision: 0, baseSha256, data: 'aGVsbG8=', byteLength: 5, encoding: 'utf-8' });
    const product = { ...initialProductState(), task: { id: 'task-1', title: 'Edit', status: 'running' }, controlLease: { clientId: 'client-1', revision: 1 }, editorDrafts: { [draft.id]: draft } };
    render(<EditorPage product={product} draftId={draft.id} store={{ dispatch, readEditorDraft } as unknown as AppStore} t={createTranslator('zh-CN')} onBack={vi.fn()} />);

    const editor = await screen.findByRole('textbox', { name: draft.path });
    await userEvent.clear(editor);
    await userEvent.type(editor, 'updated');
    await userEvent.click(screen.getByRole('button', { name: '保存草稿' }));

    expect(readEditorDraft).toHaveBeenCalledWith('task-1', draft.id, 1);
    expect(dispatch).toHaveBeenCalledWith(expect.objectContaining({ type: 'editor.save', draftId: draft.id, revision: 1, baseSha256, data: 'dXBkYXRlZA==', byteLength: 7, expectedLeaseRevision: 1 }));
    expect(JSON.stringify(product)).not.toContain('aGVsbG8=');
  });

  test('does not read or edit after the lease owner changes', async () => {
    const readEditorDraft = vi.fn();
    const product = { ...initialProductState(), task: { id: 'task-1', title: 'Edit', status: 'running' }, controlLease: { clientId: 'client-2', revision: 2 }, editorDrafts: { [draft.id]: draft } };
    render(<EditorPage product={product} draftId={draft.id} store={{ readEditorDraft } as unknown as AppStore} t={createTranslator('zh-CN')} onBack={vi.fn()} />);

    expect(await screen.findByRole('alert')).toHaveTextContent('当前运行时不允许编辑');
    expect(readEditorDraft).not.toHaveBeenCalled();
  });

  test('dispatches the saved revision when applying a projected draft', async () => {
    const dispatch = vi.fn().mockResolvedValue(undefined);
    const readEditorDraft = vi.fn().mockResolvedValue({ draftId: draft.id, revision: 1, baseSha256, data: 'aGVsbG8=', byteLength: 5, encoding: 'utf-8' });
    const savedDraft: EditorDraftState = { ...draft, status: 'saved', nextRevision: 2, proposedSha256: '486ea46224d1bb4fb680f34f7c9ad96a8f24ec88be73ea8e5a6c65260e9cb8a7', proposedByteLength: 5 };
    const product = { ...initialProductState(), task: { id: 'task-1', title: 'Edit', status: 'running' }, controlLease: { clientId: 'client-1', revision: 1 }, editorDrafts: { [draft.id]: savedDraft } };
    render(<EditorPage product={product} draftId={draft.id} store={{ dispatch, readEditorDraft } as unknown as AppStore} t={createTranslator('zh-CN')} onBack={vi.fn()} />);

    await screen.findByRole('textbox', { name: draft.path });
    await userEvent.click(screen.getByRole('button', { name: '应用补丁' }));

    expect(dispatch).toHaveBeenCalledWith({ type: 'editor.apply', draftId: draft.id, revision: 1, proposedSha256: savedDraft.proposedSha256, expectedLeaseRevision: 1 });
  });

  test('does not reread ephemeral content after the applied event is projected', async () => {
    const dispatch = vi.fn().mockResolvedValue(undefined);
    const readEditorDraft = vi.fn().mockResolvedValue({ draftId: draft.id, revision: 1, baseSha256, data: 'aGVsbG8=', byteLength: 5, encoding: 'utf-8' });
    const savedDraft: EditorDraftState = { ...draft, status: 'saved', nextRevision: 2, proposedSha256: '486ea46224d1bb4fb680f34f7c9ad96a8f24ec88be73ea8e5a6c65260e9cb8a7', proposedByteLength: 5 };
    const baseProduct = { ...initialProductState(), task: { id: 'task-1', title: 'Edit', status: 'running' }, controlLease: { clientId: 'client-1', revision: 1 } };
    const store = { dispatch, readEditorDraft } as unknown as AppStore;
    const { rerender } = render(<EditorPage product={{ ...baseProduct, editorDrafts: { [draft.id]: savedDraft } }} draftId={draft.id} store={store} t={createTranslator('zh-CN')} onBack={vi.fn()} />);

    await screen.findByRole('textbox', { name: draft.path });
    await userEvent.click(screen.getByRole('button', { name: '应用补丁' }));
    rerender(<EditorPage product={{ ...baseProduct, editorDrafts: { [draft.id]: { ...savedDraft, status: 'applied', resultSha256: savedDraft.proposedSha256, reviewer: 'client-1' } } }} draftId={draft.id} store={store} t={createTranslator('zh-CN')} onBack={vi.fn()} />);

    expect(await screen.findByRole('status')).toHaveTextContent('补丁已应用');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(readEditorDraft).toHaveBeenCalledTimes(1);
    expect(screen.getByRole('button', { name: '保存草稿' })).toBeDisabled();
    expect(screen.getByRole('button', { name: '丢弃草稿' })).toBeDisabled();
  });

  test('keeps the editor open and reports a failed discard', async () => {
    const onBack = vi.fn();
    const dispatch = vi.fn().mockRejectedValue(new Error('stale lease'));
    const readEditorDraft = vi.fn().mockResolvedValue({ draftId: draft.id, revision: 0, baseSha256, data: 'aGVsbG8=', byteLength: 5, encoding: 'utf-8' });
    const product = { ...initialProductState(), task: { id: 'task-1', title: 'Edit', status: 'running' }, controlLease: { clientId: 'client-1', revision: 1 }, editorDrafts: { [draft.id]: draft } };
    render(<EditorPage product={product} draftId={draft.id} store={{ dispatch, readEditorDraft } as unknown as AppStore} t={createTranslator('zh-CN')} onBack={onBack} />);

    await screen.findByRole('textbox', { name: draft.path });
    await userEvent.click(screen.getByRole('button', { name: '丢弃草稿' }));

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('文件已被外部修改'));
    expect(onBack).not.toHaveBeenCalled();
  });

  test('uses an injected editor surface without retaining file content in product state', async () => {
    const readEditorDraft = vi.fn().mockResolvedValue({ draftId: draft.id, revision: 0, baseSha256, data: 'aGVsbG8=', byteLength: 5, encoding: 'utf-8' });
    const product = { ...initialProductState(), task: { id: 'task-1', title: 'Edit', status: 'running' }, controlLease: { clientId: 'client-1', revision: 1 }, editorDrafts: { [draft.id]: draft } };
    render(<EditorPage product={product} draftId={draft.id} store={{ readEditorDraft } as unknown as AppStore} t={createTranslator('zh-CN')} onBack={vi.fn()} renderEditor={({ path, value }) => <div data-testid="injected-editor" data-path={path}>{value}</div>} />);

    expect(await screen.findByTestId('injected-editor')).toHaveTextContent('hello');
    expect(screen.getByTestId('injected-editor')).toHaveAttribute('data-path', draft.path);
    expect(JSON.stringify(product)).not.toContain('hello');
  });

  test('renders the evidence editor without accessibility violations', async () => {
    const readEditorDraft = vi.fn().mockResolvedValue({ draftId: draft.id, revision: 0, baseSha256, data: 'aGVsbG8=', byteLength: 5, encoding: 'utf-8' });
    const product = { ...initialProductState(), task: { id: 'task-1', title: 'Edit', status: 'running' }, controlLease: { clientId: 'client-1', revision: 1 }, editorDrafts: { [draft.id]: draft } };
    const { container } = render(<EditorPage product={product} draftId={draft.id} store={{ readEditorDraft } as unknown as AppStore} t={createTranslator('zh-CN')} onBack={vi.fn()} />);

    await screen.findByRole('textbox', { name: draft.path });
    const results = await axe(container, { rules: { 'color-contrast': { enabled: false } } });
    expect(results.violations).toEqual([]);
  });
});
