// Monaco, the lazily loaded half of code-editor.tsx: only the editor core and the contributions
// the editor uses (find, clipboard, line and word operations, Ctrl+M tab focus, bracket matching),
// Markdown and YAML highlighting, one editor worker, and a theme from MASTER's tokens. It grows
// with its text between minLines and maxLines. Accessibility support is on (D §10.12).
import 'monaco-editor/features/bracketMatching/register';
import 'monaco-editor/features/clipboard/register';
import 'monaco-editor/features/codicon/register';
import 'monaco-editor/features/contextmenu/register';
import 'monaco-editor/features/cursorUndo/register';
import 'monaco-editor/features/find/register';
import 'monaco-editor/features/linesOperations/register';
import 'monaco-editor/features/readOnlyMessage/register';
import 'monaco-editor/features/toggleTabFocusMode/register';
import 'monaco-editor/features/wordOperations/register';
import 'monaco-editor/languages/definitions/markdown/register';
import 'monaco-editor/languages/definitions/yaml/register';
import * as monaco from 'monaco-editor/editor/editor.api';
import EditorWorker from 'monaco-editor/editor/editor.worker?worker';
import { useEffect, useRef } from 'react';
import type { CodeEditorProps } from './code-editor';

self.MonacoEnvironment = { getWorker: () => new EditorWorker() };

monaco.editor.defineTheme('goal', {
  base: 'vs',
  inherit: true,
  rules: [
    { token: 'keyword', foreground: '185A71', fontStyle: 'bold' },
    { token: 'type', foreground: '185A71' },
    { token: 'string', foreground: '24563E' },
    { token: 'comment', foreground: '556270' },
    { token: 'variable', foreground: '4A3C9E' },
  ],
  colors: {
    'editor.background': '#FFFFFF',
    'editor.foreground': '#1D2733',
    'editorLineNumber.foreground': '#5F6B67',
    'editorLineNumber.activeForeground': '#1D2733',
    'editorCursor.foreground': '#1F6F8B',
    'editor.selectionBackground': '#BFE0EA',
    'editor.lineHighlightBackground': '#F6F7F5',
    'editorIndentGuide.background1': '#E3E7E5',
  },
});

const lineHeight = 23;

export default function MonacoEditor({
  value,
  onChange,
  onBlur,
  language,
  label,
  readOnly = false,
  minLines = 8,
  maxLines = 30,
}: CodeEditorProps) {
  const host = useRef<HTMLDivElement>(null);
  const editor = useRef<monaco.editor.IStandaloneCodeEditor | null>(null);
  // The latest callbacks, read by Monaco's listeners without recreating the editor.
  const handlers = useRef({ onChange, onBlur });
  handlers.current = { onChange, onBlur };
  const latest = useRef(value);
  latest.current = value;

  useEffect(() => {
    const el = host.current;
    if (!el) return;
    const ed = monaco.editor.create(el, {
      value: latest.current,
      language,
      theme: 'goal',
      ariaLabel: label,
      accessibilitySupport: 'on',
      readOnly,
      automaticLayout: true,
      minimap: { enabled: false },
      scrollBeyondLastLine: false,
      wordWrap: 'on',
      wrappingIndent: 'same',
      lineNumbers: 'on',
      lineNumbersMinChars: 3,
      lineDecorationsWidth: 12,
      folding: false,
      glyphMargin: false,
      renderLineHighlight: 'line',
      overviewRulerLanes: 0,
      hideCursorInOverviewRuler: true,
      scrollbar: { alwaysConsumeMouseWheel: false, verticalScrollbarSize: 10 },
      fontFamily: '"Fira Code", ui-monospace, monospace',
      fontSize: 13,
      lineHeight,
      padding: { top: 10, bottom: 10 },
      tabSize: 2,
      unicodeHighlight: { ambiguousCharacters: false },
      quickSuggestions: false,
      wordBasedSuggestions: 'off',
    });
    editor.current = ed;

    const resize = () => {
      const lines = Math.min(maxLines, Math.max(minLines, Math.ceil(ed.getContentHeight() / lineHeight)));
      el.style.height = `${lines * lineHeight}px`;
    };
    resize();
    const subscriptions = [
      ed.onDidContentSizeChange(resize),
      ed.onDidChangeModelContent((e) => {
        // A value put in from outside (below) is not the person typing.
        if (!e.isFlush) handlers.current.onChange(ed.getValue());
      }),
      ed.onDidBlurEditorText(() => handlers.current.onBlur?.()),
    ];
    return () => {
      for (const s of subscriptions) s.dispose();
      ed.getModel()?.dispose();
      ed.dispose();
      editor.current = null;
    };
  }, [language, label, readOnly, minLines, maxLines]);

  // The session's text changed under the editor (a reload, a conflict resolved): show it.
  useEffect(() => {
    const ed = editor.current;
    if (ed && ed.getValue() !== value) ed.setValue(value);
  }, [value]);

  return <div ref={host} className="w-full" style={{ height: minLines * lineHeight }} />;
}
