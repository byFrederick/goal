// A source's square (docs/design/screens/connections/knowledge.html, intake/intake-new.html): the near-black
// mark with a 1px inset white rule at 12% so it holds on dark, and a white icon. Blue sources pass `fill`.
import type { LucideIcon } from 'lucide-react';
import { cn } from './cn';

const sizes = {
  sm: { square: 'size-6', icon: 14 },
  md: { square: 'size-8.5', icon: 16 },
  lg: { square: 'size-9', icon: 18 },
} as const;

interface SourceTileProps {
  icon: LucideIcon;
  /** The 24, 34 or 36 px square. */
  size: keyof typeof sizes;
  /** A fill token class instead of the mark, for a blue source. */
  fill?: string | undefined;
}

export function SourceTile({ icon: Icon, size, fill }: SourceTileProps) {
  const { square, icon } = sizes[size];
  return (
    <span
      aria-hidden="true"
      className={cn(
        'grid flex-none place-items-center rounded-cell text-white shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--color-white)_12%,transparent)]',
        fill ?? 'bg-mark',
        square,
      )}
    >
      <Icon size={icon} strokeWidth={1.5} />
    </span>
  );
}
