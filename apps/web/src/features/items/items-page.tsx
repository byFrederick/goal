import { useQuery, useSuspenseQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
import { ChevronRight } from 'lucide-react';
import { itemsQuery } from '../../api/queries';
import type { ItemCard, ItemState } from '../../api/types';
import { factoryTitle, refParam, useFactory, useFactoryParams } from '../../shared/factory';
import { formatAge, formatUsd, humanize } from '../../shared/format';
import { InboxButton } from '../../shared/inbox-drawer';
import { describeState, itemStatus, toneText } from '../../shared/item-state';
import { NewItemButton } from '../../shared/new-item';
import { useOrg } from '../../shared/org';
import { holds } from '../../shared/permissions';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { SegmentedControl } from '../../ui/segmented-control';
import { StatusIcon } from '../../ui/status-icon';
import { AssemblyRow, listRows } from './assembly-rows';

export const itemFilters: { state?: ItemState; label: string }[] = [
  { label: 'All' },
  { state: 'waiting_human', label: 'Waiting on you' },
  { state: 'in_progress', label: 'In progress' },
  { state: 'blocked', label: 'Blocked' },
  { state: 'done', label: 'Done' },
];

/** The Show switch: assemblies with their parts (the default, no search param) or every item flat. */
type Show = 'assemblies' | 'flat';

const showOptions: { value: Show; label: string }[] = [
  { value: 'assemblies', label: 'Assemblies with parts' },
  { value: 'flat', label: 'Every item flat' },
];

const footnotes: Record<Show, string> = {
  assemblies: 'Parts are grouped under their assembly. Filters apply to parts.',
  flat: 'Every item is listed on its own, assemblies and parts included. Filters apply to every item.',
};

/** One plain row: an item that is neither an assembly nor a part, or any item in the flat view. */
function ItemRow({ item, decides }: { item: ItemCard; decides: boolean }) {
  const params = useFactoryParams();
  const label = describeState(item, { decides });
  const status = itemStatus(item);
  return (
    <li className="border-t border-line first:border-t-0">
      <Link
        to="/o/$org/f/$factory/items/$ref"
        params={{ ...params, ref: refParam(item.ref) }}
        className="grid grid-cols-[minmax(0,1fr)_12px] items-center gap-3 px-4 py-3 no-underline transition-colors hover:bg-shell md:grid-cols-[72px_minmax(0,1fr)_150px_170px_90px_12px] md:gap-4"
      >
        <span className="hidden text-sm text-muted md:block">{item.ref}</span>
        <span className="flex min-w-0 flex-col gap-1">
          <span className="text-[15px] leading-snug font-medium">{item.title}</span>
          <span className="flex min-w-0 items-center gap-1.5 text-[13px] text-muted md:hidden">
            <span className="min-w-0 truncate">
              {item.ref}, {humanize(item.station)},
            </span>
            <span className={cn('flex min-w-0 items-center gap-1.5', toneText[label.tone])} title={label.text}>
              {status ? <StatusIcon status={status} /> : null}
              <span className="truncate">{label.text}</span>
            </span>
          </span>
          {item.class_of_service === 'expedite' ? (
            <span className="hidden flex-wrap gap-1.5 md:flex">
              <Chip tone="rework">Expedite</Chip>
            </span>
          ) : null}
        </span>
        <span className="hidden text-sm md:block">{humanize(item.station)}</span>
        <span
          className={cn('hidden min-w-0 items-center gap-1.5 text-sm md:flex', toneText[label.tone])}
          title={label.text}
        >
          {status ? <StatusIcon status={status} /> : null}
          <span className="truncate">{label.text}</span>
        </span>
        <span className="hidden text-right text-sm text-muted tabular-nums md:block">
          {formatAge(item.age_seconds)}
          <br />
          {formatUsd(item.cost_usd)}
        </span>
        <ChevronRight size={12} strokeWidth={1.75} aria-hidden="true" className="text-muted" />
      </Link>
    </li>
  );
}

export function ItemsPage() {
  const factory = useFactory();
  const params = useFactoryParams();
  const { state, show } = useSearch({ from: '/o/$org/f/$factory/items/' });
  const org = useOrg();
  const navigate = useNavigate({ from: '/o/$org/f/$factory/items/' });
  const { data } = useSuspenseQuery(itemsQuery(org.id, factory.id, { state, limit: 200 }));
  // A filter also reads the assemblies, so a part's row has its assembly's progress (listRows).
  const { data: assemblies } = useQuery({
    ...itemsQuery(org.id, factory.id, { state: 'assembly', limit: 200 }),
    enabled: state !== undefined,
  });
  const decides = holds(factory, 'item:decide');
  const flat = show === 'flat';
  const rows = flat ? [] : listRows(data.items, assemblies?.items ?? [], state !== undefined);

  return (
    <>
      <header className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <h1 className="m-0 text-[34px] leading-[1.05] font-normal tracking-display md:text-5xl">
          {factoryTitle(factory)} items
        </h1>
        <div className="flex flex-wrap items-center gap-2">
          <InboxButton factory={factory} />
          <span className="md:hidden">
            <NewItemButton factory={factory} compact />
          </span>
          <span className="hidden md:inline-flex">
            <NewItemButton factory={factory} />
          </span>
        </div>
      </header>

      <nav aria-label="Filter items" className="-mx-4 overflow-x-auto px-4 md:mx-0 md:px-0">
        <ul className="m-0 flex list-none gap-1 p-0">
          {itemFilters.map((f) => {
            const active = f.state === state;
            return (
              <li key={f.label}>
                <Link
                  to="/o/$org/f/$factory/items"
                  params={params}
                  search={{ ...(f.state ? { state: f.state } : {}), ...(flat ? { show: 'flat' as const } : {}) }}
                  aria-current={active ? 'page' : undefined}
                  className={cn(
                    'inline-flex h-10 items-center rounded-control px-3.5 text-sm whitespace-nowrap no-underline transition-colors',
                    active ? 'bg-ink text-white' : 'bg-panel text-ink hover:bg-hover',
                  )}
                >
                  {f.label}
                </Link>
              </li>
            );
          })}
        </ul>
      </nav>

      <div className="flex flex-wrap items-center gap-3">
        <span className="text-sm text-muted">Show:</span>
        <SegmentedControl<Show>
          label="Show"
          options={showOptions}
          value={flat ? 'flat' : 'assemblies'}
          onChange={(value) =>
            navigate({
              search: (prev) => ({ ...prev, show: value === 'flat' ? ('flat' as const) : undefined }),
              replace: true,
              resetScroll: false,
            })
          }
        />
      </div>

      {data.items.length === 0 ? (
        <p className="m-0 rounded-card border border-line bg-card p-5 text-[15px] text-muted">
          No items here. {state ? 'Try another filter.' : 'Create one with New item.'}
        </p>
      ) : (
        <>
          <ul className="m-0 flex list-none flex-col overflow-hidden rounded-card border border-line bg-card p-0">
            {flat
              ? data.items.map((it) => <ItemRow key={it.id} item={it} decides={decides} />)
              : rows.map((row) =>
                  row.kind === 'item' ? (
                    <ItemRow key={row.key} item={row.item} decides={decides} />
                  ) : (
                    <AssemblyRow key={row.key} row={row} decides={decides} />
                  ),
                )}
          </ul>
          <p className="m-0 text-[13px] text-muted">{footnotes[flat ? 'flat' : 'assemblies']}</p>
        </>
      )}
    </>
  );
}
