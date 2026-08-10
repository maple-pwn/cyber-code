import { useRef, useState, type FormEvent, type KeyboardEvent } from 'react';

import { createTranslator, type Translator } from '@cyber/i18n';
import type { RuntimeCommand, RuntimeInput } from '@cyber/runtime-client';
import type { RuntimeSourceMode } from '@cyber/runtime-client';

export type RuntimeOption = {
  id: string;
  mode: RuntimeSourceMode;
  label: string;
  capabilities: readonly string[];
  available: boolean;
  setupStatus?: string;
};
export type NewTaskPageProps = {
  runtimes: readonly RuntimeOption[];
  t?: Translator;
  onCreate: (command: Extract<RuntimeCommand, { type: 'task.create' }>) => void | Promise<void>;
  pickInputs?: () => Promise<RuntimeInput[]>;
};

export function NewTaskPage({ runtimes, t = createTranslator(), onCreate, pickInputs }: NewTaskPageProps) {
  const [objective, setObjective] = useState('');
  const [runtimeId, setRuntimeId] = useState(runtimes.find((runtime) => runtime.available)?.id ?? '');
  const [workspace, setWorkspace] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [files, setFiles] = useState<File[]>([]);
  const [nativeInputs, setNativeInputs] = useState<RuntimeInput[]>([]);
  const [error, setError] = useState('');
  const submittingRef = useRef(false);
  const formRef = useRef<HTMLFormElement>(null);
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (submittingRef.current || !objective.trim() || !runtimes.find((runtime) => runtime.id === runtimeId)?.available) return;
    submittingRef.current = true;
    setSubmitting(true);
    setError('');
    try {
      const browserInputs: RuntimeInput[] = await Promise.all(files.map(async (file) => ({
        filename: file.name,
        mediaType: file.type || 'application/octet-stream',
        bytes: await readBrowserFile(file),
      })));
      const inputs = [...nativeInputs, ...browserInputs];
      await onCreate({
        type: 'task.create', objective: objective.trim(), runtimeId,
        ...(workspace.trim() ? { workspace: workspace.trim() } : {}),
        ...(inputs.length > 0 ? { inputs } : {}),
      });
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      submittingRef.current = false;
      setSubmitting(false);
    }
  };
  const onKeyDown = (event: KeyboardEvent<HTMLFormElement>) => {
    if ((event.ctrlKey || event.metaKey) && event.key === 'Enter') {
      event.preventDefault();
      formRef.current?.requestSubmit();
    }
  };
  return <section className="page page-new-task" aria-labelledby="new-task-title">
    <h1 id="new-task-title">{t.t('task.new')}</h1>
    <form className="task-creation-surface cyber-glass" data-testid="new-task-surface" ref={formRef} onSubmit={submit} onKeyDown={onKeyDown}>
      <label>{t.t('task.objective')}<textarea required value={objective} onChange={(event) => setObjective(event.currentTarget.value)} /></label>
      <fieldset><legend>{t.t('runtime.label')}</legend>
        {runtimes.map((runtime) => <label key={runtime.id} className="runtime-choice">
          <input aria-label={runtime.label} type="radio" name="runtime" value={runtime.id} disabled={!runtime.available} checked={runtimeId === runtime.id} onChange={() => setRuntimeId(runtime.id)} />
          <span><strong>{runtime.label}</strong><small>{runtime.mode.toUpperCase()} · {runtime.capabilities.join(' · ')}</small>
            {runtime.setupStatus && <small className="runtime-setup-status" role={runtime.available ? undefined : 'status'}>{runtime.setupStatus}</small>}
          </span>
        </label>)}
      </fieldset>
      <label className="local-workspace-control">{t.t('task.workspace')}<input value={workspace} onChange={(event) => setWorkspace(event.currentTarget.value)} /></label>
      <label>{t.t('task.inputs')}<input type="file" multiple onChange={(event) => setFiles(Array.from(event.currentTarget.files ?? []))} /></label>
      {pickInputs && <button type="button" onClick={() => void pickInputs().then(setNativeInputs).catch((cause) => setError(cause instanceof Error ? cause.message : String(cause)))}>{t.t('task.pickInputs')}</button>}
      {(files.length > 0 || nativeInputs.length > 0) && <ul className="task-input-list">{[...nativeInputs.map((input) => `${input.filename}:${input.bytes.byteLength}`), ...files.map((file) => `${file.name}:${file.size}`)].map((name) => <li key={name}>{name.replace(':', ' · ')} B</li>)}</ul>}
      {error && <p role="alert">{error}</p>}
      <button type="submit" disabled={submitting}>{submitting ? t.t('task.uploading') : t.t('task.create')}</button>
    </form>
  </section>;
}

async function readBrowserFile(file: File): Promise<Uint8Array> {
  if (file.size === 0) throw new Error('task_input_empty');
  if (file.size > 64 * 1024 * 1024) throw new Error('task_input_too_large');
  if (typeof file.arrayBuffer === 'function') return new Uint8Array(await file.arrayBuffer());
  return new Promise<Uint8Array>((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(new Error('task_input_read_failed'));
    reader.onload = () => reader.result instanceof ArrayBuffer
      ? resolve(new Uint8Array(reader.result))
      : reject(new Error('task_input_read_failed'));
    reader.readAsArrayBuffer(file);
  });
}
