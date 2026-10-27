// The Items list's assembly rows (ADR 0012): an assembly is one row with its parts beneath it, in
// plan order and collapsible; a part is never a row of its own at the top. `listRows` groups what
// the list returned, and `AssemblyRow` and `PartRow` draw the groups.
import { Link } from '@tanstack/react-router';
import { ChevronDown } from 'lucide-react';
import { useId, useState } from 'react';
import type { ItemCard } from '../../api/types';
import { refParam, useFactoryParams } from '../../shared/factory';
import { humanize } from '../../shared/format';
import { describeState, itemStatus, toneText } from '../../shared/item-state';
import { cn } from '../../ui/cn';
import { StatusIcon } from '../../ui/status-icon';

type PartOf = NonNullable<ItemCard['assembly']>;

export type ListRow =
  | { kind: 'item'; key: string; item: ItemCard }
  | {
      kind: 'assembly';
      key: string;
      /** The assembly's own card, when the list or the assemblies fetch has it. */
      card: ItemCard | null;
      /** The part's view of its assembly, when no card has it: ref, title and the part count. */
      partOf: PartOf | null;
      /** The parts the list returned, in plan order. */
      parts: ItemCard[];
    };

/**
 * The rows of the grouped list. A part joins its assembly's row, placed where the assembly (or its
 * first part) first appears; an assembly with no part in the list is left out while a filter is on,
 * since the filter applies to parts. `assemblies` is the list of the `assembly` state, which a filter
 * fetches so that a part's row has its assembly's progress.
 */
export function listRows(items: ItemCard[], assemblies: ItemCard[], filtered: boolean): ListRow[] {
  const partsOf = new Map<string, ItemCard[]>();
  for (const it of items) {
    if (it.assembly) partsOf.set(it.assembly.id, [...(partsOf.get(it.assembly.id) ?? []), it]);
  }
  for (const parts of partsOf.values()) parts.sort((a, b) => (a.assembly?.part ?? 0) - (b.assembly?.part ?? 0));

  const cards = new Map<string, ItemCard>();
  for (const card of [...items, ...assemblies]) if (card.parts) cards.set(card.id, card);

  const rows: ListRow[] = [];
  const placed = new Set<string>();
  for (const it of items) {
    const id = it.assembly?.id ?? (it.parts ? it.id : null);
    if (id === null) {
      rows.push({ kind: 'item', key: it.id, item: it });
      continue;
    }
    if (placed.has(id)) continue;
    placed.add(id);
    const parts = partsOf.get(id) ?? [];
    if (filtered && parts.length === 0) continue;
    rows.push({ kind: 'assembly', key: id, card: cards.get(id) ?? null, partOf: it.assembly ?? null, parts });
  }
  return rows;
}

/** One line of a status: its icon and its words, the words' full text in a title attribute. */
function StatusPill({ item, decides }: { item: ItemCard; decides: boolean }) {
  const label = describeState(item, { decides });
  const status = itemStatus(item);
  return (
    <span
      className={cn(
        'inline-flex h-[22px] max-w-full min-w-0 shrink items-center gap-1.5 rounded-chip border border-line px-2 text-xs',
        toneText[label.tone],
      )}
      title={label.text}
    >
      {status ? <StatusIcon status={status} className="size-3.5" /> : null}
      <span className="truncate">{label.text}</span>
    </span>
  );
}

