// A number that changes in place (MOTION.md, A number changes): only the characters that changed pop in
// from below with a blur, one after another, --duration-stagger apart. The characters that did not change
// stay still. Screen readers read the whole value once, from the sr-only text; the animated characters are
// hidden from them. Under reduced motion the changed characters only fade.
import { useState } from 'react';
import { cn } from './cn';

interface NumberRollProps {
  value: number | string;
  className?: string;
}

export function NumberRoll({ value, className }: NumberRollProps) {
  const text = String(value);
  // `before` is the text the last change came from; `generation` counts the changes, so each change
  // replays the animation on the characters it touched. Both are kept in state so the committed render
  // still knows which characters moved.
  const [shown, setShown] = useState({ text, before: text, generation: 0 });
  let current = shown;
  if (shown.text !== text) {
    current = { text, before: shown.text, generation: shown.generation + 1 };
    setShown(current);
  }

  const previous = current.before.padStart(text.length);
  let stagger = 0;
  const cells = [...text].map((character, i) => {
    // Keyed by the place from the right, so a digit keeps its element while the number grows.
    const place = text.length - i;
    if (current.generation === 0 || previous[i] === character) {
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
            <span key={cell.key} className="inline-block">
              {cell.character}
            </span>
          ) : (
            <span
              key={cell.key}
              className="inline-block motion-safe:animate-number-pop-in motion-reduce:animate-fade-in"
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
