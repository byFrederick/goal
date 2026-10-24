import { createFileRoute } from '@tanstack/react-router';
import { settingsQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { OrgSettingsPage } from '../features/org-settings/settings-page';

// The organization's settings: General, slot limits, Delete (C §10.9).
export const Route = createFileRoute('/o/$org/_org/settings')({
  loader: ({ context: { queryClient, org } }) => queryClient.ensureQueryData(settingsQuery(org.id)),
  head: ({ match: { context } }) => pageTitle('Settings', context.org.name),
  component: OrgSettingsPage,
});
