import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import '@cyber/ui/tokens.css';
import '@cyber/ui/components.css';
import { ProductApp, createAppStore } from '@cyber/product-app';
import '@cyber/product-app/styles.css';
import { createWebSourceFactory, readWebRuntimeConfiguration } from './source-factory';

const sources = createWebSourceFactory(readWebRuntimeConfiguration());
const store = createAppStore(sources, 'new-task');

void store.connect();

createRoot(document.getElementById('root') as HTMLElement).render(
  <StrictMode><ProductApp store={store} runtimes={sources.options()} /></StrictMode>,
);
