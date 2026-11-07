import { createFileRoute } from '@tanstack/react-router';
import { ensureLoaded, factoryQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { TriggerSkeleton } from '../app/route-skeletons';
import { TriggerPage } from '../features/intake/trigger-page';
import { factoryTitle } from '../shared/factory';

// One intake trigger (ADR 0026): this week's results, its rules and recent events, and its YAML in the
// definition. ?on=<event key> is the New trigger picker's choice: a name the definition does not have
// opens a blank trigger for that event; &check=<agent> makes it a check by that agent.
export const Route = createFileRoute('/o/$org/f/$factory/intake/$trigger')({
  pendingComponent: TriggerSkeleton,
  pendingMs: 100,
  pendingMinMs: 300,
  validateSearch: (search: Record<string, unknown>): { on?: string; check?: string } => ({
    ...(typeof search.on === 'string' && search.on !== '' ? { on: search.on } : {}),
    ...(typeof search.check === 'string' && search.check !== '' ? { check: search.check } : {}),
  }),
  loader: ({ context: { queryClient, org, factory } }) => ensureLoaded(queryClient, factoryQuery(org.id, factory.id)),
  head: ({ match: { context }, params }) => pageTitle(params.trigger, 'Intake', factoryTitle(context.factory)),
  component: TriggerPage,
});
