// The ⓘ beside a heading or a label: what the thing is, in a tooltip, so a working screen carries no
// explanatory paragraph (MASTER.md). The button's name says what it is about; the text is also its
// description, so a screen reader reads it on focus. The tip opens on hover and focus (ui/tooltip.tsx).
import { Info } from 'lucide-react';
import { useId } from 'react';
import { cn } from './cn';
import { Tooltip } from './tooltip';

export function InfoTip({
  about,
  children,
  side = 'top',
  className,
}: {
  /** What the tip is about: "Coming up" gives the button the name "About Coming up". */
  about: string;
  children: string;
  side?: 'right' | 'top' | 'bottom' | 'left';
  className?: string;
}) {
  const id = useId();
  return (
    <>
      <Tooltip side={side} label={children} wrap>
        <button
          type="button"
          aria-label={`About ${about}`}
          aria-describedby={id}
          className={cn(
            'press inline-flex size-6 flex-none items-center justify-center rounded-chip text-muted transition-colors duration-(--duration-quick) hover:text-ink motion-reduce:transition-none',
            className,
          )}
        >
          <Info size={14} strokeWidth={1.75} aria-hidden="true" />
        </button>
      </Tooltip>
      <span id={id} className="sr-only">
        {children}
      </span>
    </>
  );
}
