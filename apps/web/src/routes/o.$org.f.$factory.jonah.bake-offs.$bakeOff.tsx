import { createFileRoute } from '@tanstack/react-router';
import { Suspense } from 'react';
import { pageTitle } from '../app/route-focus';
import { lazyRetrying } from '../shared/lazy';

const BakeOffPage = lazyRetrying(() => import('../features/jonah/bake-off-page').then((m) => m.BakeOffPage));

// One of Jonah's bake-offs (ADR 0029): one agent's eval cases run on two to four models, with what each model
// passed, what it cost and what the report recommends. The page reads the bake-off itself and shows its own
// loading and error states, so the route never waits on it.
export const Route = createFileRoute('/o/$org/f/$factory/jonah/bake-offs/$bakeOff')({
  head: ({ match: { context } }) => pageTitle('Bake-offs', context.factory.name),
  component: function BakeOffRoute() {
    return (
      <Suspense fallback={<div aria-busy="true" />}>
        <BakeOffPage />
      </Suspense>
    );
  },
});
