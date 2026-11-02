// The placeholders a route shows while its data first loads (MOTION.md, Loading). Each draws the layout
// of the page it stands for in muted blocks, with no text, so nothing reads before the page arrives.
// Skeleton pulses them once and fades them into the content; PendingSkeleton is the one wrapper.
import type { ReactNode } from 'react';
import { cn } from '../ui/cn';
import { Skeleton } from '../ui/skeleton';

const block = 'rounded-control bg-hover';

/** A placeholder in the slot of a page's content: the page's own layout, pulsing until it arrives. */
export function PendingSkeleton({ placeholder }: { placeholder: ReactNode }) {
  return (
    <Skeleton ready={false} placeholder={placeholder}>
      {null}
    </Skeleton>
  );
}

function Heading() {
  return (
    <div className="flex flex-col gap-2">
      <div className={cn(block, 'h-7 w-56 max-w-full')} />
      <div className={cn(block, 'h-4 w-80 max-w-full')} />
    </div>
  );
}

/** A page with a heading, two blocks in a row and a large panel: what most pages draw. */
export function PageShape() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-5">
      <Heading />
      <div className="flex flex-col gap-4 md:flex-row">
        <div className={cn(block, 'h-24 flex-1')} />
        <div className={cn(block, 'h-24 flex-1')} />
      </div>
      <div className={cn(block, 'h-64')} />
    </div>
  );
}

function ListRow() {
  return (
    <div className="flex items-center gap-4 border-b border-line px-4 py-4 last:border-b-0">
      <div className={cn(block, 'h-4 w-40 max-w-full')} />
      <div className={cn(block, 'ms-auto h-4 w-24')} />
      <div className={cn(block, 'h-4 w-16')} />
    </div>
  );
}

/** A list page: a heading, a row of filters, then the rows of a table in a bordered card. */
export function ListShape() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-5">
      <Heading />
      <div className="flex items-center gap-2">
        <div className={cn(block, 'h-9 w-64 max-w-full')} />
        <div className={cn(block, 'h-9 w-24')} />
      </div>
      <div className="flex flex-col rounded-card border border-line bg-card">
        <ListRow />
        <ListRow />
        <ListRow />
        <ListRow />
        <ListRow />
        <ListRow />
      </div>
    </div>
  );
}

function BoardColumn() {
  return (
    <div className="flex w-64 flex-none flex-col gap-2">
      <div className={cn(block, 'h-5 w-24')} />
      <div className={cn(block, 'h-1 w-full')} />
      <div className={cn(block, 'h-10')} />
      <div className="flex flex-col gap-2 rounded-cell bg-panel p-2">
        <div className={cn(block, 'h-24')} />
        <div className={cn(block, 'h-24')} />
        <div className={cn(block, 'h-24')} />
      </div>
    </div>
  );
}

/** The Board: its title and the four filter buttons, then its station columns side by side, the last ones cut off at the edge. */
export function BoardShape() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-5">
      <div className="flex flex-wrap items-center gap-2">
        <div className={cn(block, 'h-7 w-24')} />
        <div className={cn(block, 'h-7 w-24')} />
        <div className={cn(block, 'h-7 w-16')} />
        <div className={cn(block, 'h-7 w-24')} />
        <div className={cn(block, 'h-7 w-24')} />
      </div>
      <div className="flex min-w-0 gap-3 overflow-hidden">
        <BoardColumn />
        <BoardColumn />
        <BoardColumn />
        <BoardColumn />
      </div>
    </div>
  );
}

/** The pending component of a list route (runs, items, connections, knowledge, members). */
export function ListPageSkeleton() {
  return <PendingSkeleton placeholder={<ListShape />} />;
}

/** The pending component of the Board route. */
export function BoardSkeleton() {
  return <PendingSkeleton placeholder={<BoardShape />} />;
}

/** The item page: the header and stepper, the tabs and timeline on the left, the details column on the right. */
export function ItemShape() {
  return (
    <div className="@container flex w-full min-w-0 flex-col gap-5">
      <Heading />
      <div className={cn(block, 'h-10')} />
      <div className="grid min-w-0 gap-4 @[1100px]:grid-cols-[minmax(0,1fr)_336px]">
        <div className="flex flex-col gap-4">
          <div className={cn(block, 'h-11')} />
          <div className={cn(block, 'h-16')} />
          <div className={cn(block, 'h-16')} />
          <div className={cn(block, 'h-16')} />
        </div>
        <div className="flex flex-col gap-4">
          <div className={cn(block, 'h-40')} />
          <div className={cn(block, 'h-28')} />
        </div>
      </div>
    </div>
  );
}

/** The pending component of the item route. */
export function ItemSkeleton() {
  return <PendingSkeleton placeholder={<ItemShape />} />;
}
