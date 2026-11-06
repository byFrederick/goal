// The ⓘ beside a heading or a label: what the thing is, in a tooltip, so a working screen carries no
// explanatory paragraph (MASTER.md). The button's name says what it is about; the text is also its
// description, so a screen reader reads it on focus. The tip opens on hover, focus and a tap (ui/tooltip.tsx); on a phone the hit area is 44 px.
import { Info } from 'lucide-react';
import { useId, useState } from 'react';
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
  // A tap opens the tip too (a finger has no hover); Escape or a tap elsewhere closes it.
  const [open, setOpen] = useState(false);
  return (
    <>
      <Tooltip side={side} label={children} wrap open={open} onOpenChange={setOpen}>
        <button
          type="button"
          aria-label={`About ${about}`}
          aria-describedby={id}
          onClick={(e) => {
            // Keeps Radix from closing the tip the hover just opened.
            e.preventDefault();
            setOpen(true);
          }}
          className={cn(
            'press relative inline-flex size-6 flex-none items-center justify-center rounded-chip text-muted before:absolute before:-inset-1 before:content-[""] max-md:before:-inset-2.5 transition-colors duration-(--duration-quick) hover:text-ink motion-reduce:transition-none',
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
