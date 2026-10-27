import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
import { ChevronDown } from 'lucide-react';
import { useId, useMemo } from 'react';
import { itemsQuery } from '../../api/queries';
import type { ItemState } from '../../api/types';
import { TopBarSlot } from '../../app/top-bar';
import { useFactory, useFactoryParams } from '../../shared/factory';
import { humanize } from '../../shared/format';
import { NewItemButton } from '../../shared/new-item';
import { useOrg } from '../../shared/org';
import { holds } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { cn } from '../../ui/cn';
import { Menu, MenuChoice, MenuContent, MenuRadioGroup, MenuTrigger } from '../../ui/menu';
import { NotAvailable } from '../../ui/not-available';
import { descriptionClass } from '../../ui/page-header';
import { Switch } from '../../ui/switch';
import { AssemblyTableRow, ItemTableRow, listRows } from './assembly-rows';
import { BulkActions } from './bulk-actions';
import {
  applyFilters,
  countsOf,
  type ItemsFilters,
  type ItemsView,
  isAssemblyCard,
  optionsOf,
  selectedIds,
  selectionParam,
  stateChoices,
} from './items-filters';

/** The value a filter menu shows for "any": no filter in the URL. */
const any = '*';

/** A filter button: dashed while it is unset, a tinted solid button once a value is chosen. 44 px on a phone. */
const filterButton =
  'inline-flex min-h-11 items-center gap-2 rounded-control border border-dashed border-rule bg-card px-3 text-secondary font-medium text-ink-soft transition-colors hover:bg-panel hover:text-ink md:min-h-8';
const chosenButton = 'border-solid border-transparent bg-run-tint text-run-ink hover:bg-run-tint hover:text-run-ink';

/** A saved view or a grouping's link: 44 px on a phone. */
const tabLink =
  'inline-flex min-h-11 items-center gap-2 border-b-2 px-3 text-body no-underline transition-colors md:min-h-9';

/** One filter menu: the chosen value (or "any"), and a radio list of the values the list offers. */
function FilterMenu({
  name,
  chosen,
  choices,
  onChoose,
}: {
  name: string;
  chosen: string | undefined;
  choices: { value: string; label: string }[];
  onChoose: (value: string | undefined) => void;
}) {
  const shown = choices.find((c) => c.value === chosen)?.label ?? chosen;
  return (
    <Menu>
      <MenuTrigger asChild>
        <button type="button" className={cn(filterButton, chosen !== undefined && chosenButton)}>
          {chosen === undefined ? name : `${name}: ${shown}`}
          <ChevronDown size={12} strokeWidth={1.75} aria-hidden="true" />
        </button>
      </MenuTrigger>
      <MenuContent>
        <MenuRadioGroup value={chosen ?? any} onValueChange={(v) => onChoose(v === any ? undefined : v)}>
          <MenuChoice value={any}>{`Any ${name.toLowerCase()}`}</MenuChoice>
          {choices.map((c) => (
            <MenuChoice key={c.value} value={c.value}>
              {c.label}
            </MenuChoice>
          ))}
        </MenuRadioGroup>
      </MenuContent>
    </Menu>
  );
}

/** The Board | List control and New item, in the top bar. List is the page, so it is pressed. */
function ItemsActions() {
  const factory = useFactory();
  const params = useFactoryParams();
  return (
    <>
      <fieldset className="m-0 inline-flex items-center gap-1 rounded-control border border-line bg-panel p-1">
        <legend className="sr-only">View</legend>
        <Link
          to="/o/$org/f/$factory/board"
          params={params}
          className="inline-flex min-h-9 items-center rounded-chip px-3 text-secondary font-medium text-ink-soft no-underline hover:text-ink md:min-h-7"
        >
          Board
        </Link>
        <button
          type="button"
          aria-pressed="true"
          className="inline-flex min-h-9 items-center rounded-chip border border-line bg-card px-3 text-secondary font-semibold text-ink md:min-h-7"
        >
          List
        </button>
      </fieldset>
      <NewItemButton factory={factory} />
    </>
  );
}

const columns = ['Ref', 'Title', 'Type', 'Lane', 'Station', 'State', 'Agent', 'Age', 'Cost'] as const;

