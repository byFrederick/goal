// The status icons, the same everywhere an item's status shows (MASTER's status-icon rule): working is
// a half-filled circle (teal, the run colour), waiting for another item a clock (amber), waiting for
// the same files two crossing arrows (rust), ready or merged a check (green). Decorative: the words
// beside the icon say the same, so the icon is hidden from assistive technology.
import { CircleCheckBig, Clock, Shuffle } from 'lucide-react';
import { cn } from './cn';

export type StatusKind = 'working' | 'waiting' | 'same-files' | 'ready';

const tones: Record<StatusKind, string> = {
  working: 'text-run',
  waiting: 'text-amber',
  'same-files': 'text-rework',
  ready: 'text-ok-dot',
};

export function StatusIcon({ status, className }: { status: StatusKind; className?: string }) {
  const classes = cn('shrink-0', tones[status], className);
  switch (status) {
    case 'working':
      return (
        <svg
          viewBox="0 0 24 24"
          width={16}
          height={16}
          fill="none"
          stroke="currentColor"
          strokeWidth={1.75}
          aria-hidden="true"
          className={classes}
        >
          <circle cx="12" cy="12" r="9" />
          <path d="M12 3a9 9 0 0 1 0 18z" fill="currentColor" stroke="none" />
        </svg>
      );
    case 'waiting':
      return <Clock size={16} strokeWidth={1.75} aria-hidden="true" className={classes} />;
    case 'same-files':
      return <Shuffle size={16} strokeWidth={1.75} aria-hidden="true" className={classes} />;
    case 'ready':
      return <CircleCheckBig size={16} strokeWidth={1.75} aria-hidden="true" className={classes} />;
  }
}