/** A part under its assembly: its number in the plan, its title, station and status. */
export function PartRow({ item, decides }: { item: ItemCard; decides: boolean }) {
  const params = useFactoryParams();
  const part = item.assembly?.part;
  const station = humanize(item.station);
  return (
    <li className="border-t border-line first:border-t-0">
      <Link
        to="/o/$org/f/$factory/items/$ref"
        params={{ ...params, ref: refParam(item.ref) }}
        className="grid min-w-0 grid-cols-[minmax(0,1fr)] items-center gap-x-4 gap-y-1 py-3 pr-4 pl-12 no-underline transition-colors hover:bg-shell md:grid-cols-[48px_minmax(0,1fr)_130px_170px] md:gap-4 md:pl-16"
      >
        <span className="hidden text-sm text-muted tabular-nums md:block">{part}</span>
        <span className="flex min-w-0 flex-col gap-1 md:contents">
          <span className="truncate text-[15px] leading-snug font-medium" title={item.title}>
            {item.title}
          </span>
          <span className="flex min-w-0 items-center gap-2 text-[13px] text-muted md:hidden">
            <span className="min-w-0 truncate" title={`Part ${part}, ${station}`}>
              Part {part}, {station}
            </span>
            <StatusPill item={item} decides={decides} />
          </span>
        </span>
        <span className="hidden min-w-0 truncate text-sm md:block" title={station}>
          {station}
        </span>
        <span className="hidden min-w-0 md:flex">
          <StatusPill item={item} decides={decides} />
        </span>
      </Link>
    </li>
  );
}

/**
 * An assembly: a chevron that opens or closes its parts, the 'Assembly' label and ref, its title
 * (two lines at most), its open parts' stations on one line, and the progress of its parts. The
 * progress is the assembly's card when one is known; else the part's view of it, with no bar.
 */
export function AssemblyRow({ row, decides }: { row: Extract<ListRow, { kind: 'assembly' }>; decides: boolean }) {
  const params = useFactoryParams();
  const [open, setOpen] = useState(true);
  const partsId = useId();
  const { card, partOf, parts } = row;
  const ref = card?.ref ?? partOf?.ref ?? '';
  const title = card?.title ?? partOf?.title ?? '';
  const summary = card?.parts ?? null;
  const percent = summary && summary.total > 0 ? Math.round((summary.merged / summary.total) * 100) : 0;
  const count = summary
    ? `${summary.merged} of ${summary.total} parts merged`
    : `${partOf?.parts ?? parts.length} parts`;
  const stations = summary ? summary.stations.map(humanize).join(', ') : '';

  return (
    <li className="border-t border-line first:border-t-0">
      <div className="flex items-start gap-3 px-4 py-3">
        <button
          type="button"
          aria-expanded={open}
          aria-controls={open ? partsId : undefined}
          aria-label={`Parts of ${title}`}
          onClick={() => setOpen((o) => !o)}
          className="mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-chip text-muted transition-colors hover:bg-panel hover:text-ink"
        >
          <ChevronDown
            size={16}
            strokeWidth={1.75}
            aria-hidden="true"
            className={cn('transition-transform duration-200', !open && '-rotate-90')}
          />
        </button>
        <Link
          to="/o/$org/f/$factory/items/$ref"
          params={{ ...params, ref: refParam(ref) }}
          className="grid min-w-0 flex-1 gap-1.5 no-underline md:grid-cols-[150px_minmax(0,1fr)_150px_200px] md:items-center md:gap-4"
        >
          <span className="flex min-w-0 items-center gap-2 text-[13px] text-muted">
            <span className="shrink-0">Assembly</span>
            <span className="min-w-0 truncate" title={ref}>
              {ref}
            </span>
          </span>
          <span className="line-clamp-2 text-[15px] leading-snug font-medium" title={title}>
            {title}
          </span>
          <span className="min-w-0 truncate text-sm text-muted" title={stations}>
            {stations}
          </span>
          <span className="flex min-w-0 items-center gap-2 text-sm">
            {summary ? (
              <span aria-hidden="true" className="relative h-1.5 w-20 shrink-0 overflow-hidden rounded-full bg-panel">
                <span
                  className={cn('absolute inset-y-0 left-0 rounded-full', percent === 100 ? 'bg-ok' : 'bg-run')}
                  style={{ width: `${percent}%` }}
                />
              </span>
            ) : null}
            <span className="min-w-0 truncate" title={count}>
              {count}
            </span>
          </span>
        </Link>
      </div>
      {open ? (
        <ul id={partsId} className="m-0 list-none border-t border-line p-0">
          {parts.map((part) => (
            <PartRow key={part.id} item={part} decides={decides} />
          ))}
        </ul>
      ) : null}
    </li>
  );
}
