// A factory's letter badge (MASTER.md "Sidebar" and "Top bar"): its mark on the run colour. The
// sidebar's groups show it at 24 px, the top bar's breadcrumb at 20 px, so the size is the caller's.
import { cn } from '../ui/cn';

export function FactoryBadge({ mark, className }: { mark: string; className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        'flex flex-none items-center justify-center rounded-chip bg-run text-meta font-semibold text-white font-numeral',
        className ?? 'size-6',
      )}
    >
      {mark}
    </span>
  );
}
