// A number that changes in place (MOTION.md, A number changes): only the characters that changed pop in
// from below with a blur, one after another, --duration-stagger apart. The characters that did not change
// stay still. Screen readers read the whole value once, from the sr-only text; the animated characters are
// hidden from them. Under reduced motion the changed characters only fade. Frequency decides: a value that
// changes again within QUIET_MS of its last change swaps in place with no animation, so a figure that updates
// every few seconds never shows two digits mid-roll; an isolated change still rolls.
import { useState } from 'react';
import { cn } from './cn';

/** How long a value must have held for its next change to roll (MOTION.md, A number changes). */
export const QUIET_MS = 8000;

interface NumberRollProps {
  value: number | string;
  className?: string;
}

export function NumberRoll({ value, className }: NumberRollProps) {
  const text = String(value);
  // `before` is the text the last change came from; `generation` counts the changes, so each change
  // replays the animation on the characters it touched. Both are kept in state so the committed render
  // still knows which characters moved.
  const [shown, setShown] = useState({ text, before: text, generation: 0, at: Number.NEGATIVE_INFINITY, roll: false });
  let current = shown;
  if (shown.text !== text) {
    const at = Date.now();
    current = { text, before: shown.text, generation: shown.generation + 1, at, roll: at - shown.at >= QUIET_MS };
    setShown(current);
  }

  // Characters are compared by place from the right: the old text is offset so its last character lines up
  // with the new one's, so "1234" to "234" leaves the three digits still.
  const offset = current.before.length - text.length;
  let stagger = 0;
  const cells = [...text].map((character, i) => {
    // Keyed by the place from the right, so a digit keeps its element while the number grows.
    const place = text.length - i;
    if (!current.roll || current.before[i + offset] === character) {
      return { key: `${place}`, character, delay: undefined };
    }
    const delay = stagger;
    stagger += 1;
    return { key: `${place}-${current.generation}`, character, delay };
  });

  return (
    <span className={cn('inline-flex tabular-nums', className)}>
      <span className="sr-only">{text}</span>
      <span aria-hidden="true" className="inline-flex">
        {cells.map((cell) =>
          cell.delay === undefined ? (
            <span key={cell.key} className="inline-block whitespace-pre">
              {cell.character}
            </span>
          ) : (
            <span
              key={cell.key}
              className="inline-block whitespace-pre motion-safe:animate-number-pop-in motion-reduce:animate-fade-in"
              style={{ animationDelay: `calc(var(--duration-stagger) * ${cell.delay})` }}
            >
              {cell.character}
            </span>
          ),
        )}
      </span>
    </span>
  );
}
