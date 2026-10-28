// Search from the sidebar (MASTER.md "The shell", shell.jpg s2): the top row becomes an input, and the
// sidebar's body becomes the results, grouped as Items, Pages, Agents, Connections, Factories and, last,
// Actions. The organization's items, agents, connections and factories come from searchOrg (one read per
// pause in typing); the pages are matched here from the screens registry (ADR 0016, decision 4). The actions
// are New item (for any query), Pause or Resume the line for the open factory, and Approve for each item
// waiting on the caller; each shows only when the caller holds its permission (holds, as the pages decide).
import { useQuery } from '@tanstack/react-query';
import { useNavigate, useParams } from '@tanstack/react-router';
import { Bot, Check, Pause, Play, Plug, Plus, Search, X } from 'lucide-react';
import { type KeyboardEvent, type ReactNode, useEffect, useId, useRef, useState } from 'react';
import { toast } from 'sonner';
import { factoriesQuery, searchQuery } from '../api/queries';
import type { OrgSearch, OrgSummary } from '../api/types';
import { useTogglePause } from '../features/factory-settings/pause-row';
import { factoryTitle } from '../shared/factory';
import { humanize } from '../shared/format';
import { useApprove } from '../shared/item-actions';
import { decisionsFor } from '../shared/item-state';
import { factoryMark } from '../shared/marks';
import { useOrgGrants } from '../shared/org';
import { holds } from '../shared/permissions';
import { Button } from '../ui/button';
import { cn } from '../ui/cn';
import {
  apiKeysScreen,
  connectionsScreen,
  factoryScreens,
  homeScreen,
  knowledgeScreen,
  membersScreen,
  type OrgScreen,
  screensFor,
  settingsScreen,
} from './screens';

const iconProps = { size: 16, strokeWidth: 1.5, 'aria-hidden': true } as const;

/** How many results each group holds (the API's default). */
const limit = 5;
/** The pause in typing after which a search is sent. */
const debounceMs = 150;

/** The top row's icon buttons (Search, Close search); shared with the organization's top row in sidebar.tsx. */
export const iconButton =
  'flex size-8 flex-none items-center justify-center rounded-control text-ink-soft hover:bg-hover hover:text-ink aria-disabled:cursor-default aria-disabled:text-muted aria-disabled:hover:bg-transparent';

/** A result row: 36 px, the picture's row height, with the tint of the active row. */
const resultClass =
  'flex h-9 w-full min-w-0 items-center gap-2 rounded-control px-2 text-left text-body text-ink no-underline hover:bg-hover aria-disabled:cursor-default aria-disabled:text-muted aria-disabled:hover:bg-transparent';

const groupLabel = 'm-0 px-2 pt-3 pb-1 text-meta font-semibold text-muted';

const keyCap =
  'grid h-5 min-w-5 place-items-center rounded-chip border border-rule px-1 text-meta font-semibold text-ink-soft';

const badgeClass =
  'flex size-5 flex-none items-center justify-center rounded-chip bg-run text-meta font-semibold text-white';

/** Whether each word of `query` starts a word of `label`: "pa line" finds "Pause the line". */
function wordsMatch(label: string, query: string): boolean {
  const labelWords = label.toLowerCase().split(' ');
  return query
    .toLowerCase()
    .split(/\s+/)
    .every((word) => labelWords.some((w) => w.startsWith(word)));
}

/** Ctrl+K, or Meta+K on macOS, opens search from anywhere in the organization's layout (not from the code editor). */
export function useSearchShortcut(open: () => void) {
  useEffect(() => {
    const onKeyDown = (e: globalThis.KeyboardEvent) => {
      const mac = /Mac|iPhone|iPad|iPod/.test(navigator.platform);
      const modifier = mac ? e.metaKey : e.ctrlKey;
      if (e.key.toLowerCase() !== 'k' || !modifier || e.altKey || e.shiftKey || e.repeat) return;
      // The factory editor's CodeMirror owns its keys; its own bindings win there.
      if (e.target instanceof Element && e.target.closest('.cm-editor')) return;
      e.preventDefault();
      open();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [open]);
}

/** The value once it has stopped changing for `ms`: one search per pause in typing. */
function useDebounced<T>(value: T, ms: number): T {
  const [settled, setSettled] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setSettled(value), ms);
    return () => clearTimeout(timer);
  }, [value, ms]);
  return settled;
}

