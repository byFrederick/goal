// Focus after navigation and document titles (C §10.1).
import { useRouter } from '@tanstack/react-router';
import { useEffect } from 'react';

/**
 * After every client navigation to another page (not the first load, not a change of search
 * params only), focuses the page's h1 so a screen reader starts at the new page and the next
 * Tab starts from its top. When the h1 stayed (a layout kept its header, as the Definition editor
 * does between its sections), the view's own heading marked `data-route-focus` takes focus instead.
 */
export function useRouteFocus(): void {
  const router = useRouter();
  useEffect(() => {
    let before: Element | null = null;
    const offBefore = router.subscribe('onBeforeNavigate', () => {
      before = document.querySelector('#main h1');
    });
    const offResolved = router.subscribe('onResolved', ({ fromLocation, pathChanged }) => {
      if (!fromLocation || !pathChanged) return;
      requestAnimationFrame(() => {
        const h1 = document.querySelector<HTMLElement>('#main h1');
        const view = document.querySelector<HTMLElement>('#main [data-route-focus]');
        const heading = h1 && h1 === before && view ? view : h1;
        if (!heading) return;
        heading.tabIndex = -1;
        heading.focus({ preventScroll: true });
      });
    });
    return () => {
      offBefore();
      offResolved();
    };
  }, [router]);
}

/** A document title: "Floor · sre · Acme · Goal", most specific first. */
export function pageTitle(...parts: string[]): { meta: { title: string }[] } {
  return { meta: [{ title: [...parts, 'Goal'].join(' · ') }] };
}
