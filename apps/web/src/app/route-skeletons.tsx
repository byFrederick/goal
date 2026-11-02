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
    <div className="flex w-64 flex-none flex-col gap-3">
      <div className={cn(block, 'h-5 w-24')} />
      <div className={cn(block, 'h-20')} />
      <div className={cn(block, 'h-20')} />
      <div className={cn(block, 'h-20')} />
    </div>
  );
}

/** The Board: a heading, then its station columns side by side, the last ones cut off at the edge. */
export function BoardShape() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-5">
      <Heading />
      <div className="flex min-w-0 gap-4 overflow-hidden">
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

/** The Trigger page: its title, then four form blocks beside the recent events and the YAML. */
export function TriggerShape() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <div className="flex items-center gap-4">
        <div className={cn(block, 'h-7 w-56 max-w-full')} />
        <div className={cn(block, 'h-5 w-16')} />
        <div className={cn(block, 'ms-auto hidden h-4 w-72 sm:block')} />
      </div>
      <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(0,360px)]">
        <div className="flex min-w-0 flex-col gap-4">
          <div className={cn(block, 'h-36')} />
          <div className={cn(block, 'h-40')} />
          <div className={cn(block, 'h-72')} />
          <div className={cn(block, 'h-28')} />
        </div>
        <div className="flex min-w-0 flex-col gap-4">
          <div className={cn(block, 'h-64')} />
          <div className={cn(block, 'h-48')} />
        </div>
      </div>
    </div>
  );
}

/** The pending component of the Trigger route. */
export function TriggerSkeleton() {
  return <PendingSkeleton placeholder={<TriggerShape />} />;
}

/** The Definition editor: the draft strip and Ask bar over the file tree, the editor and the outline. */
export function DefinitionShape() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <div className={cn(block, 'h-12')} />
      <div className={cn(block, 'h-10')} />
      <div className="flex min-h-180 overflow-hidden rounded-card border border-rule">
        <div className="hidden w-1/5 flex-none flex-col gap-2 bg-sidebar p-3 @[640px]:flex">
          <div className={cn(block, 'h-8')} />
          <div className={cn(block, 'h-7')} />
          <div className={cn(block, 'h-7')} />
          <div className={cn(block, 'h-7')} />
          <div className={cn(block, 'h-7')} />
        </div>
        <div className="flex min-w-0 flex-1 flex-col gap-3 bg-panel p-5">
          <div className={cn(block, 'h-5 w-40')} />
          <div className={cn(block, 'h-4 w-2/3')} />
          <div className={cn(block, 'h-4 w-1/2')} />
          <div className={cn(block, 'h-4 w-3/4')} />
          <div className={cn(block, 'h-4 w-1/3')} />
        </div>
        <div className="hidden w-1/5 flex-none flex-col gap-2 bg-sidebar p-3 @[1024px]:flex">
          <div className={cn(block, 'h-5 w-20')} />
          <div className={cn(block, 'h-6')} />
          <div className={cn(block, 'h-6')} />
          <div className={cn(block, 'h-6')} />
        </div>
      </div>
    </div>
  );
}

/** The pending component of the Definition editor's routes. */
export function DefinitionSkeleton() {
  return <PendingSkeleton placeholder={<DefinitionShape />} />;
}
