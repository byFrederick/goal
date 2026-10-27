import { createFileRoute } from '@tanstack/react-router';
import { pageTitle } from '../app/route-focus';
import { NeedsYouPage } from '../features/needs-you/needs-you-page';
import { type NeedsSearch, needsSearch } from '../features/needs-you/search';

// Needs you (ADR 0016, wave 2): what waits on a person across the organization's factories. ?type=
// is the tab (All when absent), ?factory= a factory's name, ?entry= the selected entry; on a phone an
// entry shows the approve view (mobile/approve), so the approve view is a linkable URL.
export const Route = createFileRoute('/o/$org/_org/needs-you')({
  validateSearch: (search: Record<string, unknown>): NeedsSearch => needsSearch(search),
  head: ({ match: { context } }) => pageTitle('Needs you', context.org.name),
  component: NeedsYouPage,
});
