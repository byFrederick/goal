// The item's views, in the URL: `?tab=` names the selected one; Timeline is the default and leaves
// the URL clean. Trace and Teardown are not built yet: they stay in the bar, disabled.
import { Link, useParams } from '@tanstack/react-router';
import { Button } from '../../ui/button';
import { cn } from '../../ui/cn';
import { NotAvailable } from '../../ui/not-available';

export const itemTabs = ['timeline', 'spec', 'changes', 'checks'] as const;
export type ItemTab = (typeof itemTabs)[number];

export const isItemTab = (v: unknown): v is ItemTab =>
  typeof v === 'string' && (itemTabs as readonly string[]).includes(v);

const labels: Record<ItemTab, string> = {
  timeline: 'Timeline',
  spec: 'Spec',
  changes: 'Changes',
  checks: 'Checks',
};

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
    'inline-flex h-11 shrink-0 items-center gap-2 border-b-2 px-3 text-body no-underline',
    active ? 'border-ink font-medium text-ink' : 'border-transparent text-muted hover:text-ink',
  );

export function ItemTabs({ tab, files, checks, className }: Props) {
  const params = useParams({ from: '/o/$org/f/$factory/items/$ref' });
  const counts: Partial<Record<ItemTab, string | undefined>> = {
    changes: files > 0 ? `${files} ${files === 1 ? 'file' : 'files'}` : undefined,
    checks: checks > 0 ? String(checks) : undefined,
  };

  return (
    <nav aria-label="Item views" className={cn('flex gap-1 overflow-x-auto border-b border-line', className)}>
      {itemTabs.map((t) => {
        const active = t === tab;
        return (
          <Link
            key={t}
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
      {(['Trace', 'Teardown'] as const).map((label) => (
        <NotAvailable key={label} label={label}>
          <Button variant="ghost" type="button" className={cn(tabClass(false), 'rounded-none')}>
            {label}
          </Button>
        </NotAvailable>
      ))}
    </nav>
  );
}
