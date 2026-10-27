import { isCancelledError, useQueryErrorResetBoundary } from '@tanstack/react-query';
import { type ErrorComponentProps, Link, useRouter } from '@tanstack/react-router';
import { useEffect, useRef, useState } from 'react';
import { ApiError } from '../api/client';
import { Button } from '../ui/button';

/** How long a first load may take before the thin line at the top says it is still going. */
const slowLoadMs = 1000;

/**
 * Shown while a screen's first data loads: the app background and nothing else, so the page that
 * follows lands without a layout shift. After a second a thin line fixed to the top of the viewport
 * (out of the flow, so nothing moves) says the load is still on.
 */
export function PendingScreen() {
  const [slow, setSlow] = useState(false);

  useEffect(() => {
    const timer = setTimeout(() => setSlow(true), slowLoadMs);
    return () => clearTimeout(timer);
  }, []);

  return (
    <div aria-busy="true" aria-live="polite">
      <span className="sr-only">Loading</span>
      {slow ? (
        <div aria-hidden="true" className="pointer-events-none fixed inset-x-0 top-0 z-50 h-0.5 overflow-hidden">
          <div className="h-full w-1/3 bg-run motion-safe:animate-[progress-slide_1.4s_ease-in-out_infinite]" />
        </div>
      ) : null}
    </div>
  );
}

/**
 * Why a load failed: goald refused it (an ApiError), the request never got an answer, or it was
 * cancelled (the page that started the fetch was left while it ran): that one is loaded again.
 */
export function loadFailure(error: unknown): 'refused' | 'unreachable' | 'interrupted' {
  if (isCancelledError(error)) return 'interrupted';
  return error instanceof ApiError ? 'refused' : 'unreachable';
}

const failureText = {
  unreachable: {
    title: 'goald is not answering',
    body: 'Check that goald is running (goal up) and that this page is served by it or proxied to it.',
  },
  interrupted: { title: 'This view stopped loading', body: 'Its data was still loading when the page changed.' },
};

/** A failed load: says what failed and offers to try again; a cancelled load tries again once by itself. */
export function ErrorScreen({ error, reset }: ErrorComponentProps) {
  const router = useRouter();
  const queryReset = useQueryErrorResetBoundary();
  const retried = useRef(false);
  const kind = loadFailure(error);

  useEffect(() => queryReset.reset(), [queryReset]);

  const retry = () => {
    reset();
    router.invalidate();
  };

  useEffect(() => {
    if (kind !== 'interrupted' || retried.current) return;
    retried.current = true;
    reset();
    router.invalidate();
  }, [kind, reset, router]);

  const text =
    kind === 'interrupted'
      ? failureText.interrupted
      : error instanceof ApiError
        ? { title: 'This view could not load', body: error.message }
        : failureText.unreachable;
  return (
    <div
      role="alert"
      className="flex max-w-xl flex-col gap-3 rounded-card border border-rework-tint bg-rework-tint p-5"
    >
      <h1 className="m-0 text-xl font-medium text-rework-ink">{text.title}</h1>
      <p className="m-0 text-[15px] leading-relaxed text-ink">{text.body}</p>
      <div>
        <Button variant="primary" onClick={retry}>
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
