// Navigation pieces both the shell and the screens' own headers use.
import { useQueries, useSuspenseQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { BookOpen, Bot, ChevronDown, Code, type LucideIcon, SearchCheck, Wrench } from 'lucide-react';
import { useState } from 'react';
import { boardQuery, factoriesQuery } from '../api/queries';
import type { Factory } from '../api/types';
import { cn } from '../ui/cn';
import { Dialog, DialogContent, DialogTrigger } from '../ui/dialog';
import { factoryTitle } from './factory';
import { useStreamStatus } from './stream';

export const iconProps = { size: 18, strokeWidth: 1.5, 'aria-hidden': true } as const;

/** Screens of later phases: listed so the shape of the product is visible, not built yet. */
export const laterScreens: { label: string; icon: LucideIcon }[] = [
  { label: 'Agents', icon: Bot },
  { label: 'Knowledge', icon: BookOpen },
  { label: 'Jonah', icon: SearchCheck },
  { label: 'Definition', icon: Code },
  { label: 'Remediation', icon: Wrench },
];

export const navItem =
  'flex h-10 items-center gap-3 rounded-control px-3 text-[15px] text-ink no-underline transition-colors duration-200 md:max-lg:justify-center md:max-lg:px-0';

/** Labels the 768 to 1023 px icon rail hides visually (screen readers still read them). */
export const railLabel = 'md:max-lg:sr-only';

export function LaterLinks() {
  return laterScreens.map(({ label, icon: Icon }) => (
    <span
      key={label}
      aria-disabled="true"
      title="Coming in a later phase"
      className={cn(navItem, 'cursor-default text-muted')}
    >
      <Icon {...iconProps} />
      <span className={railLabel}>{label}</span>
      <span className="ml-auto text-xs md:max-lg:hidden">Later</span>
    </span>
  ));
}

export function FactoryLinks({ current, onNavigate }: { current: string; onNavigate?: () => void }) {
  const { data: factories } = useSuspenseQuery(factoriesQuery);
  const boards = useQueries({ queries: factories.map((f) => boardQuery(f.name)) });

  return (
    <ul className="m-0 flex list-none flex-col gap-1 p-0">
      {factories.map((f, i) => {
        const items = boards[i]?.data?.stations.flatMap((s) => (s.kind === 'done' ? [] : s.items));
        const waiting = items?.filter((it) => it.state === 'waiting_human').length ?? 0;
        return (
          <li key={f.id}>
            <Link
              to="/f/$factory"
              params={{ factory: f.name }}
              onClick={onNavigate}
              aria-current={f.name === current ? 'true' : undefined}
              className={cn(navItem, 'hover:bg-nav-hover', f.name === current && 'font-medium')}
            >
              <span aria-hidden="true" className="flex w-[18px] justify-center">
                <span className={cn('size-2 rounded-full', waiting > 0 ? 'bg-you' : 'bg-ok-dot')} />
              </span>
              <span className={railLabel}>{factoryTitle(f)}</span>
              {items ? (
                <span className="ml-auto text-[13px] font-normal text-muted md:max-lg:hidden">
                  {waiting > 0 ? `${waiting} need you` : `${items.length} open`}
                </span>
              ) : null}
            </Link>
          </li>
        );
      })}
    </ul>
  );
}

/** Whether the live stream is connected. `compact` shows only the dot (the text stays for readers). */
export function LiveStatus({
  compact,
  suffix,
  className,
  textClassName,
}: {
  compact?: boolean;
  suffix?: string;
  className?: string;
  textClassName?: string;
}) {
  const status = useStreamStatus();
  const text = { connecting: 'Connecting…', live: 'Live', reconnecting: 'Reconnecting…' }[status];
  return (
    <p role="status" className={cn('m-0 flex items-center gap-2 text-[13px] text-muted', className)}>
      <span
        aria-hidden="true"
        className={cn(
          'size-2 rounded-full',
          status === 'live' ? 'bg-ok-dot' : status === 'reconnecting' ? 'bg-rework' : 'bg-faint',
        )}
      />
      <span className={cn(compact && 'sr-only', textClassName)}>
        {text}
        {suffix}
      </span>
    </p>
  );
}

/** The phone header's factory button: opens the list of factories. */
export function FactorySwitcher({ factory }: { factory: Factory }) {
  const [open, setOpen] = useState(false);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <button
          type="button"
          className="inline-flex h-11 items-center gap-2 rounded-control border border-line bg-surface px-3 text-[15px] text-ink"
          aria-label={`Factory: ${factoryTitle(factory)}. Switch factory`}
        >
          <span aria-hidden="true" className="size-2 rounded-full bg-ok-dot" />
          {factoryTitle(factory)}
          <ChevronDown size={14} strokeWidth={1.5} aria-hidden="true" />
        </button>
      </DialogTrigger>
      <DialogContent title="Factories" variant="sheet">
        <FactoryLinks current={factory.name} onNavigate={() => setOpen(false)} />
      </DialogContent>
    </Dialog>
  );
}
