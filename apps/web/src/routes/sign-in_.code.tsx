import { createFileRoute, redirect, useLocation } from '@tanstack/react-router';
import { safeNext } from '../api/session';
import { pageTitle } from '../app/route-focus';
import { CodePage } from '../features/sign-in/code-page';

declare module '@tanstack/react-router' {
  interface HistoryState {
    /** The address a sign-in code was sent to: page state, never in the URL. */
    email?: string;
  }
}

// The emailed code (spec 3 §10.1). Without the address in the page's state (a link, a new tab),
// it asks for the email again.
export const Route = createFileRoute('/sign-in_/code')({
  // Always set, undefined when refused: the raw search of the URL would otherwise show through.
  validateSearch: (search: Record<string, unknown>): { next?: string | undefined } => ({ next: safeNext(search.next) }),
  beforeLoad: ({ location, search }) => {
    if (!location.state.email) throw redirect({ to: '/sign-in', search, replace: true });
  },
  head: () => pageTitle('Check your email'),
  component: function Code() {
    const { next } = Route.useSearch();
    const email = useLocation({ select: (l) => l.state.email ?? '' });
    return <CodePage email={email} next={next} />;
  },
});
