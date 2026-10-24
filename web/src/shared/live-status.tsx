// Whether the organization's live stream is connected, for screens that show it (the board).
import { cn } from '../ui/cn';
import { useStreamStatus } from './stream';

/** Whether the live stream is connected: a dot and "Live", "Connecting…" or "Reconnecting…". */
export function LiveStatus({ suffix, className }: { suffix?: string; className?: string }) {
  const status = useStreamStatus();
  const text = { connecting: 'Connecting…', live: 'Live', reconnecting: 'Reconnecting…' }[status];
  return (
    <p role="status" className={cn('m-0 flex items-center gap-2 text-[13px] text-muted', className)}>
      <span
        aria-hidden="true"
        className={cn(
          'size-2 rounded-full',
          status === 'live' ? 'bg-ok-dot' : status === 'reconnecting' ? 'bg-rework' : 'bg-faint',
        )}
      />
      <span>
        {text}
        {suffix}
      </span>
    </p>
  );
}
