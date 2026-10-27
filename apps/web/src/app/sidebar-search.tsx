// Search from the sidebar (MASTER.md "The shell", shell.jpg s2): the top row becomes an input, and the
// sidebar's body becomes the results, grouped as Items, Pages and Actions. Items come from each
// factory's newest page of items (listItems; there is no search endpoint yet), matched here by title
// or `#number`. Pages and Actions are not built yet, so their rows are NotAvailable (ADR 0016, decision 4).
import { useQueries, useQuery } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { Activity, AlignLeft, Plus, Search, X } from 'lucide-react';
import { type KeyboardEvent, type ReactNode, useEffect, useId, useMemo, useRef, useState } from 'react';
import { factoriesQuery, itemsQuery } from '../api/queries';
import type { Factory, ItemCard, OrgSummary } from '../api/types';
import { refParam } from '../shared/factory';
import { humanize } from '../shared/format';
import { factoryMark } from '../shared/marks';
import { Button } from '../ui/button';
import { cn } from '../ui/cn';
import { NotAvailable } from '../ui/not-available';
import { Spinner } from '../ui/spinner';

const iconProps = { size: 16, strokeWidth: 1.5, 'aria-hidden': true } as const;

/** The top row's icon buttons (Search, Close search); shared with the organization's top row in sidebar.tsx. */
export const iconButton =
  'flex size-8 flex-none items-center justify-center rounded-control text-ink-soft hover:bg-hover hover:text-ink aria-disabled:cursor-default aria-disabled:text-muted aria-disabled:hover:bg-transparent';

/** A result row: 36 px, the picture's row height, with the tint of the active row. */
const resultClass =
  'flex h-9 w-full min-w-0 items-center gap-2 rounded-control px-2 text-left text-body text-ink no-underline hover:bg-hover aria-disabled:cursor-default aria-disabled:text-muted aria-disabled:hover:bg-transparent';

const groupLabel = 'm-0 px-2 pt-3 pb-1 text-meta font-semibold text-muted';

type Hit = { key: string; factory: Factory; mark: string; item: ItemCard };

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

/** A title containing the query, or a `#number` starting with it. */
function matches(item: ItemCard, needle: string): boolean {
  if (needle.startsWith('#')) return `#${item.number}`.startsWith(needle);
  return item.title.toLowerCase().includes(needle);
}

/**
 * The sidebar while it searches. `onClose(true)` returns focus to the Search icon (Escape, Close search);
 * `onClose(false)` leaves it where the page puts it (a result opened).
 */
