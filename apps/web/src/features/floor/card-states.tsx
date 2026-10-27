// The states every Floor card shares (rules: web.md, the four states). Loading is the card's sr-only
// "Loading" with aria-busy and no placeholder shape; a failure says what failed and offers Retry.
import type { ReactNode } from 'react';
import { Button } from '../../ui/button';

export function CardLoading() {
  return <span className="sr-only">Loading</span>;
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
