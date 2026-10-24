// Monaco, the lazily loaded half of code-editor.tsx: only the editor core and the contributions
// the editor uses (find, clipboard, line and word operations, Ctrl+M tab focus, bracket matching,
// hover and suggestions for the schema), Markdown and YAML highlighting, monaco-yaml bound to
// schema/factory.schema.json for files named factory.yaml (completion, hover, schema markers; D
// §10.9), the editor and YAML workers, and a theme from MASTER's tokens. It grows with its text
// between minLines and maxLines. Accessibility support is on (D §10.12). For the YAML view: one
// model per path, B's problems as markers with a rust glyph and a tint on their lines, the change
// gutter, and blocks under lines (view zones) that React renders through portals, sized to their
// content. A value from outside (a Visual save, another tab) is one undoable edit that keeps the
// cursor.
import 'monaco-editor/features/bracketMatching/register';
import 'monaco-editor/features/clipboard/register';
import 'monaco-editor/features/codicon/register';
import 'monaco-editor/features/contextmenu/register';
import 'monaco-editor/features/cursorUndo/register';
import 'monaco-editor/features/find/register';
import 'monaco-editor/features/hover/register';
import 'monaco-editor/features/linesOperations/register';
import 'monaco-editor/features/readOnlyMessage/register';
import 'monaco-editor/features/suggest/register';
import 'monaco-editor/features/toggleTabFocusMode/register';
import 'monaco-editor/features/wordOperations/register';
import 'monaco-editor/languages/definitions/markdown/register';
import 'monaco-editor/languages/definitions/yaml/register';
import * as monaco from 'monaco-editor/editor/editor.api';
import EditorWorker from 'monaco-editor/editor/editor.worker?worker';
import { configureMonacoYaml } from 'monaco-yaml';
import { type ReactNode, useEffect, useImperativeHandle, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import schema from '../../../../../schema/factory.schema.json';
import type { CodeEditorProps } from './code-editor';
import './monaco-editor.css';
import YamlWorker from './yaml.worker.ts?worker';

self.MonacoEnvironment = { getWorker: () => new EditorWorker() };

interface LegacyWorkerOptions {
  createData?: unknown;
  host?: unknown;
  keepIdleModels?: boolean;
}

/**
 * monaco-yaml 5 asks for its worker the way Monaco did before 0.57 ({moduleId, label,
 * createData}); 0.57 takes the worker itself, which reads its creation data from its second
 * message (as Monaco's own language workers do), so monaco-yaml gets a Monaco whose
 * createWebWorker starts the YAML worker that way.
 */
const yamlMonaco = {
  ...monaco,
  editor: {
    ...monaco.editor,
    createWebWorker: ({ createData, host, keepIdleModels }: LegacyWorkerOptions) => {
      const worker = new YamlWorker();
      worker.postMessage('ignore');
      worker.postMessage(createData);
      return monaco.editor.createWebWorker({ worker, host, keepIdleModels } as never);
    },
  },
};

configureMonacoYaml(yamlMonaco as never, {
  enableSchemaRequest: false,
  // Format is B's encoder (POST /definitions/format), so a later Visual save doesn't churn the file.
  format: { enable: false },
  schemas: [
    {
      // The schema's $id names it; it is never fetched.
      uri: 'https://goal.local/schema/factory.schema.json',
      fileMatch: ['**/factory.yaml', 'factory.yaml'],
      schema: schema as never,
    },
  ],
});

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
    'editorError.foreground': '#B4441C',
    'editorWarning.foreground': '#B4441C',
  },
});

const lineHeight = 23;

const severities = {
  error: monaco.MarkerSeverity.Error,
  warning: monaco.MarkerSeverity.Warning,
  info: monaco.MarkerSeverity.Info,
};

interface Zone {
  key: string;
  node: HTMLDivElement;
  /** The object Monaco was given: layoutZone reads its height from it again. */
  zone: { afterLineNumber: number; heightInPx: number; domNode: HTMLElement };
  id: string;
}

