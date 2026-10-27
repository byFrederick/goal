// The Board's header controls from 768 px: the search over the cards, the Board | List view, and the
// top bar's actions (Pull andon and New item). The search and the view sit on the title row, because
// the top bar beside its breadcrumb has no room for them at 768 px (MASTER.md "Top bar"). The phone
// keeps its search in the filters row and its andon in the status line (board-page.tsx).
import { useNavigate } from '@tanstack/react-router';
import { Search } from 'lucide-react';
import type { Board, Factory } from '../../api/types';
import { AndonButton } from '../../shared/andon';
import { useFactoryParams } from '../../shared/factory';
import { NewItemButton } from '../../shared/new-item';
import { SegmentedControl, type SegmentedOption } from '../../ui/segmented-control';

const viewChoices: SegmentedOption<'board' | 'list'>[] = [
  { value: 'board', label: 'Board' },
  { value: 'list', label: 'List' },
];

/** The search over the cards by title or number, kept in the URL as ?q=. */
export function SearchItems({ value, onChange }: { value: string; onChange: (q: string) => void }) {
  return (
    <label className="relative flex min-w-0 max-w-full items-center">
      <span className="sr-only">Search items</span>
      <Search
        size={15}
        strokeWidth={1.75}
        aria-hidden="true"
        className="pointer-events-none absolute left-3 text-muted"
      />
      <input
        type="search"
        value={value}
        placeholder="Search items"
        onChange={(event) => onChange(event.target.value)}
        className="h-10 w-56 max-w-full rounded-control border border-line bg-card pr-3 pl-8 text-sm text-ink placeholder:text-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-run"
      />
    </label>
  );
}

/** Board | List. Board is the page; List goes to the Items page. */
export function BoardViewControl() {
  const params = useFactoryParams();
  const navigate = useNavigate();
  return (
    <SegmentedControl
      label="View"
      options={viewChoices}
      value="board"
      onChange={(view) => {
        if (view === 'list') navigate({ to: '/o/$org/f/$factory/items', params });
      }}
    />
  );
}

/** The top bar's actions on the Board page: Pull andon and New item. */
export function BoardTopBarActions({ factory, andon }: { factory: Factory; andon: Board['line']['andon'] }) {
  return (
    <div className="flex min-w-0 items-center justify-end gap-2">
      <AndonButton factory={factory} andon={andon} />
      <NewItemButton factory={factory} />
    </div>
  );
}
