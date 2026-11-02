// Template variables as the picture draws them: {{check.name}} is an orange monospace token in Title,
// Brief and the YAML. This only renders text; it never changes the text that is saved.
import { Fragment, type ReactNode } from 'react';

const variable = /(\{\{[^}]*\}\})/;

const token = 'rounded-chip bg-you-tint px-1 font-mono text-you-ink';

/** The text with each {{field}} as a token. */
export function TemplateText({ text }: { text: string }): ReactNode {
  return text.split(variable).map((part, i) =>
    variable.test(part) ? (
      // biome-ignore lint/suspicious/noArrayIndexKey: parts are positional and never reordered
      <span key={i} className={token}>
        {part.slice(2, -2).trim()}
      </span>
    ) : (
      // biome-ignore lint/suspicious/noArrayIndexKey: parts are positional and never reordered
      <Fragment key={i}>{part}</Fragment>
    ),
  );
}

/** True when the text holds a {{field}}. */
export const hasVariable = (text: string) => variable.test(text);

// A key at the start of a line, a variable, or a whole number after a colon or comma.
const yamlToken = /(\{\{[^}]*\}\})|^(\s*(?:- )?[A-Za-z][\w]*:)|((?<=[:,]\s)\d+(?=\s*[,}\n]|$))/gm;

/** The trigger's YAML with keys muted, numbers in the primary colour and variables as tokens. */
export function HighlightedYaml({ text }: { text: string }): ReactNode {
  const out: ReactNode[] = [];
  let last = 0;
  for (const m of text.matchAll(yamlToken)) {
    const at = m.index ?? 0;
    if (at > last) out.push(text.slice(last, at));
    if (m[1])
      out.push(
        <span key={at} className={token}>
          {m[1]}
        </span>,
      );
    else if (m[2])
      out.push(
        <span key={at} className="text-muted">
          {m[2]}
        </span>,
      );
    else
      out.push(
        <span key={at} className="text-run">
          {m[3]}
        </span>,
      );
    last = at + m[0].length;
  }
  out.push(text.slice(last));
  return out;
}
