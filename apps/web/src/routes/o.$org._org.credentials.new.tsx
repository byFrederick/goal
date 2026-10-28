import { createFileRoute } from '@tanstack/react-router';
import { pageTitle } from '../app/route-focus';
import { NewCredentialPage } from '../features/credentials/new-credential-page';
import { type NewCredentialSearch, newCredentialSearch } from '../features/credentials/search';

// New credential (ADR 0017): ?panel=import opens the import panel. Stub until its item builds the
// form (see new-credential-page.tsx).
export const Route = createFileRoute('/o/$org/_org/credentials/new')({
  validateSearch: (search: Record<string, unknown>): NewCredentialSearch => newCredentialSearch(search),
  head: ({ match: { context } }) => pageTitle('New credential', 'Credentials', context.org.name),
  component: NewCredentialPage,
});
