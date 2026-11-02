import { useState } from 'react';
import { cn } from './cn';

/**
 * An agent's status line. While the agent runs it shimmers (the `thinking` utility); when the text
 * changes in place, the new state is drawn again with the swap animation (MOTION.md, An agent
 * working). The first draw does not animate. Both are still under prefers-reduced-motion.
 */
export function WorkingLine({
  text,
  running,
  className,
}: {
  text: string;
  /** The agent is running now; a line for anything else does not shimmer. */
  running: boolean;
  className?: string;
}) {
  const [seen, setSeen] = useState({ text, generation: 0 });
  let { generation } = seen;
  if (seen.text !== text) {
    generation += 1;
    setSeen({ text, generation });
  }
  return (
    <span
      key={generation}
      className={cn(
        running && 'thinking',
        generation > 0 && 'motion-safe:animate-text-states-swap motion-reduce:animate-fade-in',
        className,
      )}
    >
      {text}
    </span>
  );
}
