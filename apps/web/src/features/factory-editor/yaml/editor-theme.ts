// The Monaco theme from MASTER's tokens, as a pure function so a test can check its contrast.
// Monaco takes colour values, not CSS variables: the caller reads each token as the page has it now.
import type * as monaco from 'monaco-editor';

/** A token's value as '#rrggbb', for the name without the `--color-` prefix. */
export type TokenReader = (name: string) => string;

/** Monaco accepts only 6 or 8 hex digits, but the CSS build shortens white to three: expand 3 and 4 digits. */
function full(value: string): string {
  const short = /^#([0-9a-f])([0-9a-f])([0-9a-f])([0-9a-f])?$/i.exec(value);
  if (!short) return value;
  return `#${short
    .slice(1)
    .filter(Boolean)
    .map((c) => c + c)
    .join('')}`;
}

/** Monaco's token rules take the hex digits without the '#'. */
const digits = (value: string) => full(value).replace('#', '');

/** The syntax tokens and what they take: every one is checked at 4.5:1 against the editor background. */
export const syntaxTokens = {
  keyword: 'run-ink',
  type: 'run-ink',
  tag: 'run-ink',
  string: 'ok-ink',
  number: 'amber-ink',
  comment: 'muted',
  variable: 'you-ink',
  operator: 'ink-soft',
  delimiter: 'ink-soft',
} as const;

export function editorTheme(rawRead: TokenReader, dark: boolean): monaco.editor.IStandaloneThemeData {
  const read: TokenReader = (name) => full(rawRead(name));
  return {
    // The base fills every colour this theme does not name: it must match the page's theme.
    base: dark ? 'vs-dark' : 'vs',
    inherit: true,
    rules: Object.entries(syntaxTokens).map(([token, name]) => ({
      token,
      foreground: digits(read(name)),
      ...(token === 'comment' ? { fontStyle: 'italic' } : {}),
    })),
    colors: {
      'editor.background': read('shell'),
      'editor.foreground': read('ink'),
      'editorLineNumber.foreground': read('muted'),
      'editorLineNumber.activeForeground': read('ink'),
      'editorCursor.foreground': read('run'),
      'editor.selectionBackground': read('run-tint'),
      'editor.lineHighlightBackground': read('panel'),
      'editorIndentGuide.background1': read('line'),
      'editorError.foreground': read('rework'),
      'editorWarning.foreground': read('rework'),
      'editorBracketMatch.background': read('run-tint'),
      'editorBracketMatch.border': read('run'),
      'editorWidget.background': read('card'),
      'editorWidget.foreground': read('ink'),
      'editorWidget.border': read('rule'),
      'editorSuggestWidget.background': read('card'),
      'editorSuggestWidget.foreground': read('ink'),
      'editorSuggestWidget.border': read('rule'),
      'editorSuggestWidget.selectedBackground': read('hover'),
      'editorSuggestWidget.highlightForeground': read('run-ink'),
      'editorHoverWidget.background': read('card'),
      'editorHoverWidget.foreground': read('ink'),
      'editorHoverWidget.border': read('rule'),
    },
  };
}
