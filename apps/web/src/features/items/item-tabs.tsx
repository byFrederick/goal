// The item's views, in the URL: `?tab=` names the selected one; Timeline is the default and leaves
// the URL clean. Trace is the item's step-by-step record; Teardown is where its time, cost and help went.
import { Link, useNavigate, useParams } from '@tanstack/react-router';
import { type KeyboardEvent, useLayoutEffect, useRef } from 'react';
import { cn } from '../../ui/cn';

export const itemTabs = ['timeline', 'spec', 'changes', 'checks', 'trace', 'teardown'] as const;
export type ItemTab = (typeof itemTabs)[number];

export const isItemTab = (v: unknown): v is ItemTab =>
  typeof v === 'string' && (itemTabs as readonly string[]).includes(v);

const labels: Record<ItemTab, string> = {
  timeline: 'Timeline',
  spec: 'Spec',
  changes: 'Changes',
  checks: 'Checks',
  trace: 'Trace',
  teardown: 'Teardown',
};

export const tabId = (t: ItemTab) => `item-tab-${t}`;
/** The id of the panel the selected tab controls. */
export const panelId = 'item-tabpanel';

interface Props {
  tab: ItemTab;
  /** Distinct files the edits touched; shown beside Changes when there are any. */
  files: number;
  /** The checks the item ran; shown beside Checks when there are any. */
  checks: number;
  className?: string;
}

const tabClass = (active: boolean) =>
  cn(
    'inline-flex h-11 shrink-0 items-center gap-2 px-3 text-body no-underline transition-colors duration-(--duration-quick)',
    active ? 'font-medium text-ink' : 'text-muted hover:text-ink',
  );

export function ItemTabs({ tab, files, checks, className }: Props) {
  const params = useParams({ from: '/o/$org/f/$factory/items/$ref' });
  const counts: Partial<Record<ItemTab, string | undefined>> = {
    changes: files > 0 ? `${files} ${files === 1 ? 'file' : 'files'}` : undefined,
    checks: checks > 0 ? String(checks) : undefined,
  };

  // The underline is measured from the selected tab: placed at once on the first paint and on a resize, and it
  // slides when the tab changes (MOTION.md, Tabs).
  const navigate = useNavigate();
  // A keyboard switch moves the underline at once: no animation on keyboard-driven actions (MOTION.md).
  const byKeyboard = useRef(false);
  const underline = useRef<HTMLSpanElement>(null);
  const links = useRef(new Map<ItemTab, HTMLElement>());
  const placed = useRef(false);
  useLayoutEffect(() => {
    const place = (animate: boolean) => {
      const el = underline.current;
      const chosen = links.current.get(tab);
      if (!el || !chosen) return;
      if (!animate) el.style.transition = 'none';
      el.style.width = `${chosen.offsetWidth}px`;
      el.style.transform = `translateX(${chosen.offsetLeft}px)`;
      if (!animate) {
        void el.offsetWidth;
        el.style.transition = '';
      }
    };
    place(placed.current && !byKeyboard.current);
    // On a phone the row scrolls sideways: keep the selected tab in view.
    links.current.get(tab)?.scrollIntoView?.({ block: 'nearest', inline: 'nearest' });
    byKeyboard.current = false;
    placed.current = true;
    const onResize = () => place(false);
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, [tab]);

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const at = itemTabs.indexOf(tab);
    const next =
      e.key === 'ArrowRight'
        ? itemTabs[(at + 1) % itemTabs.length]
        : e.key === 'ArrowLeft'
          ? itemTabs[(at - 1 + itemTabs.length) % itemTabs.length]
          : e.key === 'Home'
            ? itemTabs[0]
            : e.key === 'End'
              ? itemTabs[itemTabs.length - 1]
              : undefined;
    if (!next) return;
    e.preventDefault();
    byKeyboard.current = true;
    links.current.get(next)?.focus();
    void navigate({
      to: '/o/$org/f/$factory/items/$ref',
      params,
      search: next === 'timeline' ? {} : { tab: next },
    });
  };

  return (
    <div
      role="tablist"
      aria-label="Item views"
      onKeyDown={onKeyDown}
      className={cn('relative flex gap-1 overflow-x-auto border-b border-line', className)}
    >
      <span
        ref={underline}
        aria-hidden="true"
        className="pointer-events-none absolute bottom-0 left-0 h-0.5 bg-ink transition-[transform,width] duration-(--duration-fast) ease-out motion-reduce:transition-none"
      />
      {itemTabs.map((t) => {
        const active = t === tab;
        return (
          <Link
            key={t}
            id={tabId(t)}
            role="tab"
            aria-selected={active}
            aria-controls={active ? panelId : undefined}
            tabIndex={active ? 0 : -1}
            ref={(el) => {
              if (el) links.current.set(t, el);
            }}
            to="/o/$org/f/$factory/items/$ref"
            params={params}
            search={t === 'timeline' ? {} : { tab: t }}
            activeOptions={{ includeSearch: true, exact: true }}
            className={tabClass(active)}
          >
            {labels[t]}
            {counts[t] ? <span className="text-secondary text-muted">{counts[t]}</span> : null}
          </Link>
        );
      })}
    </div>
  );
}
