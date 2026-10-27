// The Items list's rows (ADR 0012): an assembly is one row with its parts beneath it, in plan order
// and collapsible; a part is never a row of its own at the top. `listRows` groups what the list
// returned; `AssemblyTableRow` and `ItemTableRow` draw the table's rows, with the Select box of each.
import { Link } from '@tanstack/react-router';
import { ChevronDown, LayoutGrid } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import type { ItemCard } from '../../api/types';
import { refParam, useFactoryParams } from '../../shared/factory';
import { formatAge, formatUsd, humanize } from '../../shared/format';
import { describeState, itemStatus, toneText } from '../../shared/item-state';
import { Chip } from '../../ui/chip';
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
 * since the filter applies to parts. `assemblies` holds the assembly cards, which the unfiltered
 * list carries, so a part's row has its assembly's progress.
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

/**
 * A Select box, 44 px square on a phone. Indeterminate when only some of an assembly's parts are
 * checked, which the browser shows only through the property.
 */
export function SelectBox({
  label,
  checked,
  indeterminate = false,
  onChange,
}: {
  label: string;
  checked: boolean;
  indeterminate?: boolean;
  onChange: () => void;
}) {
  const box = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (box.current) box.current.indeterminate = indeterminate;
  }, [indeterminate]);
  return (
    <span className="flex size-11 items-center justify-center md:size-6">
      <input
        ref={box}
        type="checkbox"
        aria-label={label}
        checked={checked}
        onChange={onChange}
        className="m-0 size-4 cursor-pointer accent-run"
      />
    </span>
  );
}

/** The table cell's padding, shared by every row. */
const cell = 'px-3 py-2 align-middle';

/** An agent's initials on a small round square: the role's first letters, two at most. */
function monogram(agent: string): string {
  return agent
    .split(/[-\s]+/)
    .map((word) => word.charAt(0))
    .join('')
    .slice(0, 2)
    .toUpperCase();
}

/** One item as a table row: its Select box, ref, title, type, lane, station, state, agent, age and cost. */
export function ItemTableRow({
  item,
  decides,
  selected,
  onSelect,
  indent = false,
}: {
  item: ItemCard;
  decides: boolean;
  selected: boolean;
  onSelect: (id: string) => void;
  /** A part sits under its assembly: its title is indented. */
  indent?: boolean;
}) {
  const params = useFactoryParams();
  const label = describeState(item, { decides });
  const status = itemStatus(item);
  return (
    <tr className={cn('border-t border-line', selected ? 'bg-run-tint' : 'hover:bg-panel')}>
      <td className={cn(cell, 'w-px')}>
        <SelectBox label={`Select ${item.ref}`} checked={selected} onChange={() => onSelect(item.id)} />
      </td>
      <td className={cn(cell, 'whitespace-nowrap text-muted tabular-nums')}>{item.ref}</td>
      <td className={cn(cell, indent && 'pl-8')}>
        <Link
          to="/o/$org/f/$factory/items/$ref"
          params={{ ...params, ref: refParam(item.ref) }}
          className="font-medium text-ink no-underline hover:text-run-ink"
        >
          {item.title}
        </Link>
      </td>
      <td className={cell}>{item.type ? <Chip tone="plain">{humanize(item.type)}</Chip> : null}</td>
      <td className={cell}>
        {item.lane ? <Chip tone="plain">{item.lane}</Chip> : <span className="text-muted">Not set</span>}
      </td>
      <td className={cn(cell, 'whitespace-nowrap')}>{humanize(item.station)}</td>
      <td className={cn(cell, 'whitespace-nowrap')}>
        <span className="inline-flex items-center gap-2">
          <span
            className={cn('inline-flex min-w-0 items-center gap-2 text-secondary font-medium', toneText[label.tone])}
            title={label.text}
          >
            {status ? <StatusIcon status={status} /> : null}
            <span>{label.text}</span>
          </span>
          {item.class_of_service === 'expedite' ? <Chip tone="expedite">Expedite</Chip> : null}
        </span>
      </td>
      <td className={cn(cell, 'whitespace-nowrap')}>
        {item.agent ? (
          <span className="inline-flex items-center gap-2 text-secondary text-ink-soft">
            <span
              aria-hidden="true"
              className="grid size-6 place-items-center rounded-full bg-run-tint text-meta font-semibold text-run-ink"
            >
              {monogram(item.agent)}
            </span>
            {item.agent}
          </span>
        ) : (
          <span className="text-muted">None yet</span>
        )}
      </td>
      <td className={cn(cell, 'whitespace-nowrap text-right text-muted tabular-nums')}>
        {formatAge(item.age_seconds)}
      </td>
      <td className={cn(cell, 'whitespace-nowrap text-right font-medium tabular-nums')}>{formatUsd(item.cost_usd)}</td>
    </tr>
  );
}

