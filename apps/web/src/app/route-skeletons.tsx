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

function Panel({ className }: { className?: string }) {
  return <div className={cn('rounded-card border border-line bg-card', className)} />;
}

function HomeShape() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-5">
      <Heading />
      <div className="flex flex-col gap-3">
        <Panel className="h-[86px]" />
        <Panel className="h-[86px]" />
        <Panel className="h-[86px]" />
      </div>
      <div className="grid gap-4 md:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
        <Panel className="h-72" />
        <Panel className="h-72" />
      </div>
    </div>
  );
}

/** The pending component of Home: the factory rows, then Needs you first beside Spend this week. */
export function HomeSkeleton() {
  return <PendingSkeleton placeholder={<HomeShape />} />;
}

function AgentsShape() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-5">
      <Heading />
      <Panel className="h-16" />
      <div className="flex min-w-0 gap-4 overflow-hidden">
        <div className="flex w-64 flex-none flex-col gap-3">
          <div className={cn(block, 'h-5 w-24')} />
          <div className={cn(block, 'h-10 w-16')} />
          <Panel className="h-28" />
        </div>
        <div className="flex w-64 flex-none flex-col gap-3">
          <div className={cn(block, 'h-5 w-24')} />
          <div className={cn(block, 'h-10 w-16')} />
          <Panel className="h-44" />
        </div>
        <div className="flex w-64 flex-none flex-col gap-3">
          <div className={cn(block, 'h-5 w-24')} />
          <div className={cn(block, 'h-10 w-16')} />
          <Panel className="h-28" />
          <Panel className="h-28" />
        </div>
        <div className="flex w-64 flex-none flex-col gap-3">
          <div className={cn(block, 'h-5 w-24')} />
          <div className={cn(block, 'h-10 w-16')} />
          <Panel className="h-44" />
        </div>
      </div>
    </div>
  );
}

/** The pending component of Agents: the factory defaults strip, then one column of cards per station. */
export function AgentsSkeleton() {
  return <PendingSkeleton placeholder={<AgentsShape />} />;
}

function AgentShape() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-5">
      <div className="flex items-start gap-4">
        <div className="size-10 flex-none rounded-full bg-hover" />
        <div className="flex-1">
          <Heading />
        </div>
      </div>
      <div className="flex gap-6 border-b border-line pb-3">
        <div className={cn(block, 'h-5 w-20')} />
        <div className={cn(block, 'h-5 w-16')} />
        <div className={cn(block, 'h-5 w-28')} />
        <div className={cn(block, 'h-5 w-20')} />
      </div>
      <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(0,22rem)]">
        <div className="flex flex-col gap-5">
          <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
            <div className={cn(block, 'h-16')} />
            <div className={cn(block, 'h-16')} />
            <div className={cn(block, 'h-16')} />
            <div className={cn(block, 'h-16')} />
          </div>
          <Panel className="h-48" />
          <Panel className="h-56" />
        </div>
        <Panel className="h-96" />
      </div>
    </div>
  );
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

/** The pending component of an agent: its header and tabs, the stats and sections, the settings panel. */
export function AgentSkeleton() {
  return <PendingSkeleton placeholder={<AgentShape />} />;
}

function ItemsShape() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-5">
      <Heading />
      <div className="flex gap-6 border-b border-line pb-3">
        <div className={cn(block, 'h-5 w-16')} />
        <div className={cn(block, 'h-5 w-28')} />
        <div className={cn(block, 'h-5 w-20')} />
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <div className={cn(block, 'h-9 w-20')} />
        <div className={cn(block, 'h-9 w-20')} />
        <div className={cn(block, 'h-9 w-28')} />
        <div className={cn(block, 'h-9 w-24')} />
      </div>
      <div className="flex flex-col rounded-card border border-line bg-card">
        <div className="h-10 border-b border-line" />
        <ListRow />
        <ListRow />
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

/** The pending component of Items: the title, the view tabs, the filters, then the table's rows. */
export function ItemsSkeleton() {
  return <PendingSkeleton placeholder={<ItemsShape />} />;
}

/** The pending component of the Trigger route. */
export function TriggerSkeleton() {
  return <PendingSkeleton placeholder={<TriggerShape />} />;
}
