import * as Dialog from '@radix-ui/react-dialog';
import { useRef } from 'react';

import type { Translator } from '@cyber/i18n';
import type { ImmutableEvidence } from '@cyber/protocol';

export type EvidenceDrawerProps = {
  evidence: ImmutableEvidence | null;
  open: boolean;
  t: Translator;
  onOpenChange: (open: boolean) => void;
};

export function EvidenceDrawer({ evidence, open, t, onOpenChange }: EvidenceDrawerProps) {
  const restoreFocus = useRef<HTMLElement | null>(null);
  return <Dialog.Root open={open} onOpenChange={onOpenChange}>
    <Dialog.Portal>
      <Dialog.Overlay className="cyber-dialog-overlay" />
      <Dialog.Content
        className="cyber-dialog"
        onOpenAutoFocus={() => { restoreFocus.current = document.activeElement as HTMLElement | null; }}
        onCloseAutoFocus={(event) => {
          event.preventDefault();
          restoreFocus.current?.focus();
        }}
      >
        <Dialog.Title>{t.t('evidence.title')}</Dialog.Title>
        <Dialog.Description>{t.t('evidence.immutable')}</Dialog.Description>
        {evidence && <>
          <h3>{evidence.summary}</h3>
          <p>{evidence.kind} · {evidence.id}</p>
          <pre tabIndex={0}>{JSON.stringify(evidence.data, null, 2)}</pre>
        </>}
        <Dialog.Close asChild><button type="button">{t.t('common.close')}</button></Dialog.Close>
      </Dialog.Content>
    </Dialog.Portal>
  </Dialog.Root>;
}
