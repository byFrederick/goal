import { createFileRoute } from '@tanstack/react-router';
import { pageTitle } from '../app/route-focus';
import { ImportSkeleton } from '../app/route-skeletons';
import { ImportSecretsPage } from '../features/secrets/import-secrets-page';

// Import secrets (ADR 0037, canvas "Secrets-Import"): a .env or YAML text, checked by a dry run, then written.
export const Route = createFileRoute('/o/$org/_org/secrets/import')({
  pendingComponent: ImportSkeleton,
  head: ({ match: { context } }) => pageTitle('Import secrets', 'Secrets', context.org.name),
  component: ImportSecretsPage,
});
