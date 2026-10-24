// The editor's code editor (D §10.9, review decision 6): Monaco (ADR 0001), loaded lazily in its
// own chunk outside the initial budget, with a read-only copy with line numbers until it loads;
// on phones (under 768 px with a coarse pointer) an auto-growing textarea with a line-number
// gutter instead, because Monaco is not made for touch. The text is controlled: `value` is what
// the draft session holds, `onChange` gets every keystroke. For the YAML view (D10) it also takes
// the file's path (factory.yaml gets monaco-yaml's schema), B's problems as markers, the lines
// changed since the base (a "+" gutter), blocks under lines (a problem with its fixes, Monaco
// only), the cursor's moves, and `reveal` through its ref. If Monaco cannot load, the textarea
// takes its place.
import { TriangleAlert } from 'lucide-react';
import {
  Component,
  type ReactNode,
  type Ref,
  Suspense,
  useImperativeHandle,
  useMemo,
  useRef,
  useSyncExternalStore,
} from 'react';
import { lazyRetrying } from '../../../shared/lazy';
import { cn } from '../../../ui/cn';

export interface EditorMarker {
  line: number;
  column: number;
  endLine: number;
  endColumn: number;
  severity: 'error' | 'warning' | 'info';
  message: string;
}

export interface EditorZone {
  key: string;
  /** The block goes under this line. */
  line: number;
  content: ReactNode;
}

export interface CursorPosition {
  line: number;
  column: number;
}

export interface CodeEditorHandle {
  /** Moves the cursor to the start of a line and scrolls it into view; false before the editor is ready. */
  reveal: (line: number, focus?: boolean) => boolean;
}

export interface CodeEditorProps {
  value: string;
  onChange: (text: string) => void;
  /** Leaving the editor: the session saves at once. */
  onBlur?: () => void;
  language: 'markdown' | 'yaml' | 'plaintext';
  /** The accessible name ("Prompt of reviewer", "factory.yaml, draft of v9"). */
  label: string;
  readOnly?: boolean;
  /** Lines shown before the editor scrolls (it grows to its content up to this). */
  maxLines?: number;
  /** Lines it shows at least. */
  minLines?: number;
  /** The file's path in the definition: Monaco's model is named by it, and factory.yaml gets the schema. */
  path?: string;
  markers?: readonly EditorMarker[];
  changedLines?: readonly number[];
  zones?: readonly EditorZone[];
  onCursor?: (position: CursorPosition) => void;
  ref?: Ref<CodeEditorHandle>;
}

const MonacoEditor = lazyRetrying(() => import('./monaco-editor').then((m) => m.default));

const phoneQuery = '(max-width: 767px) and (pointer: coarse)';

function subscribePhone(change: () => void) {
  const query = window.matchMedia(phoneQuery);
  query.addEventListener('change', change);
  return () => query.removeEventListener('change', change);
}

/** A phone: the textarea instead of Monaco. */
export function usePhoneEditor(): boolean {
  return useSyncExternalStore(
    subscribePhone,
    () => window.matchMedia(phoneQuery).matches,
    () => false,
  );
}

export function CodeEditor(props: CodeEditorProps) {
  const phone = usePhoneEditor();
  if (phone) return <TextareaEditor {...props} />;
  return (
    <div className="flex flex-col">
      <FallBack to={<TextareaEditor {...props} />}>
        <Suspense fallback={<LinedText text={props.value} minLines={props.minLines} />}>
          <MonacoEditor {...props} />
        </Suspense>
      </FallBack>
      {props.readOnly ? null : (
        <p className="m-0 border-t border-line bg-card px-3 py-1.5 text-xs text-muted">
          <kbd className="font-sans">Ctrl+M</kbd>: Tab moves focus out of the editor
        </p>
      )}
    </div>
  );
}

/** When Monaco cannot load (its chunk is unreachable, it throws), the textarea takes its place. */
class FallBack extends Component<{ to: ReactNode; children: ReactNode }, { failed: boolean }> {
  override state = { failed: false };

  static getDerivedStateFromError(): { failed: boolean } {
    return { failed: true };
  }

  override render() {
    return this.state.failed ? this.props.to : this.props.children;
  }
}

/** One number per line of the text. */
const lineNumbers = (text: string, min = 1) =>
  Array.from({ length: Math.max(min, text.split('\n').length) }, (_, i) => i + 1);

