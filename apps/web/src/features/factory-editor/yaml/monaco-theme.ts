// The YAML editor's theme as a pure function of the resolved theme and a token reader (no Monaco at
// runtime, so its test loads fast). The base follows the theme so any token without a Goal rule
// falls back to Monaco's dark colours on a dark page, not its light ones.
import type { editor } from 'monaco-editor/editor/editor.api';

/** Reads a design token's value by name, for the theme in force. */
export type TokenReader = (name: string) => string;

export function goalTheme(theme: 'light' | 'dark', token: TokenReader): editor.IStandaloneThemeData {
  /** Monaco's token rules take the hex digits without the '#'. */
  const hex = (name: string) => token(name).replace('#', '');
  return {
    base: theme === 'dark' ? 'vs-dark' : 'vs',
    inherit: true,
    rules: [
      { token: 'keyword', foreground: hex('run-ink') },
      { token: 'type', foreground: hex('run-ink') },
      { token: 'string', foreground: hex('ok-ink') },
      { token: 'number', foreground: hex('amber-ink') },
      { token: 'comment', foreground: hex('muted'), fontStyle: 'italic' },
      { token: 'variable', foreground: hex('you-ink') },
    ],
    colors: {
      'editor.background': token('shell'),
      'editor.foreground': token('ink'),
      'editorLineNumber.foreground': token('muted'),
      'editorLineNumber.activeForeground': token('ink'),
      'editorCursor.foreground': token('run'),
      'editor.selectionBackground': token('run-tint'),
      'editor.lineHighlightBackground': token('panel'),
      'editorIndentGuide.background1': token('line'),
      'editorError.foreground': token('rework'),
      'editorWarning.foreground': token('rework'),
    },
  };
}
