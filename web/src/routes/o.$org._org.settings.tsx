import { createFileRoute } from '@tanstack/react-router';
import { authConfigQuery, ensureLoaded, orgQuery, settingsQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { OrgSettingsPage } from '../features/org-settings/settings-page';

// The organization's settings: General, slot limits, Transfer ownership and Delete (C §10.9,
// spec 3 §10.2), shown by what the caller may do (the organization's can).
export const Route = createFileRoute('/o/$org/_org/settings')({
  loader: ({ context: { queryClient, org } }) =>
    Promise.all([
      ensureLoaded(queryClient, settingsQuery(org.id)),
      ensureLoaded(queryClient, orgQuery(org.id)),
      ensureLoaded(queryClient, authConfigQuery),
    ]),
  head: ({ match: { context } }) => pageTitle('Settings', context.org.name),
  component: OrgSettingsPage,
});
