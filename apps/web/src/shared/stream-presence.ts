// What the person is told about the organization's stream (C §10.10). A stream that drops while the
// page is hidden is not an outage, and a short drop is not one either: the shell's banner and the
// andon say "reconnecting" only once the stream has stayed down for the grace period while the page
// is visible. A return to the page reopens the stream at once, and the reopened stream resyncs, so
// what the screens show is refetched without a banner.
import type { StreamStatus } from '../api/client';

export const graceMs = 5_000;

/** The part of one stream subscription this module drives. */
export interface StreamLink {
  onStatus: (status: StreamStatus) => void;
  onResync: () => void;
}

/** The page as the browser shows it. */
export interface PageSource {
  hidden(): boolean;
  /** Calls `listener(fromCache)` when the page is shown again; `fromCache` for a back/forward-cache restore. */
  onShow(listener: (fromCache: boolean) => void): () => void;
}

export interface PresenceOptions {
  /** Opens one subscription with these callbacks and returns the function that closes it. */
  connect: (link: StreamLink) => () => void;
  page: PageSource;
  /** Called with each change of the status to show; not called for a status that is already shown. */
  onStatus: (status: StreamStatus) => void;
  /** Called when the screens must re-read what they show: events may have been missed. */
  onResync: () => void;
  graceMs?: number;
}

/** Follows the organization's stream and reports the status to show; returns the function that stops it. */
export function followPresence({ connect, page, onStatus, onResync, graceMs: grace = graceMs }: PresenceOptions) {
  let status: StreamStatus = 'connecting';
  let live = false;
  // Set when the stream is reopened after a return: its first open has missed events, so it resyncs.
  let reopened = false;
  let close: (() => void) | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;

  const report = (next: StreamStatus) => {
    if (next === status) return;
    status = next;
    onStatus(next);
  };
  const clearTimer = () => {
    clearTimeout(timer);
    timer = undefined;
  };

  const open = () => {
    close?.();
    live = false;
    close = connect({
      onStatus: (next) => {
        if (next === 'live') {
          live = true;
          clearTimer();
          if (reopened) {
            reopened = false;
            onResync();
          }
          report('live');
        } else if (next === 'reconnecting') {
          live = false;
          // A hidden page shows nothing; the return reopens the stream.
          if (page.hidden()) return;
          timer ??= setTimeout(() => {
            timer = undefined;
            if (!page.hidden()) report('reconnecting');
          }, grace);
        }
      },
      onResync,
    });
  };

  open();
  const stopShow = page.onShow((fromCache) => {
    // A restored page may hold a stream the browser closed while it was in the cache.
    if (live && !fromCache) return;
    clearTimer();
    reopened = true;
    open();
  });

  return () => {
    stopShow();
    clearTimer();
    close?.();
  };
}

/** The browser's page: hidden while the tab or app is in the background. */
export function browserPage(): PageSource {
  return {
    hidden: () => document.visibilityState === 'hidden',
    onShow(listener) {
      const onVisibility = () => {
        if (document.visibilityState === 'visible') listener(false);
      };
      const onPageShow = (event: PageTransitionEvent) => {
        if (event.persisted) listener(true);
      };
      document.addEventListener('visibilitychange', onVisibility);
      window.addEventListener('pageshow', onPageShow);
      return () => {
        document.removeEventListener('visibilitychange', onVisibility);
        window.removeEventListener('pageshow', onPageShow);
      };
    },
  };
}