export function SidebarSearch({ org, onClose }: { org: OrgSummary; onClose: (returnFocus: boolean) => void }) {
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  const navigate = useNavigate();
  const input = useRef<HTMLInputElement>(null);
  const listId = useId();
  const itemsLabel = useId();
  const pagesLabel = useId();
  const actionsLabel = useId();

  useEffect(() => input.current?.focus(), []);

  const factories = useQuery(factoriesQuery(org.id));
  const list = useMemo(
    () => (factories.data ?? []).filter((f) => !f.archived_at).toSorted((a, b) => a.name.localeCompare(b.name)),
    [factories.data],
  );
  // Every factory's newest page; the query keys are the ones the items page and the stream keep fresh.
  const pages = useQueries({ queries: list.map((f) => itemsQuery(org.id, f.id, { limit: 200 })) });

  const needle = query.trim().toLowerCase();
  const hits: Hit[] = needle
    ? list.flatMap((factory, i) =>
        (pages[i]?.data?.items ?? [])
          .filter((item) => matches(item, needle))
          .map((item) => ({ key: `${factory.id}-${item.id}`, factory, mark: factoryMark(factory, list), item })),
      )
    : [];

  const pending = factories.isPending || pages.some((p) => p.isPending);
  const failed = factories.isError || pages.some((p) => p.isError);
  const index = hits.length === 0 ? -1 : Math.min(active, hits.length - 1);
  const optionId = (i: number) => `${listId}-${i}`;

  useEffect(() => {
    if (index >= 0) document.getElementById(`${listId}-${index}`)?.scrollIntoView?.({ block: 'nearest' });
  }, [index, listId]);

  const openHit = (hit: Hit) => {
    navigate({
      to: '/o/$org/f/$factory/items/$ref',
      params: { org: org.slug, factory: hit.factory.name, ref: refParam(hit.item.ref) },
    });
    onClose(false);
  };

  const retry = () => {
    if (factories.isError) factories.refetch();
    for (const page of pages) if (page.isError) page.refetch();
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      if (hits.length === 0) return;
      const step = e.key === 'ArrowDown' ? 1 : -1;
      setActive(Math.min(hits.length - 1, Math.max(0, index + step)));
    } else if (e.key === 'Enter') {
      e.preventDefault();
      const hit = hits[index];
      if (hit) openHit(hit);
    } else if (e.key === 'Escape') {
      e.preventDefault();
      onClose(true);
    }
  };

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
            aria-expanded="true"
            aria-autocomplete="list"
            aria-controls={needle ? listId : undefined}
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

        {needle ? (
          <section aria-labelledby={itemsLabel}>
            <p id={itemsLabel} className={cn(groupLabel, 'flex items-center gap-2')}>
              Items
              {pending ? <Spinner tone="dark" className="size-3" /> : null}
            </p>
            {failed ? (
              <ErrorLine onRetry={retry} />
            ) : (
              <>
                <div id={listId} role="listbox" aria-labelledby={itemsLabel} className="flex flex-col gap-1">
                  {hits.map((hit, i) => (
                    <div
                      key={hit.key}
                      id={optionId(i)}
                      role="option"
                      tabIndex={-1}
                      aria-selected={i === index}
                      aria-label={`${hit.factory.name} #${hit.item.number}, ${hit.item.title}`}
                      // A mouse press opens the row; the input keeps focus, so the arrow keys keep working.
                      onMouseDown={(e) => {
                        e.preventDefault();
                        openHit(hit);
                      }}
                      className={cn(resultClass, 'cursor-pointer', i === index && 'bg-run-tint')}
                    >
                      <span
                        aria-hidden="true"
                        className="flex size-5 flex-none items-center justify-center rounded-chip bg-run text-meta font-semibold text-white"
                      >
                        {hit.mark}
                      </span>
                      <span className="min-w-0 flex-1 truncate">
                        #{hit.item.number} {hit.item.title}
                      </span>
                      <span className="flex-none text-meta text-muted">{humanize(hit.item.station)}</span>
                    </div>
                  ))}
                </div>
                {!pending && hits.length === 0 ? (
                  <p className="m-0 px-2 py-1 text-secondary text-muted">No items match "{query.trim()}".</p>
                ) : null}
              </>
            )}
          </section>
        ) : null}

        <section aria-labelledby={pagesLabel}>
          <p id={pagesLabel} className={groupLabel}>
            Pages
          </p>
          <ResultRow icon={<Activity {...iconProps} />} label="Spend and usage" detail="Organization" />
          <ResultRow icon={<AlignLeft {...iconProps} />} label="Budget, in factory settings" detail="Settings" />
        </section>

        <section aria-labelledby={actionsLabel}>
          <p id={actionsLabel} className={groupLabel}>
            Actions
          </p>
          <ResultRow
            icon={<Plus {...iconProps} />}
            label={needle ? `New item about "${query.trim()}"` : 'New item'}
            detail={<kbd className={keyCap}>C</kbd>}
          />
        </section>

        <p className="mt-auto flex flex-wrap items-center gap-2 px-2 pt-3 pb-1 text-meta text-muted">
          <kbd className={keyCap}>↑</kbd>
          <kbd className={keyCap}>↓</kbd>
          <span>to move,</span>
          <kbd className={keyCap}>↵</kbd>
          <span>to open</span>
        </p>
      </div>
    </div>
  );
}

const keyCap =
  'grid h-5 min-w-5 place-items-center rounded-chip border border-rule px-1 text-meta font-semibold text-ink-soft';

/** A row the picture shows that is not built yet: disabled, focusable, never navigates (ADR 0016, decision 4). */
function ResultRow({ icon, label, detail }: { icon: ReactNode; label: string; detail?: ReactNode }) {
  return (
    <NotAvailable>
      <button type="button" className={resultClass}>
        {icon}
        <span className="min-w-0 flex-1 truncate">{label}</span>
        {detail ? <span className="flex-none text-meta text-muted">{detail}</span> : null}
      </button>
    </NotAvailable>
  );
}

function ErrorLine({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="flex flex-wrap items-center gap-2 px-2 py-1 text-secondary text-muted">
      <span>Could not load items.</span>
      <Button size="sm" onClick={onRetry}>
        Retry
      </Button>
    </div>
  );
}
