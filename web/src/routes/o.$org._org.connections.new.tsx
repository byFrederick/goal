import { createFileRoute } from '@tanstack/react-router';
import { connectionKindsQuery, connectionsQuery, ensureLoaded } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { NewConnectionPage } from '../features/connections/new-connection-page';
import { type NewConnectionSearch, newConnectionSearch } from '../features/connections/search';

// New connection (spec 4 §10, canvas "Connection-New"): choose a kind (?kind=), then its fields.
// The kinds load before the page; the connections too, so a name already taken says so at once.
export const Route = createFileRoute('/o/$org/_org/connections/new')({
  validateSearch: (search: Record<string, unknown>): NewConnectionSearch => newConnectionSearch(search),
  loader: ({ context: { queryClient, org } }) =>
    Promise.all([
      ensureLoaded(queryClient, connectionKindsQuery(org.id)),
      ensureLoaded(queryClient, connectionsQuery(org.id)),
    ]),
  head: ({ match: { context } }) => pageTitle('New connection', 'Connections', context.org.name),
  component: NewConnectionPage,
});
