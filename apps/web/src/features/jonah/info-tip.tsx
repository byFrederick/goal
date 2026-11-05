// The ⓘ beside a heading: how something works sits behind it, never in a paragraph (MOTION.md, Tooltip).
// A button so it takes focus and shows the tip on keyboard focus too; it takes a 44 px box on phones, in the row's flow, so it never lies over the control below.
import { Info } from 'lucide-react';
import { useState } from 'react';
import { Tooltip } from '../../ui/tooltip';

export function InfoTip({ label, children }: { label: string; children: string }) {
  // A tap opens the tip too: a finger has no hover.
  const [open, setOpen] = useState(false);
  return (
    <Tooltip label={children} side="bottom" open={open} onOpenChange={setOpen}>
      <button
        type="button"
        aria-label={label}
        onClick={(e) => {
          e.preventDefault();
          setOpen(true);
        }}
        className="press inline-grid size-6 shrink-0 place-items-center rounded-chip text-muted hover:text-ink max-md:size-11"
      >
        <Info size={14} strokeWidth={1.75} aria-hidden="true" />
      </button>
    </Tooltip>
  );
}
