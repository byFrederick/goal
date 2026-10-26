// "View only. Editors of goal change its definition." (canvas Define-View): the bar above a page
// the caller may see but not change, saying who changes it, with an optional muted detail.
import { Eye } from 'lucide-react';
import type { ReactNode } from 'react';
import { cn } from './cn';

export function ViewOnlyBanner({
  children,
  detail,
  className,
}: {
  children: ReactNode;
  detail?: ReactNode;
  className?: string;
}) {
  return (
    <p
      className={cn(
        'm-0 flex items-start gap-2.5 rounded-control border border-line bg-surface px-4 py-2.5 text-sm leading-snug',
        className,
      )}
    >
      <Eye size={16} strokeWidth={1.5} aria-hidden="true" className="mt-0.5 shrink-0 text-muted" />
      <span>
        <span className="font-medium text-ink">{children}</span>
        {detail ? <span className="text-muted"> {detail}</span> : null}
      </span>
    </p>
  );
}
