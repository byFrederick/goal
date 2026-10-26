import { createFileRoute } from '@tanstack/react-router';
import {
  authConfigQuery,
  ensureLoaded,
  factoriesQuery,
  invitationsQuery,
  membersQuery,
  orgQuery,
} from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { MembersPage } from '../features/members/members-page';

// Members and roles (spec 3 §10.1): the members, their factories' names and what the caller may
// do, in parallel; the pending invitations only for those who may read them (owners and admins).
export const Route = createFileRoute('/o/$org/_org/members')({
  loader: async ({ context: { queryClient, org } }) => {
    const [detail] = await Promise.all([
      ensureLoaded(queryClient, orgQuery(org.id)),
      ensureLoaded(queryClient, membersQuery(org.id)),
      ensureLoaded(queryClient, factoriesQuery(org.id)),
      ensureLoaded(queryClient, authConfigQuery),
    ]);
    if (detail.can.includes('invitation:read')) await ensureLoaded(queryClient, invitationsQuery(org.id));
  },
  head: ({ match: { context } }) => pageTitle('Members and roles', context.org.name),
  component: MembersPage,
});