export function ItemsPage() {
  const factory = useFactory();
  const params = useFactoryParams();
  const { state, show, view, type, lane, assembly, sel } = useSearch({ from: '/o/$org/f/$factory/items/' });
  const org = useOrg();
  const navigate = useNavigate({ from: '/o/$org/f/$factory/items/' });
  const groupedId = useId();
  const everything = useQuery(itemsQuery(org.id, factory.id, { limit: 200 }));
  const listed = useQuery(itemsQuery(org.id, factory.id, { state, limit: 200 }));
  const actions = useMemo(() => <ItemsActions />, []);

  const canOperate = holds(factory, 'item:operate');
  const decides = holds(factory, 'item:decide');
  const flat = show === 'flat';

  // Every hook is above the early returns. The top bar gets the page's actions, whatever the state.
  const all = everything.data?.items ?? [];
  const filters: ItemsFilters = { view, type, lane, assembly };
  const narrowed =
    state !== undefined || view !== undefined || type !== undefined || lane !== undefined || assembly !== undefined;
  const shown = applyFilters(listed.data?.items ?? [], filters);
  const rows = flat ? [] : listRows(shown, all, narrowed);
  const counts = countsOf(all, Date.now());
  const selected = new Set(selectedIds(sel));
  const checked = shown.filter((it) => !isAssemblyCard(it) && selected.has(it.id));

  const setSearch = (patch: {
    state?: ItemState | undefined;
    view?: ItemsView | undefined;
    type?: string | undefined;
    lane?: string | undefined;
    assembly?: string | undefined;
    show?: 'flat' | undefined;
    sel?: string | undefined;
  }) => navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true, resetScroll: false });

  // The URL's selection is read when the navigation runs, so two quick checks both stick.
  const toggle = (ids: string[]) =>
    navigate({
      search: (prev) => {
        const next = new Set(selectedIds(prev.sel));
        const allChecked = ids.every((id) => next.has(id));
        for (const id of ids) {
          if (allChecked) next.delete(id);
          else next.add(id);
        }
        return { ...prev, sel: selectionParam([...next]) };
      },
      replace: true,
      resetScroll: false,
    });

  if (everything.isPending || listed.isPending) {
    return (
      <main aria-busy="true" className="min-h-40">
        <p className="sr-only">Loading</p>
      </main>
    );
  }

  if (everything.error || listed.error) {
    const error = everything.error ?? listed.error;
    return (
      <div role="alert" className="flex flex-col items-start gap-3 rounded-card border border-line bg-card p-5">
        <h1 className="m-0 text-page-title tracking-display">Items</h1>
        <p className="m-0 text-body text-ink-soft">Could not load the items. {error?.message}</p>
        <Button
          variant="secondary"
          onClick={() => {
            void everything.refetch();
            void listed.refetch();
          }}
        >
          Retry
        </Button>
      </div>
    );
  }

  const types = optionsOf(all, (it) => (isAssemblyCard(it) ? null : it.type));
  const lanes = optionsOf(all, (it) => it.lane ?? null);
  const assemblies = [
    ...new Map(all.flatMap((it) => (it.assembly ? [[it.assembly.id, it.assembly] as const] : []))).values(),
  ];
  const visible = shown.filter((it) => !isAssemblyCard(it)).length;

  const viewKeep = {
    ...(flat ? { show: 'flat' as const } : {}),
    ...(type ? { type } : {}),
    ...(lane ? { lane } : {}),
    ...(assembly ? { assembly } : {}),
  };
  const views = [
    { key: 'open', label: 'Open', count: counts.open, current: !view && state === undefined, search: viewKeep },
    {
      key: 'waiting',
      label: 'Waiting on you',
      count: counts.waiting,
      current: !view && state === 'waiting_human',
      search: { ...viewKeep, state: 'waiting_human' as const },
    },
    {
      key: 'expedite',
      label: 'Expedite',
      count: counts.expedite,
      current: view === 'expedite',
      search: { ...viewKeep, view: 'expedite' as const },
    },
    {
      key: 'done',
      label: 'Done this week',
      count: counts.doneThisWeek,
      current: !view && state === 'done',
      search: { ...viewKeep, state: 'done' as const },
    },
  ];

  return (
    <div className="flex flex-col gap-5">
      <TopBarSlot actions={actions} />
      <header className="flex flex-wrap items-end justify-between gap-x-4 gap-y-2">
        <div className="flex min-w-0 flex-col gap-1">
          <h1 className="m-0 text-page-title tracking-display">Items</h1>
          <p className={descriptionClass}>
            {counts.open} open, {counts.doneThisWeek} done this week
          </p>
        </div>
      </header>

      <nav
        aria-label="Saved views"
        className="-mx-4 flex gap-1 overflow-x-auto border-b border-line px-4 md:mx-0 md:px-0"
      >
        {/* A tab is active by its whole search, and inactive tabs take their aria-current from the view. */}
        {views.map((v) => (
          <Link
            key={v.key}
            to="/o/$org/f/$factory/items"
            params={params}
            search={v.search}
            activeOptions={{ exact: true, includeSearch: true }}
            inactiveProps={{ 'aria-current': v.current ? 'page' : 'false' }}
            className={cn(
              tabLink,
              '-mb-px',
              v.current ? 'border-run font-semibold text-ink' : 'border-transparent text-ink-soft hover:text-ink',
            )}
          >
            {v.label}
            <span className="text-meta text-muted tabular-nums">{v.count}</span>
          </Link>
        ))}
        <NotAvailable>
          <button type="button" className={cn(tabLink, 'border-transparent text-run-ink')}>
            + Save view
          </button>
        </NotAvailable>
      </nav>

      <fieldset className="m-0 flex min-w-0 flex-wrap items-center gap-2 border-0 p-0">
        <legend className="sr-only">Filters</legend>
        <FilterMenu
          name="State"
          chosen={state}
          choices={stateChoices.flatMap((c) => (c.state ? [{ value: c.state, label: c.label }] : []))}
          onChoose={(value) => setSearch({ state: value as ItemState | undefined })}
        />
        <FilterMenu
          name="Type"
          chosen={type}
          choices={types.map((t) => ({ value: t, label: humanize(t) }))}
          onChoose={(value) => setSearch({ type: value })}
        />
        <FilterMenu
          name="Lane"
          chosen={lane}
          choices={lanes.map((l) => ({ value: l, label: l }))}
          onChoose={(value) => setSearch({ lane: value })}
        />
        <FilterMenu
          name="Assembly"
          chosen={assembly}
          choices={assemblies.map((a) => ({ value: a.id, label: `${a.ref} ${a.title}` }))}
          onChoose={(value) => setSearch({ assembly: value })}
        />
        <NotAvailable>
          <button type="button" className={filterButton}>
            Source
            <ChevronDown size={12} strokeWidth={1.75} aria-hidden="true" />
          </button>
        </NotAvailable>
        <div className="ml-auto flex items-center gap-2 text-secondary text-muted">
          <span id={groupedId}>Grouped by assembly</span>
          <Switch
            checked={!flat}
            aria-labelledby={groupedId}
            onCheckedChange={(on) => setSearch({ show: on ? undefined : 'flat' })}
          />
        </div>
      </fieldset>

      {checked.length > 0 ? (
        <BulkActions
          items={checked}
          factory={factory}
          canOperate={canOperate}
          onClear={() => setSearch({ sel: undefined })}
        />
      ) : null}

      {all.length === 0 ? (
        <div className="flex flex-col items-start gap-3 rounded-card border border-line bg-card p-5">
          <p className="m-0 text-body text-ink-soft">No items here. Create one with New item.</p>
          <NewItemButton factory={factory} />
        </div>
      ) : shown.length === 0 ? (
        <p className="m-0 rounded-card border border-line bg-card p-5 text-body text-ink-soft">
          No items match these filters. Try another view or filter.
        </p>
      ) : (
        <>
          <div className="overflow-x-auto rounded-card border border-line bg-card">
            <table aria-label="Items" className="w-full min-w-max border-collapse text-body">
              <thead className="bg-panel text-secondary font-medium text-muted">
                <tr>
                  <th scope="col" className="w-px px-3 py-2 text-left">
                    <span className="sr-only">Select</span>
                  </th>
                  {columns.map((column) => (
                    <th
                      key={column}
                      scope="col"
                      className={cn(
                        'px-3 py-2 text-left font-medium whitespace-nowrap',
                        column === 'Cost' && 'text-right',
                      )}
                    >
                      {column}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {flat
                  ? shown.map((it) => (
                      <ItemTableRow
                        key={it.id}
                        item={it}
                        decides={decides}
                        selected={selected.has(it.id)}
                        onSelect={(id) => toggle([id])}
                      />
                    ))
                  : rows.map((row) =>
                      row.kind === 'item' ? (
                        <ItemTableRow
                          key={row.key}
                          item={row.item}
                          decides={decides}
                          selected={selected.has(row.item.id)}
                          onSelect={(id) => toggle([id])}
                        />
                      ) : (
                        <AssemblyTableRow
                          key={row.key}
                          row={row}
                          decides={decides}
                          selected={selected}
                          onSelect={toggle}
                        />
                      ),
                    )}
              </tbody>
            </table>
          </div>
          <p className="m-0 text-secondary text-muted">
            Showing {visible} of {all.length} items. Moving items skips their station's checks, so Goal asks first.
          </p>
        </>
      )}
    </div>
  );
}
