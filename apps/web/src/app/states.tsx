import { isCancelledError, useQueryErrorResetBoundary } from '@tanstack/react-query';
import { type ErrorComponentProps, Link, useRouter } from '@tanstack/react-router';
import { useEffect, useRef } from 'react';
import { ApiError, NetworkError } from '../api/client';
import { isStarting, restartDelay } from '../api/retry';
import { InfoTip } from '../features/jonah/info-tip';
import { Button } from '../ui/button';
import { PageShape, PendingSkeleton } from './route-skeletons';

/**
 * Shown while a route's data first loads: the page's skeleton, which pulses once and fades out as the
 * page arrives (MOTION.md, Loading). The router shows it after 100 ms and keeps it for at least 300 ms
 * (router-options.ts). A route with its own layout sets another pendingComponent (route-skeletons.tsx).
 */
export function PendingScreen() {
  return <PendingSkeleton placeholder={<PageShape />} />;
}

/**
 * Why a load failed: goald is unreachable (the request got no answer, or a gateway answered 502 or 504), restarting (a 503 that is not a missing feature), goald refused it (another ApiError), it was cancelled (the page that started the fetch was
 * left while it ran: that one is loaded again), or the page's own code crashed.
 */
export function loadFailure(error: unknown): 'refused' | 'unreachable' | 'starting' | 'interrupted' | 'crashed' {
  if (isCancelledError(error)) return 'interrupted';
  if (error instanceof NetworkError) return 'unreachable';
  if (isStarting(error)) return 'starting';
  if (error instanceof ApiError) return [502, 503, 504].includes(error.status) ? 'unreachable' : 'refused';
  return 'crashed';
}

const failureText = {
  unreachable: {
    title: 'goald is not answering',
    body: 'Check that goald is running (goal up) and that this page is served by it or proxied to it.',
  },
  crashed: { title: 'This page hit an error', body: '' },
  interrupted: { title: 'This view stopped loading', body: 'Its data was still loading when the page changed.' },
};

/** A failed load: says what failed and offers to try again; a cancelled load tries again once by itself. */
export function ErrorScreen({ error, reset }: ErrorComponentProps) {
  const router = useRouter();
  const queryReset = useQueryErrorResetBoundary();
  const retried = useRef(false);
  const kind = loadFailure(error);

  const attempt = useRef(0);

  useEffect(() => queryReset.reset(), [queryReset]);

  // A restarting goald is asked again by itself, after Retry-After; a still-failing answer waits the next delay.
  useEffect(() => {
    if (kind !== 'starting') return;
    const wait = error instanceof ApiError ? error.retryAfterSeconds : undefined;
    const timer = window.setTimeout(
      () => {
        attempt.current += 1;
        reset();
        router.invalidate();
      },
      restartDelay(attempt.current, wait),
    );
    return () => window.clearTimeout(timer);
  }, [kind, error, reset, router]);

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

  if (kind === 'starting') {
    return (
      <div role="status" className="flex max-w-xl flex-col gap-3 rounded-card border border-line bg-card p-5">
        <h1 className="m-0 text-xl font-medium text-ink">Goal is restarting</h1>
        <p className="m-0 text-body text-muted">This page loads again by itself.</p>
        <div>
          <Button variant="primary" onClick={retry}>
            Try again
          </Button>
        </div>
      </div>
    );
  }
  const text =
    kind === 'interrupted'
      ? failureText.interrupted
      : kind === 'refused' && error instanceof ApiError
        ? { title: 'This view could not load', body: error.message }
        : kind === 'crashed'
          ? failureText.crashed
          : failureText.unreachable;
  const detail = kind === 'crashed' && error instanceof Error ? error.message : '';
  return (
    <div
      role="alert"
      className="flex max-w-xl flex-col gap-3 rounded-card border border-rework-tint bg-rework-tint p-5"
    >
      <h1 className="m-0 text-xl font-medium text-rework-ink">{text.title}</h1>
      {text.body ? <p className="m-0 text-[15px] leading-relaxed text-ink">{text.body}</p> : null}
      <div className="flex items-center gap-2">
        <Button variant="primary" onClick={kind === 'crashed' ? () => window.location.reload() : retry}>
          {kind === 'crashed' ? 'Reload' : 'Try again'}
        </Button>
        {detail ? <InfoTip label="What went wrong">{detail}</InfoTip> : null}
      </div>
    </div>
  );
}

/**
 * What a not-found of a known organization says it is about. A not-found thrown in a route's beforeLoad is shown by
 * the parent route's boundary, so the organization route reads this to name the missing factory.
 */
export const unknownFactory = { unknown: 'factory' } as const;

/**
 * Not found, centred in the content area under the top bar (the design's "Not found"): a heading, one
 * sentence and one action. The caller's main region provides the Route change fade and rise.
 */
export function NotFoundScreen({ what = 'page' }: { what?: string }) {
  return (
    <div className="flex flex-1 flex-col items-start justify-center gap-3 pb-15">
      <h1 className="m-0 text-page-title tracking-[-0.01em]">No such {what}</h1>
      <p className="m-0 text-body text-muted">It may have been renamed, or the link is from another installation.</p>
      <Button asChild variant="action">
        <Link to="/">Go to the floor</Link>
      </Button>
    </div>
  );
}
