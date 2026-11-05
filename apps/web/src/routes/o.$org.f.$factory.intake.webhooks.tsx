import { createFileRoute, redirect } from '@tanstack/react-router';
import { PendingScreen } from '../app/states';

// Webhooks moved from the factory to the organization (ADR 0040 decision 12): the factory's old address goes
// to the organization's page, so the links in Intake and in onboarding keep working.
export const Route = createFileRoute('/o/$org/f/$factory/intake/webhooks')({
  pendingComponent: PendingScreen,
  beforeLoad: ({ params }) => {
    throw redirect({ to: '/o/$org/webhooks', params: { org: params.org }, replace: true });
  },
});
