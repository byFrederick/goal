// The pieces both sidebars are built from (C §10.5): a 232 px column pinned beside the rail, a
// header (eyebrow, name, one line under it), groups of 40 px entries, and the "Later" entry of a
// screen a later phase builds.
import type { LucideIcon } from 'lucide-react';
import { type ReactNode, useId } from 'react';
import { cn } from '../ui/cn';
import { laterText } from './screens';

export const iconProps = { size: 18, strokeWidth: 1.5, 'aria-hidden': true } as const;

/** A sidebar entry; the current one (aria-current) is ink. `group` lets a Count react to it. */
export const entryClass =
  'group flex h-10 items-center gap-3 rounded-control px-3 text-[15px] text-ink no-underline transition-colors duration-200 hover:bg-nav-hover aria-[current=page]:bg-ink aria-[current=page]:text-white';

export function Sidebar({
  label,
  eyebrow,
  title,
  subtitle,
  children,
}: {
  /** The nav landmark's name: "sre factory", "Acme organization". */
  label: string;
  eyebrow: ReactNode;
  title: ReactNode;
  subtitle?: ReactNode;
  children: ReactNode;
}) {
  return (
    <nav
      aria-label={label}
      className="sticky top-0 hidden h-dvh w-[232px] shrink-0 flex-col gap-0.5 overflow-y-auto border-r border-rule bg-shell px-3 pt-[22px] pb-4 md:flex"
    >
      <div className="flex flex-col gap-0.5 px-3 pb-1">
        <span className="text-xs font-medium text-muted">{eyebrow}</span>
        <span className="text-xl leading-tight font-medium tracking-[-0.02em] break-words">{title}</span>
        {subtitle ? <span className="text-[13px] leading-snug text-muted">{subtitle}</span> : null}
      </div>
      {children}
    </nav>
  );
}

/** A labelled group of entries ("Work", "Build", "Learn"). */
export function SidebarGroup({ label, children }: { label?: string; children: ReactNode }) {
  const id = useId();
  return (
    <>
      {label ? (
        <p id={id} className="m-0 mt-[18px] mb-1 ml-3 text-[13px] font-medium text-muted">
          {label}
        </p>
      ) : null}
      <ul
        aria-labelledby={label ? id : undefined}
        className={cn('m-0 flex list-none flex-col gap-0.5 p-0', !label && 'mt-[18px]')}
      >
        {children}
      </ul>
    </>
  );
}

/** A screen a later phase builds: listed so the product's shape shows, disabled, marked "Later". */
export function LaterEntry({ label, icon: Icon }: { label: string; icon: LucideIcon }) {
  return (
    <li>
      {/* A long label wraps rather than truncates beside "Later". */}
      <span
        aria-disabled="true"
        className={cn(
          entryClass,
          'h-auto min-h-10 cursor-default py-1.5 leading-tight text-muted hover:bg-transparent',
        )}
      >
        <Icon {...iconProps} />
        <span className="min-w-0">{label}</span>
        <span className="ml-auto shrink-0 text-xs">{laterText}</span>
      </span>
    </li>
  );
}
