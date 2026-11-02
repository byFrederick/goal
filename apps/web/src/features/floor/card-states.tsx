// The states every Floor card shares (rules: web.md, the four states). Loading is the card's skeleton: a
// muted block that pulses once, then gives way to the content (MOTION.md, Loading). A failure says what failed
// and offers Retry.
import type { ReactNode } from 'react';
import { Button } from '../../ui/button';
import { Skeleton } from '../../ui/skeleton';

export function CardLoading() {
  return (
    <Skeleton ready={false} placeholder={<span className="block h-20 rounded-control bg-hover" />}>
      {null}
    </Skeleton>
  );
}

export function CardFailed({ what, onRetry }: { what: string; onRetry: () => void }) {
  return (
    <div className="flex flex-col items-start gap-3">
      <p role="alert" className="m-0 text-body text-ink-soft">
        Could not load {what}.
      </p>
      <Button size="sm" onClick={onRetry}>
        Retry
      </Button>
    </div>
  );
}

/** An empty card's line: one sentence in the muted tone. */
export function CardEmpty({ children }: { children: ReactNode }) {
  return <p className="m-0 text-body text-muted">{children}</p>;
}