/** One result row: what it shows, its accessible name, and what Enter does with it. */
interface Row {
  key: string;
  label: string;
  content: ReactNode;
  open: () => void;
}

interface Group {
  key: string;
  label: string;
  rows: Row[];
}

/**
 * The sidebar while it searches. `onClose(true)` returns focus to the Search icon (Escape, Close search);
 * `onClose(false)` leaves it where the page puts it (a result opened).
 */
export function SidebarSearch({
  org,
  onClose,
  onNewItem,
}: {
  org: OrgSummary;
  onClose: (returnFocus: boolean) => void;
  /** Opens the sidebar's New item composer with the title (the search's New item row). */
  onNewItem: (title: string) => void;
}) {
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  const navigate = useNavigate();
  const { factory: openFactoryName } = useParams({ strict: false });
  const can = useOrgGrants()?.can;
  const input = useRef<HTMLInputElement>(null);
  const listId = useId();
  const approve = useApprove();
  const togglePause = useTogglePause(org, (message) => toast.error(message));

  useEffect(() => input.current?.focus(), []);

  const factories = useQuery(factoriesQuery(org.id));
  const list = (factories.data ?? []).filter((f) => !f.archived_at).toSorted((a, b) => a.name.localeCompare(b.name));
  const openFactory = list.find((f) => f.name === openFactoryName);

  // An empty or blank query sends nothing: `enabled` keeps the read off until there is a word to find.
  const q = useDebounced(query.trim(), debounceMs);
  const search = useQuery({ ...searchQuery(org.id, { q, limit }), enabled: q !== '' });
  const pending = q !== '' && (search.isPending || search.isPlaceholderData);
  const failed = q !== '' && search.isError;
  const result: OrgSearch | undefined = q !== '' && !pending && !failed ? search.data : undefined;

  const go = (open: () => void) => {
    open();
    onClose(false);
  };

  const groups: Group[] = result ? resultGroups(result) : [];
  const rows = groups.flatMap((group) => group.rows);
  const index = rows.length === 0 ? -1 : Math.min(active, rows.length - 1);
  const optionId = (i: number) => `${listId}-${i}`;

  useEffect(() => {
    if (index >= 0) document.getElementById(`${listId}-${index}`)?.scrollIntoView?.({ block: 'nearest' });
  }, [index, listId]);

  /** The rows of each group, from the answer and the screens the search matches on the client. */
  function resultGroups(answer: OrgSearch): Group[] {
    const needle = q.toLowerCase();
    const mark = (name: string) => factoryMark({ name }, list);

    const items: Row[] = answer.items.map((hit) => ({
      key: `item-${hit.id}`,
      label: `${hit.factory_name} #${hit.number}, ${hit.title}`,
      content: (
        <>
          <span aria-hidden="true" className={badgeClass}>
            {mark(hit.factory_name)}
          </span>
          <span className="min-w-0 flex-1 truncate">
            #{hit.number} {hit.title}
          </span>
          <span className="flex-none text-meta text-muted">{humanize(hit.station)}</span>
        </>
      ),
      open: () =>
        go(() =>
          navigate({
            to: '/o/$org/f/$factory/items/$ref',
            params: { org: org.slug, factory: hit.factory_name, ref: String(hit.number) },
          }),
        ),
    }));

    // The organization's pages, then the open factory's. Its Settings is named for what it is; its
    // other pages carry the factory's own title.
    const orgPages = screensFor(
      [homeScreen, membersScreen, apiKeysScreen, connectionsScreen, knowledgeScreen, settingsScreen],
      can,
    ).map((screen: OrgScreen) => ({
      screen,
      detail: 'Organization',
      open: () => navigate({ to: screen.to, params: { org: org.slug } }),
    }));
    const factoryPages = openFactory
      ? factoryScreens.flatMap((screen) =>
          screen.available && 'to' in screen
            ? [
                {
                  screen,
                  detail: screen.id === 'settings' ? 'Settings' : factoryTitle(openFactory),
                  open: () => navigate({ to: screen.to, params: { org: org.slug, factory: openFactory.name } }),
                },
              ]
            : [],
        )
      : [];
    const pages: Row[] = [...orgPages, ...factoryPages]
      .filter(({ screen }) => screen.label.toLowerCase().includes(needle))
      .map(({ screen, detail, open }) => ({
        key: `page-${screen.id}-${detail === 'Organization' ? 'org' : 'factory'}`,
        label: screen.label,
        content: (
          <>
            <screen.icon {...iconProps} />
            <span className="min-w-0 flex-1 truncate">{screen.label}</span>
            <span className="flex-none text-meta text-muted">{detail}</span>
          </>
        ),
        open: () => go(open),
      }));

    const agents: Row[] = answer.agents.map((hit) => ({
      key: `agent-${hit.factory_id}-${hit.name}`,
      label: `${hit.name}, ${hit.factory_name}`,
      content: (
        <>
          <Bot {...iconProps} />
          <span className="min-w-0 flex-1 truncate">{hit.name}</span>
          <span className="flex-none text-meta text-muted">{hit.factory_name}</span>
        </>
      ),
      // The agent's own page is not built yet, so the row opens the Floor of the factory it works in.
      open: () =>
        go(() => navigate({ to: '/o/$org/f/$factory', params: { org: org.slug, factory: hit.factory_name } })),
    }));

    const connections: Row[] = answer.connections.map((hit) => ({
      key: `connection-${hit.name}`,
      label: `${hit.name}, ${humanize(hit.kind)}`,
      content: (
        <>
          <Plug {...iconProps} />
          <span className="min-w-0 flex-1 truncate">{hit.name}</span>
          <span className="flex-none text-meta text-muted">{humanize(hit.kind)}</span>
        </>
      ),
      open: () =>
        go(() => navigate({ to: '/o/$org/connections/$connection', params: { org: org.slug, connection: hit.name } })),
    }));

    const factoryRows: Row[] = answer.factories.map((hit) => ({
      key: `factory-${hit.id}`,
      label: factoryTitle(hit),
      content: (
        <>
          <span aria-hidden="true" className={badgeClass}>
            {mark(hit.name)}
          </span>
          <span className="min-w-0 flex-1 truncate">{factoryTitle(hit)}</span>
        </>
      ),
      open: () => go(() => navigate({ to: '/o/$org/f/$factory', params: { org: org.slug, factory: hit.name } })),
    }));

    // New item for any query, when the caller may add items to some factory. Pause or Resume the line
    // for the open factory, when its words match and the caller may manage it. Approve for each item
    // waiting on the caller, in a factory that holds item:decide.
    const canCreate = list.some((f) => holds(f, 'item:create'));
    const newItemRows: Row[] = canCreate
      ? [
          {
            key: 'action-new-item',
            label: `New item about "${q}"`,
            content: (
              <>
                <Plus {...iconProps} />
                <span className="min-w-0 flex-1 truncate">New item about "{q}"</span>
                <kbd className={keyCap}>C</kbd>
              </>
            ),
            open: () => go(() => onNewItem(q)),
          },
        ]
      : [];
    const pauseLabel = openFactory?.paused ? 'Resume the line' : 'Pause the line';
    const pauseRows: Row[] =
      openFactory && holds(openFactory, 'factory:manage') && wordsMatch(pauseLabel, q)
        ? [
            {
              key: 'action-pause-line',
              label: pauseLabel,
              content: (
                <>
                  {openFactory.paused ? <Play {...iconProps} /> : <Pause {...iconProps} />}
                  <span className="min-w-0 flex-1 truncate">{pauseLabel}</span>
                  <span className="flex-none text-meta text-muted">{factoryTitle(openFactory)}</span>
                </>
              ),
              open: () => go(() => togglePause.mutate(openFactory)),
            },
          ]
        : [];
    const approveRows: Row[] = answer.items.flatMap((hit) => {
      const factory = list.find((f) => f.id === hit.factory_id);
      const station = factory?.stations.find((s) => s.id === hit.station);
      if (!factory || !holds(factory, 'item:decide') || !decisionsFor(hit, station).approve) return [];
      return [
        {
          key: `action-approve-${hit.id}`,
          label: `Approve #${hit.number}`,
          content: (
            <>
              <Check {...iconProps} />
              <span className="min-w-0 flex-1 truncate">Approve #{hit.number}</span>
              <span className="flex-none text-meta text-muted">{hit.factory_name}</span>
            </>
          ),
          open: () => go(() => approve.mutate({ key: hit.id, ref: `#${hit.number}`, factory: hit.factory_id })),
        },
      ];
    });
    const actions: Row[] = [...newItemRows, ...pauseRows, ...approveRows];

    return [
      { key: 'items', label: 'Items', rows: items },
      { key: 'pages', label: 'Pages', rows: pages },
      { key: 'agents', label: 'Agents', rows: agents },
      { key: 'connections', label: 'Connections', rows: connections },
      { key: 'factories', label: 'Factories', rows: factoryRows },
      { key: 'actions', label: 'Actions', rows: actions },
    ].filter((group) => group.rows.length > 0);
  }

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      if (rows.length === 0) return;
      const step = e.key === 'ArrowDown' ? 1 : -1;
      setActive(Math.min(rows.length - 1, Math.max(0, index + step)));
    } else if (e.key === 'Enter') {
      e.preventDefault();
      rows[index]?.open();
    } else if (e.key === 'Escape') {
      e.preventDefault();
      onClose(true);
    }
  };

  const nothingFound = result !== undefined && rows.length === 0;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="mb-2 flex items-center gap-1">
        <div className="flex h-9 min-w-0 flex-1 items-center gap-2 rounded-control border-2 border-run bg-shell px-2 text-ink-soft">
          <Search {...iconProps} className="flex-none" />
          <input
            ref={input}
            type="text"
            role="combobox"
            aria-label="Search"
            aria-expanded={rows.length > 0}
            aria-autocomplete="list"
            aria-controls={rows.length > 0 ? listId : undefined}
            aria-activedescendant={index >= 0 ? optionId(index) : undefined}
            autoComplete="off"
            spellCheck={false}
            placeholder="Search"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setActive(0);
            }}
            onKeyDown={onKeyDown}
            className="min-w-0 flex-1 bg-transparent text-body text-ink outline-none placeholder:text-muted"
          />
        </div>
        <button type="button" aria-label="Close search" onClick={() => onClose(true)} className={iconButton}>
          <X {...iconProps} />
        </button>
      </div>

      <div aria-busy={pending} className="flex min-h-0 flex-1 flex-col overflow-y-auto">
        {pending ? <span className="sr-only">Loading</span> : null}

        {failed ? <ErrorLine onRetry={() => search.refetch()} /> : null}

        {rows.length > 0 ? (
          <div id={listId} role="listbox" aria-label="Search results" className="flex flex-col">
            {groups.map((group) => (
              <fieldset key={group.key} className="m-0 min-w-0 border-0 p-0">
                <legend className={groupLabel}>{group.label}</legend>
                <div className="flex flex-col gap-1">
                  {group.rows.map((row) => {
                    const i = rows.indexOf(row);
                    return (
                      <div
                        key={row.key}
                        id={optionId(i)}
                        role="option"
                        tabIndex={-1}
                        aria-selected={i === index}
                        aria-label={row.label}
                        // A mouse press opens the row; the input keeps focus, so the arrow keys keep working.
                        onMouseDown={(e) => {
                          e.preventDefault();
                          row.open();
                        }}
                        className={cn(resultClass, 'cursor-pointer', i === index && 'bg-run-tint')}
                      >
                        {row.content}
                      </div>
                    );
                  })}
                </div>
              </fieldset>
            ))}
          </div>
        ) : null}

        {nothingFound ? <p className="m-0 px-2 py-1 text-secondary text-muted">Nothing matches "{q}".</p> : null}

        <p className="mt-auto flex flex-wrap items-center gap-2 px-2 pt-3 pb-1 text-meta text-muted">
          <kbd className={keyCap}>↑</kbd> <kbd className={keyCap}>↓</kbd> to move, <kbd className={keyCap}>↵</kbd> to
          open
        </p>
      </div>
    </div>
  );
}

function ErrorLine({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="flex flex-wrap items-center gap-2 px-2 py-1 text-secondary text-muted">
      <span>Could not search.</span>
      <Button size="sm" onClick={onRetry}>
        Retry
      </Button>
    </div>
  );
}
