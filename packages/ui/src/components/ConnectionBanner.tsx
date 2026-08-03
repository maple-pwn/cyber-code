import type { Translator } from '@cyber/i18n';
import type { ConnectionState } from '@cyber/runtime-client';

export type ConnectionBannerProps = { connection: ConnectionState; t: Translator; onReconnect?: () => void };

export function ConnectionBanner({ connection, t, onReconnect }: ConnectionBannerProps) {
  const unhealthy = connection.status !== 'healthy';
  return <section className={`cyber-banner cyber-status-${unhealthy ? 'warning' : 'success'}`} role="status">
    <strong>{t.t(`connection.${connection.status}`)}</strong>
    <span>cursor {connection.lastTrustedCursor}</span>
    {connection.errorCode && <code>{connection.errorCode}</code>}
    {unhealthy && <span>{t.t('connection.readOnly')}</span>}
    {onReconnect && ['offline', 'degraded'].includes(connection.status) && <button type="button" onClick={onReconnect}>{t.t('connection.reconnect')}</button>}
  </section>;
}
