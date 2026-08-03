import { useState } from 'react';

import type { Translator } from '@cyber/i18n';

export type PaletteCommand = { id: string; label: string };
export type CommandPaletteProps = {
  open: boolean;
  commands: readonly PaletteCommand[];
  t: Translator;
  onOpenChange: (open: boolean) => void;
  onCommand: (commandId: string) => void;
};

export function CommandPalette({ open, commands, t, onOpenChange, onCommand }: CommandPaletteProps) {
  const [query, setQuery] = useState('');
  const visible = commands.filter((command) => command.label.toLocaleLowerCase().includes(query.toLocaleLowerCase()));
  if (!open) return null;
  return <>
    <div className="cyber-dialog-overlay" aria-hidden="true" />
    <section
      className="cyber-dialog"
      role="dialog"
      aria-modal="true"
      aria-labelledby="command-palette-title"
      onKeyDown={(event) => { if (event.key === 'Escape') onOpenChange(false); }}
    >
      <h2 id="command-palette-title">{t.t('command.open')}</h2>
      <input aria-label={t.t('command.placeholder')} value={query} onChange={(event) => setQuery(event.currentTarget.value)} />
      {visible.length === 0 ? <p>{t.t('command.noResults')}</p> : <ul>{visible.map((command) => <li key={command.id}>
        <button type="button" onClick={() => { onCommand(command.id); onOpenChange(false); }}>{command.label}</button>
      </li>)}</ul>}
      <button type="button" onClick={() => onOpenChange(false)}>{t.t('common.close')}</button>
    </section>
  </>;
}
