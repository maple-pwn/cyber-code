import { Component, useEffect, useRef, useState, useSyncExternalStore, type ErrorInfo, type ReactNode } from 'react';
import { Activity, FileText, Network, Radar, ShieldPlus, type LucideIcon } from 'lucide-react';

import { createTranslator, setLocale, type Locale } from '@cyber/i18n';
import { CommandPalette } from '@cyber/ui';

import type { AppRoute, AppStore } from './app-store';
import type { RuntimeInput } from '@cyber/runtime-client';
import { FindingsPage } from './pages/FindingsPage';
import { MissionControlPage } from './pages/MissionControlPage';
import { NewTaskPage } from './pages/NewTaskPage';
import type { RuntimeOption } from './pages/NewTaskPage';
import { ReportsPage } from './pages/ReportsPage';
import { ScopeReviewPage } from './pages/ScopeReviewPage';
import { EditorPage } from './pages/EditorPage';
import { AssetGraphPage } from './pages/AssetGraphPage';
import type { CodeEditorSurfaceProps } from './pages/EditorPage';

type RouteKey = 'nav.newTask' | 'nav.missionControl' | 'nav.findings' | 'nav.reports' | 'nav.assetGraph';

const routes: { route: AppRoute; key: RouteKey; chord: string; icon: LucideIcon }[] = [
  { route: 'new-task', key: 'nav.newTask', chord: 'n', icon: ShieldPlus },
  { route: 'mission-control', key: 'nav.missionControl', chord: 'm', icon: Radar },
  { route: 'findings', key: 'nav.findings', chord: 'f', icon: Activity },
  { route: 'reports', key: 'nav.reports', chord: 'r', icon: FileText },
  { route: 'asset-graph', key: 'nav.assetGraph', chord: 'a', icon: Network },
];

class AppErrorBoundary extends Component<{ children: ReactNode }, { error?: Error }> {
  state: { error?: Error } = {};
  static getDerivedStateFromError(error: Error) { return { error }; }
  componentDidCatch(error: Error, info: ErrorInfo) { console.error('cyber_app_error', error, info); }
  render() { return this.state.error ? <main><h1>CYBER</h1><p role="alert">{this.state.error.message}</p></main> : this.props.children; }
}

export type ProductAppProps = { store: AppStore; runtimes?: readonly RuntimeOption[]; renderEditor?: (props: CodeEditorSurfaceProps) => ReactNode; pickInputs?: () => Promise<RuntimeInput[]> };

