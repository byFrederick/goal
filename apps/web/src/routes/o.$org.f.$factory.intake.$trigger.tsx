import { createFileRoute } from '@tanstack/react-router';
import { ensureLoaded, factoryQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { TriggerPage } from '../features/intake/trigger-page';

// One intake trigger (ADR 0026): this week's results, its rules and recent events, and its YAML in the
// definition. ?on=<event key> is the New trigger picker's choice: a name the definition does not have
// opens a blank trigger for that event.
export const Route = createFileRoute('/o/$org/f/$factory/intake/$trigger')({
  validateSearch: (search: Record<string, unknown>): { on?: string } =>
    typeof search.on === 'string' && search.on !== '' ? { on: search.on } : {},
  loader: ({ context: { queryClient, org, factory } }) => ensureLoaded(queryClient, factoryQuery(org.id, factory.id)),
  head: ({ match: { context }, params }) => pageTitle(params.trigger, 'Intake', context.factory.name),
  component: TriggerPage,
});
