import type { Translator } from '@cyber/i18n';
import type { ValidatedProductEvent } from '@cyber/protocol';

export type NarrativeStreamProps = { events: readonly ValidatedProductEvent[]; t: Translator };

export function NarrativeStream({ events, t }: NarrativeStreamProps) {
  return <section className="cyber-narrative" aria-labelledby="narrative-title">
    <h2 id="narrative-title">{t.t('mission.timeline')}</h2>
    {events.length === 0
      ? <p>{t.t('mission.empty')}</p>
      : <ol aria-live="polite">
          {events.map((event) => <li key={event.eventId}>
            <time dateTime={event.occurredAt}>{event.occurredAt}</time>
            <strong>{event.type}</strong>
          </li>)}
        </ol>}
  </section>;
}
