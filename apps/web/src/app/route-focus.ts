// Focus after navigation and document titles (C §10.1).
import { useRouter } from '@tanstack/react-router';
import { useEffect } from 'react';

/**
 * After every client navigation to another page (not the first load, not a change of search
 * params only), focuses the page's h1 so a screen reader starts at the new page and the next
 * Tab starts from its top. When the h1 stayed (a layout kept its header, as the Definition editor
 * does between its sections), the view's own heading marked `data-route-focus` takes focus instead,
 * once it has mounted when the view loads lazily.
 */
export function useRouteFocus(): void {
  const router = useRouter();
  useEffect(() => {
    let before: Element | null = null;
    let viewBefore: Element | null = null;
    let focusedBefore: Element | null = null;
    const offBefore = router.subscribe('onBeforeNavigate', () => {
      before = document.querySelector('#main h1');
      viewBefore = document.querySelector('#main [data-route-focus]');
      focusedBefore = document.activeElement;
    });
    let waiting: (() => void) | undefined;
    const offResolved = router.subscribe('onResolved', ({ fromLocation, pathChanged }) => {
      if (!fromLocation || !pathChanged) return;
      waiting?.();
      requestAnimationFrame(() => {
        // The view put focus on what the link asked for (?station= focuses its row): it stays.
        const now = document.activeElement;
        if (now && now !== focusedBefore && now !== document.body && now.closest('#main')) return;
        const h1 = document.querySelector<HTMLElement>('#main h1');
        const view = document.querySelector<HTMLElement>('#main [data-route-focus]');
        if (h1 && h1 === before && (!view || view === viewBefore)) {
          // A view loaded lazily: its heading arrives with its code, the old view showing until then.
          waiting = awaitViewHeading(h1, view);
          return;
        }
        focusHeading(h1 && h1 === before && view ? view : h1);
      });
    });
    return () => {
      offBefore();
      offResolved();
      waiting?.();
    };
  }, [router]);
}

function focusHeading(heading: HTMLElement | null): void {
  if (!heading) return;
  heading.tabIndex = -1;
  heading.focus({ preventScroll: true });
}

/** How long a lazily loaded view's heading is waited for before the h1 takes focus. */
const viewWaitMs = 5000;

/** How long a heading that was already there is given to be replaced before it counts as the new one. */
const sameViewMs = 400;

/**
 * Waits for the view's `data-route-focus` heading to mount (a lazy section's code arriving; the
 * old view stays on screen until then) and focuses it, unless focus moved meanwhile. A heading
 * that was there before (`stale`) is the new one if it is still there after `sameViewMs` (one view
 * serving both routes); after `viewWaitMs` the h1 takes focus. Returns a cancel.
 */
function awaitViewHeading(h1: HTMLElement, stale: HTMLElement | null): () => void {
  const main = document.querySelector('#main');
  const from = document.activeElement;
  const current = () => main?.querySelector<HTMLElement>('[data-route-focus]') ?? null;
  const done = () => {
    observer.disconnect();
    clearTimeout(timer);
    clearTimeout(same);
  };
  const settle = (heading: HTMLElement | null) => {
    done();
    if (document.activeElement === from || document.activeElement === document.body) focusHeading(heading);
  };
  const observer = new MutationObserver(() => {
    const view = current();
    if (view && view !== stale) settle(view);
  });
  if (main) observer.observe(main, { childList: true, subtree: true });
  const same = setTimeout(() => {
    if (stale?.isConnected && current() === stale) settle(stale);
  }, sameViewMs);
  const timer = setTimeout(() => settle(h1), viewWaitMs);
  return done;
}

/** A document title: "Floor · sre · Acme · Goal", most specific first. */
export function pageTitle(...parts: string[]): { meta: { title: string }[] } {
  return { meta: [{ title: [...parts, 'Goal'].join(' · ') }] };
}