/**
 * An assembly as a table group: its Select box checks all its parts, a chevron opens or closes them
 * (open by default; `aria-expanded`), then its title, its open parts' stations, its progress and its
 * parts' cost. The progress is the assembly's card when one is known; else the part's view of it,
 * with no bar.
 */
export function AssemblyTableRow({
  row,
  decides,
  selected,
  onSelect,
}: {
  row: Extract<ListRow, { kind: 'assembly' }>;
  decides: boolean;
  /** The ids of the parts checked in the list. */
  selected: ReadonlySet<string>;
  onSelect: (ids: string[]) => void;
}) {
  const params = useFactoryParams();
  const [open, setOpen] = useState(true);
  const { card, partOf, parts } = row;
  const ref = card?.ref ?? partOf?.ref ?? '';
  const title = card?.title ?? partOf?.title ?? '';
  const summary = card?.parts ?? null;
  const percent = summary && summary.total > 0 ? Math.round((summary.merged / summary.total) * 100) : 0;
  const count = summary
    ? `${summary.merged} of ${summary.total} parts merged`
    : `${partOf?.parts ?? parts.length} parts`;
  const stations = summary ? summary.stations.map(humanize).join(', ') : '';
  const checkedParts = parts.filter((part) => selected.has(part.id)).length;
  const cost = parts.reduce((sum, part) => sum + part.cost_usd, 0);

  return (
    <>
      <tr className={cn('border-t border-line bg-panel', checkedParts > 0 && 'bg-run-tint')}>
        <td className={cn(cell, 'w-px')}>
          <SelectBox
            label={`Select the parts of ${title}`}
            checked={parts.length > 0 && checkedParts === parts.length}
            indeterminate={checkedParts > 0 && checkedParts < parts.length}
            onChange={() => onSelect(parts.map((part) => part.id))}
          />
        </td>
        <td className={cn(cell, 'whitespace-nowrap')}>
          <span className="inline-flex items-center gap-2">
            <button
              type="button"
              aria-expanded={open}
              aria-label={`Parts of ${title}`}
              onClick={() => setOpen((o) => !o)}
              className="grid size-11 place-items-center rounded-chip text-muted transition-colors hover:bg-hover hover:text-ink md:size-6"
            >
              <ChevronDown
                size={16}
                strokeWidth={1.75}
                aria-hidden="true"
                className={cn('transition-transform duration-200', !open && '-rotate-90')}
              />
            </button>
            <span className="text-muted tabular-nums">{ref}</span>
          </span>
        </td>
        <td colSpan={6} className={cell}>
          <div className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-2">
            <Link
              to="/o/$org/f/$factory/items/$ref"
              params={{ ...params, ref: refParam(ref) }}
              className="inline-flex min-w-0 items-center gap-2 font-semibold text-ink no-underline hover:text-run-ink"
            >
              <LayoutGrid size={14} strokeWidth={1.75} aria-hidden="true" className="flex-none text-ok-ink" />
              <span className="sr-only">Assembly, </span>
              <span className="truncate" title={title}>
                {title}
              </span>
            </Link>
            <span className="text-secondary text-muted" title={stations}>
              {stations}
            </span>
            <span className="inline-flex items-center gap-2 text-secondary text-ink-soft">
              {summary ? (
                <span
                  aria-hidden="true"
                  className="relative h-1.5 w-20 flex-none overflow-hidden rounded-full bg-hover"
                >
                  <span
                    className={cn('absolute inset-y-0 left-0 rounded-full', percent === 100 ? 'bg-ok' : 'bg-run')}
                    style={{ width: `${percent}%` }}
                  />
                </span>
              ) : null}
              <span>{count}</span>
            </span>
          </div>
        </td>
        <td className={cn(cell, 'text-right text-muted tabular-nums')} aria-hidden="true" />
        <td className={cn(cell, 'whitespace-nowrap text-right font-medium tabular-nums')}>{formatUsd(cost)}</td>
      </tr>
      {open
        ? parts.map((part) => (
            <ItemTableRow
              key={part.id}
              item={part}
              decides={decides}
              indent
              selected={selected.has(part.id)}
              onSelect={(id) => onSelect([id])}
            />
          ))
        : null}
    </>
  );
}
