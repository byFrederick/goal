import { createFileRoute, redirect } from '@tanstack/react-router';

// Credentials became Secrets (ADR 0037): the old New credential address opens Import secrets, the one page
// of the old pair that still has a home (a single secret is made in the list's New secret sheet).
export const Route = createFileRoute('/o/$org/_org/credentials/new')({
  beforeLoad: ({ params }) => {
    throw redirect({ to: '/o/$org/secrets/import', params: { org: params.org }, replace: true });
  },
});
