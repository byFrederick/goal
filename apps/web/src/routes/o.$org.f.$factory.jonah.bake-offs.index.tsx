import { createFileRoute } from '@tanstack/react-router';
import { Suspense } from 'react';
import { pageTitle } from '../app/route-focus';
import { lazyRetrying } from '../shared/lazy';

const BakeOffIndexPage = lazyRetrying(() =>
  import('../features/jonah/bake-off-index-page').then((m) => m.BakeOffIndexPage),
);

// Jonah's Bake-offs (ADR 0029): the factory's newest bake-off, which this route opens, or an empty state when
// there is none. The page reads the list itself, so the route never waits on it.
export const Route = createFileRoute('/o/$org/f/$factory/jonah/bake-offs/')({
  head: ({ match: { context } }) => pageTitle('Bake-offs', context.factory.name),
  component: function BakeOffIndexRoute() {
    return (
      <Suspense fallback={<div aria-busy="true" />}>
        <BakeOffIndexPage />
      </Suspense>
    );
  },
});
