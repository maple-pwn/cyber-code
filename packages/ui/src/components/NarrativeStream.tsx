import type { Translator } from '@cyber/i18n';
import type { ValidatedProductEvent } from '@cyber/protocol';

export type NarrativeStreamProps = { events: readonly ValidatedProductEvent[]; t: Translator };

export function NarrativeStream({ events, t }: NarrativeStreamProps) {
  const dateTime = new Intl.DateTimeFormat(t.locale, {
    dateStyle: 'short',
    timeStyle: 'medium',
  });
  return <section className="cyber-narrative" aria-labelledby="narrative-title">
    <h2 id="narrative-title">{t.t('mission.timeline')}</h2>
    {events.length === 0
      ? <p>{t.t('mission.empty')}</p>
      : <ol aria-label={t.t('mission.timeline')} aria-live="polite" tabIndex={0}>
          {events.map((event) => <li key={event.eventId}>
            <i className="cyber-event-marker" aria-hidden="true" />
            <div><strong>{event.type}</strong><span className="cyber-event-source">{event.source.agentId ?? event.source.runtimeId} · cursor {event.cursor}</span></div>
            <time dateTime={event.occurredAt}>{dateTime.format(new Date(event.occurredAt))}</time>
          </li>)}
        </ol>}
  </section>;
}
