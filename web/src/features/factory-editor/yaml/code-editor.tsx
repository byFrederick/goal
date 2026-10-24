// The editor's code editor (D §10.9, review decision 6): Monaco (ADR 0001), loaded lazily in its
// own chunk outside the initial budget, with a read-only copy with line numbers until it loads;
// on phones (under 768 px with a coarse pointer) an auto-growing textarea with a line-number
// gutter instead, because Monaco is not made for touch. The text is controlled: `value` is what
// the draft session holds, `onChange` gets every keystroke.
import { lazy, Suspense, useSyncExternalStore } from 'react';
import { cn } from '../../../ui/cn';

export interface CodeEditorProps {
  value: string;
  onChange: (text: string) => void;
  /** Leaving the editor: the session saves at once. */
  onBlur?: () => void;
  language: 'markdown' | 'yaml';
  /** The accessible name ("Prompt of reviewer", "factory.yaml, draft of v9"). */
  label: string;
  readOnly?: boolean;
  /** Lines shown before the editor scrolls (it grows to its content up to this). */
  maxLines?: number;
  /** Lines it shows at least. */
  minLines?: number;
}

const MonacoEditor = lazy(() => import('./monaco-editor'));

const phoneQuery = '(max-width: 767px) and (pointer: coarse)';

function subscribePhone(change: () => void) {
  const query = window.matchMedia(phoneQuery);
  query.addEventListener('change', change);
  return () => query.removeEventListener('change', change);
}

/** A phone: the textarea instead of Monaco. */
function usePhoneEditor(): boolean {
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
      <Suspense fallback={<LinedText text={props.value} minLines={props.minLines} />}>
        <MonacoEditor {...props} />
      </Suspense>
      {props.readOnly ? null : (
        <p className="m-0 border-t border-line bg-card px-3 py-1.5 text-xs text-muted">
          <kbd className="font-sans">Ctrl+M</kbd>: Tab moves focus out of the editor
        </p>
      )}
    </div>
  );
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

function TextareaEditor({ value, onChange, onBlur, label, readOnly, minLines = 8 }: CodeEditorProps) {
  return (
    <div className="flex max-h-[70dvh] overflow-auto overscroll-contain py-2.5">
      <div aria-hidden="true" className={cn('w-10 shrink-0 pr-2 text-right text-[#5f6b67] select-none', phoneText)}>
        {lineNumbers(value, minLines).map((n) => (
          <div key={n}>{n}</div>
        ))}
      </div>
      <textarea
        aria-label={label}
        value={value}
        readOnly={readOnly}
        onChange={(e) => onChange(e.target.value)}
        onBlur={onBlur}
        wrap="off"
        spellCheck={false}
        autoCapitalize="off"
        autoComplete="off"
        className={cn(
          'field-sizing-content min-w-0 flex-1 resize-none overflow-hidden border-0 bg-transparent p-0 pr-3 text-ink outline-none',
          phoneText,
        )}
        style={{ minHeight: `${minLines * 1.75}em` }}
      />
    </div>
  );
}
