import { lazy, StrictMode, Suspense } from 'react';
import { createRoot } from 'react-dom/client';

import { ProductApp } from '@cyber/product-app';
import '@cyber/product-app/styles.css';
import '@cyber/ui/tokens.css';
import '@cyber/ui/components.css';

import { createDesktopBootstrap } from './bootstrap';

const MonacoEditorSurface = lazy(() => import('./MonacoEditorSurface').then(({ MonacoEditorSurface: surface }) => ({ default: surface })));

const { store, runtimes } = createDesktopBootstrap();
void store.connect();

createRoot(document.getElementById('root') as HTMLElement).render(
  <StrictMode><ProductApp store={store} runtimes={runtimes} renderEditor={(props) => <Suspense fallback={<div className="editor-monaco-loading" role="status" aria-busy="true">Loading editor</div>}><MonacoEditorSurface {...props} /></Suspense>} /></StrictMode>,
);
