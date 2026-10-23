import { useQueryErrorResetBoundary } from '@tanstack/react-query';
import { type ErrorComponentProps, Link, useRouter } from '@tanstack/react-router';
import { useEffect } from 'react';
import { ApiError } from '../api/client';
import { Button } from '../ui/button';

/** Shown while a screen's first data loads; reserves the space the cards will take. */
export function PendingScreen() {
  return (
    <div aria-busy="true" aria-live="polite" className="flex flex-col gap-5">
      <span className="sr-only">Loading</span>
      <div className="h-12 w-64 max-w-full rounded-control bg-control/70" />
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {[0, 1, 2].map((i) => (
          <div key={i} className="h-72 rounded-card border border-line bg-card" />
        ))}
      </div>
    </div>
  );
}

/** A failed load: says what failed and offers to try again. */
export function ErrorScreen({ error, reset }: ErrorComponentProps) {
  const router = useRouter();
  const queryReset = useQueryErrorResetBoundary();

  useEffect(() => queryReset.reset(), [queryReset]);

  const unreachable = !(error instanceof ApiError);
  return (
    <div
      role="alert"
      className="flex max-w-xl flex-col gap-3 rounded-card border border-rework-line bg-rework-tint p-5"
    >
      <h1 className="m-0 text-xl font-medium text-rework-ink">
        {unreachable ? 'goald is not answering' : 'This view could not load'}
      </h1>
      <p className="m-0 text-[15px] leading-relaxed text-ink">
        {unreachable
          ? 'Check that goald is running (goal up) and that this page is served by it or proxied to it.'
          : error.message}
      </p>
      <div>
        <Button
          variant="primary"
          onClick={() => {
            reset();
            router.invalidate();
          }}
        >
          Try again
        </Button>
      </div>
    </div>
  );
}

export function NotFoundScreen({ what = 'page' }: { what?: string }) {
  return (
    <div className="flex max-w-xl flex-col gap-3 rounded-card border border-line bg-card p-5">
      <h1 className="m-0 text-xl font-medium">No such {what}</h1>
      <p className="m-0 text-[15px] text-muted">It may have been renamed, or the link is from another goald.</p>
      <div>
        <Button asChild variant="primary">
          <Link to="/">Go to the floor</Link>
        </Button>
      </div>
    </div>
  );
}
