// The Intake page's placeholder while its data first loads (MOTION.md, Loading): the five cards of the page
// (Coming up, origins, triage, triggers, webhooks) as muted blocks, with no text.
import { PendingSkeleton } from '../../app/route-skeletons';
import { cn } from '../../ui/cn';

const block = 'rounded-control bg-hover';
const rows = ['row-1', 'row-2', 'row-3'];
const panels = ['coming-up', 'origins', 'triage', 'triggers', 'webhooks'];

function IntakeShape() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-6">
      <div className="flex flex-col gap-2">
        <div className={cn(block, 'h-7 w-32')} />
        <div className={cn(block, 'h-4 w-80 max-w-full')} />
      </div>
      {panels.map((key) => (
        <div key={key} className="flex flex-col gap-3 rounded-card border border-line p-5">
          <div className={cn(block, 'h-5 w-40 max-w-full')} />
          {rows.map((row) => (
            <div key={row} className="flex items-center gap-4">
              <div className={cn(block, 'h-4 w-24')} />
              <div className={cn(block, 'h-4 w-64 max-w-full')} />
              <div className={cn(block, 'ms-auto h-7 w-20')} />
            </div>
          ))}
        </div>
      ))}
    </div>
  );
}

/** The Intake route's pendingComponent, and the page's own while its query loads. */
export function IntakeSkeleton() {
  return <PendingSkeleton placeholder={<IntakeShape />} />;
}