const codeText = 'font-mono text-[13px] leading-[1.75]';
/** 16 px on phones, so iOS does not zoom into the field; the gutter matches it line for line. */
const phoneText = 'font-mono text-base leading-[1.75]';

/** The text with line numbers, read-only: what shows while Monaco loads. */
export function LinedText({ text, minLines }: { text: string; minLines?: number | undefined }) {
  const lines = text.split('\n');
  return (
    <div aria-busy="true" className={cn('grid grid-cols-[40px_minmax(0,1fr)] py-2.5', codeText)}>
      <span className="sr-only">Loading the editor</span>
      {lineNumbers(text, minLines).map((n) => (
        <div key={n} className="contents">
          <span aria-hidden="true" className="pr-3 text-right text-[#5f6b67] select-none">
            {n}
          </span>
          <span className="pr-3 whitespace-pre-wrap [overflow-wrap:anywhere]">{lines[n - 1] ?? ''}</span>
        </div>
      ))}
    </div>
  );
}

/** The 1-based line and column of an offset in a text. */
function positionAt(text: string, offset: number): CursorPosition {
  const before = text.slice(0, offset);
  return { line: before.split('\n').length, column: offset - before.lastIndexOf('\n') };
}

/** The offset of the start of a 1-based line. */
function offsetOf(text: string, line: number): number {
  let offset = 0;
  for (let i = 1; i < line; i++) {
    const next = text.indexOf('\n', offset);
    if (next < 0) return text.length;
    offset = next + 1;
  }
  return offset;
}

function TextareaEditor({
  value,
  onChange,
  onBlur,
  label,
  readOnly,
  minLines = 8,
  markers = [],
  changedLines = [],
  onCursor,
  ref,
}: CodeEditorProps) {
  const box = useRef<HTMLDivElement>(null);
  const area = useRef<HTMLTextAreaElement>(null);
  const changed = useMemo(() => new Set(changedLines), [changedLines]);
  const problems = useMemo(() => new Set(markers.filter((m) => m.severity !== 'info').map((m) => m.line)), [markers]);
  const cursor = useRef(onCursor);
  cursor.current = onCursor;

  useImperativeHandle(
    ref,
    () => ({
      reveal: (line, focus = false) => {
        const el = area.current;
        const scroller = box.current;
        if (!el || !scroller) return false;
        const at = offsetOf(el.value, line);
        if (focus) el.focus({ preventScroll: true });
        el.setSelectionRange(at, at);
        cursor.current?.(positionAt(el.value, at));
        const lineHeight = Number.parseFloat(getComputedStyle(el).lineHeight) || 28;
        scroller.scrollTop = Math.max(0, (line - 1) * lineHeight - scroller.clientHeight / 2);
        scroller.scrollIntoView({ block: 'nearest' });
        return true;
      },
    }),
    [],
  );

  const report = () => {
    const el = area.current;
    if (el && onCursor) onCursor(positionAt(el.value, el.selectionStart));
  };

  return (
    <div ref={box} className="flex max-h-[70dvh] overflow-auto overscroll-contain py-2.5">
      <div aria-hidden="true" className={cn('w-12 shrink-0 pr-2 text-right text-[#5f6b67] select-none', phoneText)}>
        {lineNumbers(value, minLines).map((n) => (
          <div key={n} className="flex items-center justify-end gap-0.5">
            {problems.has(n) ? (
              <TriangleAlert size={12} strokeWidth={2} className="text-rework" />
            ) : changed.has(n) ? (
              <span className="text-ok-ink">+</span>
            ) : null}
            <span className={cn(problems.has(n) && 'text-rework-ink')}>{n}</span>
          </div>
        ))}
      </div>
      <textarea
        ref={area}
        aria-label={label}
        value={value}
        readOnly={readOnly}
        onChange={(e) => onChange(e.target.value)}
        onSelect={report}
        onKeyUp={report}
        onBlur={onBlur}
        wrap="off"
        spellCheck={false}
        autoCapitalize="off"
        autoComplete="off"
        className={cn(
          'field-sizing-content min-w-0 flex-1 self-start resize-none overflow-hidden border-0 bg-transparent p-0 pr-3 text-ink outline-none',
          phoneText,
        )}
        style={{ minHeight: `${minLines * 1.75}em` }}
      />
    </div>
  );
}
