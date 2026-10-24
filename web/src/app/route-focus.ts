// Focus after navigation and document titles (C §10.1).
import { useRouter } from '@tanstack/react-router';
import { useEffect } from 'react';

/**
 * After every client navigation to another page (not the first load, not a change of search
 * params only), focuses the page's h1 so a screen reader starts at the new page and the next
 * Tab starts from its top.
 */
export function useRouteFocus(): void {
  const router = useRouter();
  useEffect(
    () =>
      router.subscribe('onResolved', ({ fromLocation, pathChanged }) => {
        if (!fromLocation || !pathChanged) return;
        requestAnimationFrame(() => {
          const heading = document.querySelector<HTMLElement>('#main h1');
          if (!heading) return;
          heading.tabIndex = -1;
          heading.focus({ preventScroll: true });
        });
      }),
    [router],
  );
}

/** A document title: "Floor · sre · Acme · Goal", most specific first. */
export function pageTitle(...parts: string[]): { meta: { title: string }[] } {
  return { meta: [{ title: [...parts, 'Goal'].join(' · ') }] };
}
