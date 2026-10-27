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
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { TooltipProvider } from '../../ui/tooltip';
import { CodePage } from './code-page';

const { signInWithCode } = vi.hoisted(() => ({ signInWithCode: vi.fn() }));

// The code check is the only network call the page makes: the test stands in for the identity service.
vi.mock('../../api/auth', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../api/auth')>();
  return {
    ...actual,
    authMutations: { ...actual.authMutations, signInWithCode: { mutationFn: signInWithCode } },
  };
});

const { AuthFailure } = await import('../../api/auth');

/** The code page inside a router; the sign-in page is the one it links back to. */
function renderCode() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const root = createRootRoute({ component: Outlet });
  const code = createRoute({
    getParentRoute: () => root,
    path: '/sign-in/code',
    component: () => <CodePage email="you@example.com" next={undefined} />,
  });
  const signIn = createRoute({ getParentRoute: () => root, path: '/sign-in', component: () => <p>Sign in page</p> });
  const router = createRouter({
    routeTree: root.addChildren([code, signIn]),
    history: createMemoryHistory({ initialEntries: ['/sign-in/code'] }),
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <RouterProvider router={router} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  signInWithCode.mockReset();
});

afterEach(cleanup);

describe('the code page', () => {
  it('asks for the code sent to the address, with the verify button, the way back and the resend countdown', async () => {
    renderCode();

    expect(await screen.findByRole('heading', { level: 1, name: 'Check your email' })).toBeTruthy();
    expect(document.body.textContent).toContain('We sent a 6-digit code to you@example.com. It expires in 5 minutes.');
    expect(screen.getByLabelText('Code')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Verify' })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Use a different email' }).getAttribute('href')).toBe('/sign-in');

    const resend = screen.getByRole('button', { name: /^Send a new code in \d:\d\d$/ });
    expect((resend as HTMLButtonElement).disabled).toBe(true);
  });

  it('says the code did not work when the identity service refuses it', async () => {
    signInWithCode.mockRejectedValue(new AuthFailure('refused'));
    renderCode();

    // Six digits submit the code by themselves.
    fireEvent.change(await screen.findByLabelText('Code'), { target: { value: '222222' } });

    expect(await screen.findByText('That code didn’t work. Check it, or send a new one.')).toBeTruthy();
    expect(signInWithCode.mock.calls[0]?.[0]).toMatchObject({ email: 'you@example.com', code: '222222' });
  });
});