export default function MonacoEditor({
  value,
  onChange,
  onBlur,
  language,
  label,
  readOnly = false,
  minLines = 8,
  maxLines = 30,
  path,
  markers = [],
  changedLines = [],
  zones = [],
  onCursor,
  ref,
}: CodeEditorProps) {
  const host = useRef<HTMLDivElement>(null);
  const [editor, setEditor] = useState<monaco.editor.IStandaloneCodeEditor | null>(null);
  const [mounted, setMounted] = useState<Zone[]>([]);
  // The latest callbacks, read by Monaco's listeners without recreating the editor.
  const handlers = useRef({ onChange, onBlur, onCursor });
  handlers.current = { onChange, onBlur, onCursor };
  const latest = useRef(value);
  latest.current = value;

  useEffect(() => {
    const el = host.current;
    if (!el) return;
    // A path names the model (factory.yaml gets the schema); one model per path at a time.
    const uri = path ? monaco.Uri.parse(`file:///${path}`) : undefined;
    if (uri) monaco.editor.getModel(uri)?.dispose();
    const model = monaco.editor.createModel(latest.current, language, uri);
    const ed = monaco.editor.create(el, {
      model,
      theme: 'goal',
      ariaLabel: label,
      accessibilitySupport: 'on',
      readOnly,
      automaticLayout: true,
      minimap: { enabled: false },
      scrollBeyondLastLine: false,
      wordWrap: language === 'markdown' ? 'on' : 'off',
      wrappingIndent: 'same',
      lineNumbers: 'on',
      lineNumbersMinChars: 3,
      lineDecorationsWidth: 14,
      folding: false,
      glyphMargin: language !== 'markdown',
      renderLineHighlight: 'line',
      overviewRulerLanes: 0,
      hideCursorInOverviewRuler: true,
      fixedOverflowWidgets: true,
      scrollbar: { alwaysConsumeMouseWheel: false, verticalScrollbarSize: 10, horizontalScrollbarSize: 10 },
      fontFamily: '"Fira Code", ui-monospace, monospace',
      fontSize: 13,
      lineHeight,
      padding: { top: 10, bottom: 10 },
      tabSize: 2,
      insertSpaces: true,
      unicodeHighlight: { ambiguousCharacters: false },
      quickSuggestions: language === 'yaml',
      wordBasedSuggestions: 'off',
    });

    const resize = () => {
      const lines = Math.min(maxLines, Math.max(minLines, Math.ceil(ed.getContentHeight() / lineHeight)));
      el.style.height = `${lines * lineHeight}px`;
    };
    // Blocks under lines are as wide as the visible text area, not the longest line.
    const zoneWidth = () => {
      const l = ed.getLayoutInfo();
      el.style.setProperty('--goal-zone-width', `${Math.max(200, l.width - l.contentLeft - 24)}px`);
    };
    resize();
    zoneWidth();
    const subscriptions = [
      ed.onDidContentSizeChange(resize),
      ed.onDidLayoutChange(zoneWidth),
      ed.onDidChangeModelContent((e) => {
        // A value put in from outside (below) is not the person typing.
        if (!e.isFlush && ed.getValue() !== latest.current) handlers.current.onChange(ed.getValue());
      }),
      ed.onDidChangeCursorPosition((e) =>
        handlers.current.onCursor?.({ line: e.position.lineNumber, column: e.position.column }),
      ),
      ed.onDidBlurEditorText(() => handlers.current.onBlur?.()),
    ];
    setEditor(ed);
    return () => {
      for (const s of subscriptions) s.dispose();
      ed.dispose();
      model.dispose();
      setEditor(null);
    };
  }, [language, label, readOnly, minLines, maxLines, path]);

  // The session's text changed under the editor (a Visual save, a reload, a conflict resolved):
  // one undoable edit, the cursor kept.
  useEffect(() => {
    const model = editor?.getModel();
    if (!editor || !model || model.getValue() === value) return;
    const selections = editor.getSelections();
    editor.pushUndoStop();
    editor.executeEdits('goal', [{ range: model.getFullModelRange(), text: value, forceMoveMarkers: true }]);
    if (selections) editor.setSelections(selections);
    editor.pushUndoStop();
  }, [editor, value]);

  useEffect(() => {
    const model = editor?.getModel();
    if (!model) return;
    monaco.editor.setModelMarkers(
      model,
      'goal',
      markers.map((m) => {
        const line = Math.min(Math.max(1, m.line), model.getLineCount());
        const end = Math.min(Math.max(line, m.endLine), model.getLineCount());
        return {
          startLineNumber: line,
          startColumn: Math.max(1, m.column),
          endLineNumber: end,
          endColumn: m.endColumn > 0 ? m.endColumn : model.getLineMaxColumn(end),
          severity: severities[m.severity],
          message: m.message,
        };
      }),
    );
  }, [editor, markers]);

  useEffect(() => {
    if (!editor) return;
    const problems = new Set(markers.filter((m) => m.severity !== 'info').map((m) => m.line));
    const added = new Set(changedLines);
    const collection = editor.createDecorationsCollection(
      [...new Set([...problems, ...added])].map((line) => ({
        range: new monaco.Range(line, 1, line, 1),
        options: problems.has(line)
          ? {
              isWholeLine: true,
              className: 'goal-line-problem',
              glyphMarginClassName: 'goal-glyph-problem',
              linesDecorationsClassName: added.has(line) ? 'goal-gutter-added' : null,
            }
          : { isWholeLine: true, className: 'goal-line-added', linesDecorationsClassName: 'goal-gutter-added' },
      })),
    );
    return () => collection.clear();
  }, [editor, markers, changedLines]);

  // Blocks under lines: a DOM node per zone that React renders into; its height follows its content.
  const zoneKey = zones.map((z) => `${z.key}@${z.line}`).join('|');
  // biome-ignore lint/correctness/useExhaustiveDependencies: rebuilt when keys or lines change; content renders through the portal
  useEffect(() => {
    if (!editor) return;
    const made: Zone[] = [];
    editor.changeViewZones((acc) => {
      for (const z of zones) {
        const node = document.createElement('div');
        node.className = 'goal-zone';
        const zone = { afterLineNumber: z.line, heightInPx: 0, domNode: node };
        made.push({ key: z.key, node, zone, id: acc.addZone(zone) });
      }
    });
    setMounted(made);
    return () => {
      editor.changeViewZones((acc) => {
        for (const z of made) acc.removeZone(z.id);
      });
      setMounted([]);
    };
  }, [editor, zoneKey]);

  /** A zone's content measured itself: Monaco reads the new height from the zone it was given. */
  const sized = (z: Zone, height: number) => {
    if (!editor || z.zone.heightInPx === height) return;
    z.zone.heightInPx = height;
    editor.changeViewZones((acc) => acc.layoutZone(z.id));
  };

  useImperativeHandle(
    ref,
    () => ({
      reveal: (line, focus = false) => {
        if (!editor || !host.current) return false;
        const n = Math.min(Math.max(1, line), editor.getModel()?.getLineCount() ?? 1);
        editor.setPosition({ lineNumber: n, column: 1 });
        editor.revealLineInCenter(n);
        host.current.scrollIntoView({ block: 'nearest' });
        if (focus) editor.focus();
        return true;
      },
    }),
    [editor],
  );

  const contentOf = (key: string) => zones.find((z) => z.key === key)?.content;

  return (
    <>
      <div ref={host} className="w-full" style={{ height: minLines * lineHeight }} />
      {mounted.map((z) =>
        createPortal(<ZoneBody onSize={(h) => sized(z, h)}>{contentOf(z.key)}</ZoneBody>, z.node, z.key),
      )}
    </>
  );
}

/** A zone's content, reporting its height (with a gap under it) whenever it changes. */
function ZoneBody({ onSize, children }: { onSize: (height: number) => void; children: ReactNode }) {
  const body = useRef<HTMLDivElement>(null);
  const report = useRef(onSize);
  report.current = onSize;
  useEffect(() => {
    const el = body.current;
    if (!el) return;
    const measure = () => report.current(Math.ceil(el.getBoundingClientRect().height) + 8);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);
  return (
    <div ref={body} className="goal-zone-body">
      {children}
    </div>
  );
}
