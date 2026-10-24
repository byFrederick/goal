import { createFileRoute, redirect } from '@tanstack/react-router';
import { authConfigQuery, ensureLoaded } from '../api/queries';
import { safeNext } from '../api/session';
import { pageTitle } from '../app/route-focus';
import { SignInPage } from '../features/sign-in/sign-in-page';

export interface SignInSearch {
  /** The page to open once signed in: a path of this origin only (safeNext). */
  next?: string | undefined;
  /** Google's answer, when it sent the person back here. */
  error?: string | undefined;
}

// Sign in (spec 3 §10.1). Under GOAL_AUTH=none there is no sign-in: the page opens the app.
export const Route = createFileRoute('/sign-in')({
  // Both members are always set, undefined when refused: a route's search is laid over the raw
  // search of the URL, which would otherwise show through.
  validateSearch: (search: Record<string, unknown>): SignInSearch => ({
    next: safeNext(search.next),
    error: typeof search.error === 'string' && search.error !== '' ? search.error : undefined,
  }),
  beforeLoad: async ({ context, search }) => {
    const config = await ensureLoaded(context.queryClient, authConfigQuery);
    if (config.mode === 'none') throw redirect({ href: search.next ?? '/', replace: true });
  },
  head: () => pageTitle('Sign in'),
  component: function SignIn() {
    const { next, error } = Route.useSearch();
    return <SignInPage next={next} googleError={error} />;
  },
});
