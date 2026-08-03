import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { RuntimeClient } from '@cyber/runtime-client';
import { ScenarioPlayer } from '@cyber/scenario-player';
import '@cyber/ui/tokens.css';
import '@cyber/ui/components.css';

import { App } from './App';
import { createAppStore } from './app-store';
import './styles.css';

const source = new ScenarioPlayer({ runtimeId: 'scenario-local', speedMs: 80 });
const client = new RuntimeClient(source);
const store = createAppStore(client, 'new-task');

void store.connect();

createRoot(document.getElementById('root') as HTMLElement).render(<StrictMode><App store={store} /></StrictMode>);
