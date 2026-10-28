// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { authConfigQuery } from '../../api/queries';
import type { AuthConfig } from '../../api/types';
import { TooltipProvider } from '../../ui/tooltip';
import { SignInPage } from './sign-in-page';

const { sendCode } = vi.hoisted(() => ({ sendCode: vi.fn() }));

// The mutation is the only network call the page makes: the test stands in for the identity service.
vi.mock('../../api/auth', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../api/auth')>();
  return {
    ...actual,
    authMutations: { ...actual.authMutations, sendCode: { mutationFn: sendCode } },
  };
});

// AuthFailure is imported after the mock, so it is the real class the page checks with instanceof.
const { AuthFailure } = await import('../../api/auth');

const both: AuthConfig = { mode: 'session', methods: ['email_code', 'google'] } as AuthConfig;
const googleOnly: AuthConfig = { mode: 'session', methods: ['google'] } as AuthConfig;

/** The page inside a router, with the auth config already read. The code page is the one place it goes. */
function renderSignIn(config: AuthConfig, googleError?: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  queryClient.setQueryData(authConfigQuery.queryKey, config);
  const root = createRootRoute({ component: Outlet });
  const signIn = createRoute({
    getParentRoute: () => root,
    path: '/sign-in',
    component: () => <SignInPage next={undefined} googleError={googleError} />,
  });
  const code = createRoute({ getParentRoute: () => root, path: '/sign-in/code', component: () => <p>Code page</p> });
  const router = createRouter({
    routeTree: root.addChildren([signIn, code]),
    history: createMemoryHistory({ initialEntries: ['/sign-in'] }),
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <RouterProvider router={router} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

const typeEmail = async (address: string) => {
  fireEvent.change(await screen.findByLabelText('Work email'), { target: { value: address } });
};

beforeEach(() => {
  sendCode.mockReset();
});

afterEach(cleanup);

describe('the sign-in page', () => {
  it('shows the heading, GitHub sign-in as not available yet, Google, the email field and the invite-only note', async () => {
    renderSignIn(both);

    expect(await screen.findByRole('heading', { level: 1, name: 'Sign in to Goal' })).toBeTruthy();
    expect(screen.getByText('Your factories, and what needs you, wherever you are.')).toBeTruthy();

    const github = screen.getByRole('button', { name: 'Continue with GitHub' });
    expect(github.getAttribute('aria-disabled')).toBe('true');
    expect(screen.getByRole('button', { name: 'Continue with Google' })).toBeTruthy();
    expect(screen.getByLabelText('Work email')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Email me a sign-in code' })).toBeTruthy();
    expect(screen.getByText('Goal is invite-only. Ask an owner of your organization to invite you.')).toBeTruthy();
  });

  it('shows no email form on a Google-only install', async () => {
    renderSignIn(googleOnly);

    expect(await screen.findByRole('button', { name: 'Continue with Google' })).toBeTruthy();
    expect(screen.queryByLabelText('Work email')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Email me a sign-in code' })).toBeNull();
    expect(screen.queryByText('or')).toBeNull();
  });

  it('refuses an address that is not one before anything is sent', async () => {
    renderSignIn(both);
    await typeEmail('not-an-address');
    fireEvent.click(await screen.findByRole('button', { name: 'Email me a sign-in code' }));

    expect(await screen.findByText('Enter your email address, like you@example.com.')).toBeTruthy();
    expect(sendCode).not.toHaveBeenCalled();
  });

  it('sends a code to the address and opens the code page', async () => {
    sendCode.mockResolvedValue(undefined);
    renderSignIn(both);
    await typeEmail('you@example.com');
    fireEvent.click(await screen.findByRole('button', { name: 'Email me a sign-in code' }));

    await waitFor(() => expect(sendCode.mock.calls[0]?.[0]).toBe('you@example.com'));
    expect(await screen.findByText('Code page')).toBeTruthy();
  });

  it('says how long to wait after a rate limit, and keeps the button off until then', async () => {
    sendCode.mockRejectedValue(new AuthFailure('rate-limited', 120));
    renderSignIn(both);
    await typeEmail('you@example.com');
    fireEvent.click(await screen.findByRole('button', { name: 'Email me a sign-in code' }));

    expect(await screen.findByText('Too many tries. Try again in 2 minutes.')).toBeTruthy();
    expect((screen.getByRole('button', { name: 'Email me a sign-in code' }) as HTMLButtonElement).disabled).toBe(true);
  });

  it('shows what went wrong when Google sent the person back without signing them in', async () => {
    renderSignIn(both, 'SIGN_UP_NOT_ALLOWED');

    expect(await screen.findByText('That Google account isn’t invited to Goal.')).toBeTruthy();
  });
});
