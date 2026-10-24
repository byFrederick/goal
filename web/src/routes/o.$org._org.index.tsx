import { createFileRoute } from '@tanstack/react-router';
import { pageTitle } from '../app/route-focus';
import { FactoriesPage } from '../features/factories/factories-page';

// The organization's home: its factories. The /o/$org layout has already loaded them.
export const Route = createFileRoute('/o/$org/_org/')({
  head: ({ match: { context } }) => pageTitle('Factories', context.org.name),
  component: FactoriesPage,
});
