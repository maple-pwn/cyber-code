import Editor, { loader } from '@monaco-editor/react';
import * as localMonaco from 'monaco-editor/esm/vs/editor/edcore.main.js';
import 'monaco-editor/esm/vs/basic-languages/go/go.contribution.js';
import 'monaco-editor/esm/vs/basic-languages/markdown/markdown.contribution.js';
import 'monaco-editor/esm/vs/basic-languages/python/python.contribution.js';
import 'monaco-editor/esm/vs/basic-languages/rust/rust.contribution.js';
import 'monaco-editor/esm/vs/basic-languages/typescript/typescript.contribution.js';
import 'monaco-editor/esm/vs/basic-languages/yaml/yaml.contribution.js';
import 'monaco-editor/esm/vs/language/json/monaco.contribution.js';

import type { CodeEditorSurfaceProps } from '@cyber/product-app';

loader.config({ monaco: localMonaco });

const languageForPath = (path: string) => {
  const extension = path.split('.').pop()?.toLowerCase();
  return ({ go: 'go', ts: 'typescript', tsx: 'typescript', js: 'javascript', jsx: 'javascript', rs: 'rust', py: 'python', json: 'json', md: 'markdown', yaml: 'yaml', yml: 'yaml' } as Record<string, string>)[extension ?? ''] ?? 'plaintext';
};

export function MonacoEditorSurface({ path, value, readOnly, onChange }: CodeEditorSurfaceProps) {
  return <div className="editor-monaco" role="region" aria-label={path}>
    <Editor height="min(68vh, 720px)" path={path} language={languageForPath(path)} value={value} onChange={(next) => onChange(next ?? '')} theme="vs-dark" options={{ readOnly, minimap: { enabled: false }, automaticLayout: true, scrollBeyondLastLine: false, fontSize: 13, lineNumbersMinChars: 3, wordWrap: 'off', accessibilitySupport: 'auto' }} />
  </div>;
}
