import { Component, useEffect, useRef, useState, useSyncExternalStore, type ErrorInfo, type ReactNode } from 'react';

import { createTranslator, setLocale, type Locale } from '@cyber/i18n';
import { CommandPalette } from '@cyber/ui';

import type { AppRoute, AppStore } from './app-store';
import { FindingsPage } from './pages/FindingsPage';
import { MissionControlPage } from './pages/MissionControlPage';
import { NewTaskPage } from './pages/NewTaskPage';
import { ReportsPage } from './pages/ReportsPage';
import { ScopeReviewPage } from './pages/ScopeReviewPage';

const routes: { route: AppRoute; key: 'nav.newTask' | 'nav.missionControl' | 'nav.findings' | 'nav.reports'; chord: string }[] = [
  { route: 'new-task', key: 'nav.newTask', chord: 'n' },
  { route: 'mission-control', key: 'nav.missionControl', chord: 'm' },
  { route: 'findings', key: 'nav.findings', chord: 'f' },
  { route: 'reports', key: 'nav.reports', chord: 'r' },
];

class AppErrorBoundary extends Component<{ children: ReactNode }, { error?: Error }> {
  state: { error?: Error } = {};
  static getDerivedStateFromError(error: Error) { return { error }; }
  componentDidCatch(error: Error, info: ErrorInfo) { console.error('cyber_app_error', error, info); }
  render() { return this.state.error ? <main><h1>CYBER</h1><p role="alert">{this.state.error.message}</p></main> : this.props.children; }
}

export function App({ store }: { store: AppStore }) {
  const snapshot = useSyncExternalStore(store.subscribe, store.getSnapshot);
  const [locale, updateLocale] = useState<Locale>('zh-CN');
  const [paletteOpen, setPaletteOpen] = useState(false);
  const chord = useRef<{ key: string; timer?: number }>({ key: '' });
  const t = createTranslator(locale);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault(); setPaletteOpen(true); return;
      }
      if (event.ctrlKey || event.metaKey || event.altKey || event.target instanceof HTMLInputElement || event.target instanceof HTMLTextAreaElement) return;
      if (chord.current.key === 'g') {
        const target = routes.find((route) => route.chord === event.key.toLowerCase());
        window.clearTimeout(chord.current.timer);
        chord.current = { key: '' };
        if (target) { event.preventDefault(); store.navigate(target.route); }
      } else if (event.key.toLowerCase() === 'g') {
        chord.current = { key: 'g', timer: window.setTimeout(() => { chord.current = { key: '' }; }, 700) };
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => { window.removeEventListener('keydown', onKeyDown); window.clearTimeout(chord.current.timer); };
  }, [store]);

  const setAppLocale = (next: Locale) => { setLocale(next); updateLocale(next); };
  const navigate = (route: AppRoute) => { store.navigate(route); setPaletteOpen(false); };
  const renderPage = () => {
    switch (snapshot.route) {
      case 'new-task': return <NewTaskPage t={t} runtimes={[
        { id: 'scenario-local', label: t.t('runtime.local'), capabilities: ['isolated', 'deterministic'] },
        { id: 'scenario-remote', label: t.t('runtime.remote'), capabilities: ['connected', 'managed'] },
      ]} onCreate={async (command) => { await store.dispatch(command); navigate('scope-review'); }} />;
      case 'scope-review': return snapshot.view.product.scope
        ? <ScopeReviewPage scope={snapshot.view.product.scope} runtime={{ id: 'scenario-local', label: t.t('runtime.local') }} t={t} onEdit={() => navigate('new-task')} onConfirm={async (scopeId) => { await store.dispatch({ type: 'scope.confirm', scopeId }); navigate('mission-control'); }} />
        : <p>{t.t('common.loading')}</p>;
      case 'mission-control': return <MissionControlPage view={snapshot.view} t={t} onDispatch={(command) => store.dispatch(command)} onReconnect={() => void store.reconnect()} />;
      case 'findings': return <FindingsPage product={snapshot.view.product} t={t} />;
      case 'reports': return <ReportsPage product={snapshot.view.product} t={t} />;
    }
  };

  return <AppErrorBoundary>
    <a className="skip-link" href="#main-content">Skip to content</a>
    <div className="app-shell">
      <header className="topbar"><strong>{t.t('app.name')}</strong><span>{t.t(`connection.${snapshot.view.connection.status}`)}</span>
        <div className="locale-control" aria-label="Language">
          <button type="button" aria-pressed={locale === 'zh-CN'} onClick={() => setAppLocale('zh-CN')}>{t.t('locale.zhCN')}</button>
          <button type="button" aria-pressed={locale === 'en'} onClick={() => setAppLocale('en')}>{t.t('locale.en')}</button>
        </div>
      </header>
      <nav className="sidebar" aria-label="Primary">{routes.map((item) => <button key={item.route} type="button" aria-current={snapshot.route === item.route ? 'page' : undefined} onClick={() => navigate(item.route)}>{t.t(item.key)}</button>)}</nav>
      <main id="main-content" className="workspace">{renderPage()}</main>
    </div>
    <CommandPalette open={paletteOpen} commands={routes.map((item) => ({ id: item.route, label: t.t(item.key) }))} t={t} onOpenChange={setPaletteOpen} onCommand={(id) => navigate(id as AppRoute)} />
  </AppErrorBoundary>;
}
