import { useRef, useState, type FormEvent, type KeyboardEvent } from 'react';

import { createTranslator, type Translator } from '@cyber/i18n';
import type { RuntimeCommand } from '@cyber/runtime-client';

export type RuntimeOption = { id: 'scenario-local' | 'scenario-remote'; label: string; capabilities: readonly string[] };
export type NewTaskPageProps = {
  runtimes: readonly RuntimeOption[];
  t?: Translator;
  onCreate: (command: Extract<RuntimeCommand, { type: 'task.create' }>) => void | Promise<void>;
};

export function NewTaskPage({ runtimes, t = createTranslator(), onCreate }: NewTaskPageProps) {
  const [objective, setObjective] = useState('');
  const [runtimeId, setRuntimeId] = useState<RuntimeOption['id']>('scenario-local');
  const [workspace, setWorkspace] = useState('');
  const formRef = useRef<HTMLFormElement>(null);
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!objective.trim()) return;
    await onCreate({
      type: 'task.create', objective: objective.trim(), runtimeId,
      ...(workspace.trim() ? { workspace: workspace.trim() } : {}),
    });
  };
  const onKeyDown = (event: KeyboardEvent<HTMLFormElement>) => {
    if ((event.ctrlKey || event.metaKey) && event.key === 'Enter') {
      event.preventDefault();
      formRef.current?.requestSubmit();
    }
  };
  return <section className="page page-new-task" aria-labelledby="new-task-title">
    <h1 id="new-task-title">{t.t('task.new')}</h1>
    <form ref={formRef} onSubmit={submit} onKeyDown={onKeyDown}>
      <label>{t.t('task.objective')}<textarea required value={objective} onChange={(event) => setObjective(event.currentTarget.value)} /></label>
      <fieldset><legend>{t.t('runtime.label')}</legend>
        {runtimes.map((runtime) => <label key={runtime.id} className="runtime-choice">
          <input aria-label={runtime.label} type="radio" name="runtime" value={runtime.id} checked={runtimeId === runtime.id} onChange={() => setRuntimeId(runtime.id)} />
          <span><strong>{runtime.label}</strong><small>{runtime.capabilities.join(' · ')}</small></span>
        </label>)}
      </fieldset>
      <label>{t.t('task.workspace')}<input value={workspace} onChange={(event) => setWorkspace(event.currentTarget.value)} /></label>
      <button type="submit">{t.t('task.create')}</button>
    </form>
  </section>;
}
