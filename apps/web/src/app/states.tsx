import { isCancelledError, useQueryErrorResetBoundary } from '@tanstack/react-query';
import { type ErrorComponentProps, Link, useRouter } from '@tanstack/react-router';
import { useEffect, useRef } from 'react';
import { ApiError } from '../api/client';
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

/**
 * Not found, centred in the content area under the top bar (the design's "Not found"): a heading, one
 * sentence and one action. The caller's main region provides the Route change fade and rise.
 */
export function NotFoundScreen({ what = 'page' }: { what?: string }) {
  return (
    <div className="flex flex-1 flex-col items-start justify-center gap-3 pb-15">
      <h1 className="m-0 text-page-title tracking-[-0.01em]">No such {what}</h1>
      <p className="m-0 text-body text-muted">It may have been renamed, or the link is from another goald.</p>
      <Button asChild variant="action">
        <Link to="/">Go to the floor</Link>
      </Button>
    </div>
  );
}
