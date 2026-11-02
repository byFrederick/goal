// A search box: a magnifier, a visible placeholder and an sr-only label that say the same words. The
// caller owns the value and the width (the top bar and a card header size it differently). Typing does not
// animate (MOTION.md: no animation on high-frequency input); only the border colour eases on hover.
import { Search } from 'lucide-react';
import { useId } from 'react';
import { cn } from './cn';

export function SearchField({
  value,
  onChange,
  label,
  className,
}: {
  value: string;
  onChange: (value: string) => void;
  /** The placeholder and the accessible name, e.g. "Search members". */
  label: string;
  className?: string;
}) {
  const id = useId();
  return (
    <div className={cn('relative w-full', className)}>
      <label htmlFor={id} className="sr-only">
        {label}
      </label>
      <Search
        size={16}
        strokeWidth={1.5}
        aria-hidden="true"
        className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-muted"
      />
      <input
        id={id}
        type="search"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={label}
        autoComplete="off"
        className="h-11 w-full rounded-control border border-rule bg-card pr-3 pl-9 text-body text-ink transition-colors duration-200 hover:border-muted md:h-8"
      />
    </div>
  );
}
