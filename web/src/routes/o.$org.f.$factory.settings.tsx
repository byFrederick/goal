import { createFileRoute } from '@tanstack/react-router';
import { ensureLoaded, factoryQuery, orgQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { FactorySettingsPage } from '../features/factory-settings/settings-page';

// The factory's settings: Pause or Resume, and the Danger zone (archive, restore, delete), shown by
// what the caller may do (the factory's `can` for Pause, the organization's for the Danger zone).
export const Route = createFileRoute('/o/$org/f/$factory/settings')({
  loader: ({ context: { queryClient, org, factory } }) =>
    Promise.all([
      ensureLoaded(queryClient, factoryQuery(org.id, factory.id)),
      ensureLoaded(queryClient, orgQuery(org.id)),
    ]),
  head: ({ match: { context } }) => pageTitle('Settings', context.factory.name),
  component: FactorySettingsPage,
});
