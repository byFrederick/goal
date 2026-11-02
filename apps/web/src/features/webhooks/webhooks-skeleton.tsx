// The Webhooks page's placeholder while its endpoints first load (MOTION.md, Loading): the heading, one
// endpoint card with its rows and the Send a test card beside it, in muted blocks with no text.
import { cn } from '../../ui/cn';

const block = 'rounded-control bg-hover';

export function WebhooksShape() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-5">
      <div className="flex flex-col gap-2">
        <div className={cn(block, 'h-7 w-40 max-w-full')} />
        <div className={cn(block, 'h-4 w-72 max-w-full')} />
      </div>
      <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_400px] xl:items-start">
        <div className="flex min-w-0 flex-col rounded-card border border-line">
          <div className="flex items-center gap-3 border-b border-line px-4 py-3">
            <div className={cn(block, 'h-4 w-40')} />
            <div className={cn(block, 'ms-auto h-5.5 w-32')} />
          </div>
          <div className="flex flex-col gap-3 px-4 py-3">
            <div className={cn(block, 'h-8 w-full max-w-lg')} />
            <div className={cn(block, 'h-5.5 w-64 max-w-full')} />
            <div className={cn(block, 'h-5.5 w-48 max-w-full')} />
            <div className={cn(block, 'h-5.5 w-40 max-w-full')} />
          </div>
          <div className={cn(block, 'm-4 mt-0 h-24')} />
        </div>
        <div className="flex flex-col gap-3 rounded-card border border-line p-4">
          <div className={cn(block, 'h-4 w-24')} />
          <div className={cn(block, 'h-40')} />
          <div className={cn(block, 'h-5.5 w-56 max-w-full')} />
        </div>
      </div>
    </div>
  );
}