export function ProductApp({ store, runtimes, renderEditor, pickInputs }: ProductAppProps) {
  const snapshot = useSyncExternalStore(store.subscribe, store.getSnapshot);
  const [locale, updateLocale] = useState<Locale>('zh-CN');
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [activeDraftId, setActiveDraftId] = useState('');
  const paletteRestoreFocus = useRef<HTMLElement | null>(null);
  const chord = useRef<{ key: string; timer?: number }>({ key: '' });
  const t = createTranslator(locale);
  const availableRuntimes = runtimes ?? [
    { id: 'scenario-local', mode: 'demo' as const, label: t.t('runtime.local'), capabilities: ['isolated', 'deterministic'], available: true },
    { id: 'scenario-remote', mode: 'demo' as const, label: t.t('runtime.remote'), capabilities: ['connected', 'managed'], available: true },
  ];
  const trustedSource = snapshot.view.source;

  useEffect(() => {
    const closePalette = () => {
      setPaletteOpen(false);
      const target = paletteRestoreFocus.current;
      paletteRestoreFocus.current = null;
      window.requestAnimationFrame(() => { if (target?.isConnected) target.focus(); });
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (paletteOpen && event.key === 'Escape') {
        event.preventDefault(); closePalette(); return;
      }
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault();
        if (!paletteOpen) paletteRestoreFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        setPaletteOpen(true);
        return;
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
  }, [paletteOpen, store]);

  const setPalette = (open: boolean) => {
    if (open) {
      if (!paletteOpen) paletteRestoreFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      setPaletteOpen(true);
      return;
    }
    setPaletteOpen(false);
    const target = paletteRestoreFocus.current;
    paletteRestoreFocus.current = null;
    window.requestAnimationFrame(() => { if (target?.isConnected) target.focus(); });
  };

  const setAppLocale = (next: Locale) => { setLocale(next); updateLocale(next); };
  const navigate = (route: AppRoute) => { store.navigate(route); setPaletteOpen(false); };
  const renderPage = () => {
    switch (snapshot.route) {
      case 'new-task': return <NewTaskPage t={t} runtimes={availableRuntimes} pickInputs={pickInputs} onCreate={async (command) => { await store.dispatch(command); navigate('scope-review'); }} />;
      case 'scope-review': return snapshot.view.product.scope
        ? <ScopeReviewPage scope={snapshot.view.product.scope} runtime={{
            id: trustedSource?.runtimeId ?? 'unavailable',
            label: trustedSource ? `${trustedSource.mode.charAt(0).toUpperCase()}${trustedSource.mode.slice(1)}` : t.t('common.none'),
          }} t={t} onEdit={() => void store.dispatch({ type: 'instruction.send', content: 'request_scope_revision' })} onConfirm={(scopeId) => {
            navigate('mission-control');
            void store.dispatch({ type: 'scope.confirm', scopeId }).catch((error: unknown) => {
              console.error('scope_confirmation_failed', error);
            });
          }} />
        : <p>{t.t('common.loading')}</p>;
      case 'mission-control': return <MissionControlPage view={snapshot.view} t={t} onDispatch={(command) => store.dispatch(command)} onReconnect={() => void store.reconnect()} onDisconnect={() => void store.disconnect()} />;
      case 'findings': return <FindingsPage product={snapshot.view.product} t={t} store={store} editorAvailable={trustedSource?.capabilities.includes('editor.read') === true && trustedSource.capabilities.includes('editor.write')} onOpenEditor={(draftId) => { setActiveDraftId(draftId); navigate('editor'); }} />;
      case 'editor': {
        const draftId = activeDraftId || Object.keys(snapshot.view.product.editorDrafts)[0] || '';
        return <EditorPage product={snapshot.view.product} draftId={draftId} store={store} t={t} onBack={() => navigate('findings')} renderEditor={renderEditor} />;
      }
      case 'reports': return <ReportsPage product={snapshot.view.product} source={snapshot.view.source} t={t} />;
      case 'asset-graph': return <AssetGraphPage product={snapshot.view.product} t={t} onOpenReport={() => navigate('reports')} />;
    }
  };

  return <AppErrorBoundary>
    <a className="skip-link" href="#main-content">Skip to content</a>
    <div className={`app-shell app-canvas route-${snapshot.route}`} data-testid="app-canvas">
      <header className="topbar product-bar cyber-glass"><strong>CYBER</strong><span className="product-context">{t.t('app.name')}</span><span className="connection-state" data-status={snapshot.view.connection.status}>{t.t(`connection.${snapshot.view.connection.status}`)}</span>
        <div className="locale-control" aria-label="Language">
          <button type="button" aria-pressed={locale === 'zh-CN'} onClick={() => setAppLocale('zh-CN')}>{t.t('locale.zhCN')}</button>
          <button type="button" aria-pressed={locale === 'en'} onClick={() => setAppLocale('en')}>{t.t('locale.en')}</button>
        </div>
      </header>
      <nav className="sidebar navigation-rail cyber-glass" aria-label="Primary">{routes.map((item) => {
        const Icon = item.icon;
        const label = t.t(item.key);
        return <button key={item.route} type="button" aria-label={label} title={label} aria-current={snapshot.route === item.route ? 'page' : undefined} onClick={() => navigate(item.route)}>
          <Icon data-testid="nav-icon" aria-hidden="true" size={19} strokeWidth={1.8} />
          <span className="cyber-visually-hidden">{label}</span>
        </button>;
      })}</nav>
      <main id="main-content" className="workspace">{renderPage()}</main>
    </div>
    <CommandPalette open={paletteOpen} commands={routes.map((item) => ({ id: item.route, label: t.t(item.key) }))} t={t} onOpenChange={setPalette} onCommand={(id) => navigate(id as AppRoute)} />
  </AppErrorBoundary>;
}
