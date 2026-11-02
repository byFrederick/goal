// The Intake page's placeholder while its data first loads (MOTION.md, Loading): the sources panel, the
// triggers table and the suggestion cards in muted blocks, with no text.
import { PendingSkeleton } from '../../app/route-skeletons';
import { cn } from '../../ui/cn';

const block = 'rounded-control bg-hover';
const rows = ['row-1', 'row-2', 'row-3', 'row-4', 'row-5'];
const cards = ['card-1', 'card-2', 'card-3'];

function IntakeShape() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-6">
      <div className="flex flex-col gap-2">
        <div className={cn(block, 'h-7 w-32')} />
        <div className={cn(block, 'h-4 w-80 max-w-full')} />
      </div>
      <div className="flex flex-col gap-4 rounded-card border border-line p-5">
        <div className={cn(block, 'h-5 w-64 max-w-full')} />
        <div className={cn(block, 'h-3.5 w-full')} />
      </div>
      <div className="flex flex-col rounded-card border border-line">
        {rows.map((key) => (
          <div key={key} className="flex items-center gap-4 border-b border-line px-5 py-4 last:border-b-0">
            <div className={cn(block, 'h-4 w-40 max-w-full')} />
            <div className={cn(block, 'ms-auto h-4 w-24')} />
          </div>
        ))}
      </div>
      <div className="grid gap-3 md:grid-cols-3">
        {cards.map((key) => (
          <div key={key} className={cn(block, 'h-32 rounded-card')} />
        ))}
      </div>
    </div>
  );
}

/** The Intake route's pendingComponent, and the page's own while its query loads. */
export function IntakeSkeleton() {
  return <PendingSkeleton placeholder={<IntakeShape />} />;
}
