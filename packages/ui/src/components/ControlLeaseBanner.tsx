import type { Translator } from '@cyber/i18n';
import type { ControlLease } from '@cyber/protocol';

export type ControlLeaseBannerProps = {
  lease: ControlLease | null;
  t: Translator;
  disabled?: boolean;
  onTakeControl: (expectedRevision: number) => void;
};

export function ControlLeaseBanner({ lease, t, disabled = false, onTakeControl }: ControlLeaseBannerProps) {
  return <section className="cyber-banner" aria-label={t.t('control.label')}>
    <span>{t.t('control.controller')}: {lease?.clientId ?? t.t('common.none')}</span>
    <span>{t.t('control.revision')}: {lease?.revision ?? 0}</span>
    <button type="button" disabled={disabled} onClick={() => onTakeControl(lease?.revision ?? 0)}>{t.t('control.take')}</button>
  </section>;
}
