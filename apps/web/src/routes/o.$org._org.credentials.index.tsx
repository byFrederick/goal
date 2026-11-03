import { createFileRoute, redirect } from '@tanstack/react-router';
import { PendingScreen } from '../app/states';

// Credentials became Secrets (ADR 0037): the old list's address opens the new one.
export const Route = createFileRoute('/o/$org/_org/credentials/')({
  pendingComponent: PendingScreen,
  beforeLoad: ({ params }) => {
    throw redirect({ to: '/o/$org/secrets', params: { org: params.org }, replace: true });
  },
});
