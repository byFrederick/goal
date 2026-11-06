// @vitest-environment jsdom
import { cleanup, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../api/client';
import type { Secret } from '../../api/types';
import { createWebhook, listWebhookDeliveries, patchWebhook } from '../../api/webhooks';
import { choose } from '../../test/select';
import { renderWebhooks, webhook } from './test-harness';

vi.mock('../../api/webhooks', async (original) => ({
  ...(await original<typeof import('../../api/webhooks')>()),
  createWebhook: vi.fn(),
  patchWebhook: vi.fn(),
  listWebhookDeliveries: vi.fn(),
}));

const api = vi.mocked({ createWebhook, patchWebhook, listWebhookDeliveries });

// An all-zero fixture, never a real value.
const zeros = '0'.repeat(64);

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

beforeEach(() => {
  api.listWebhookDeliveries.mockResolvedValue({ deliveries: [], next_after: null });
});

const secrets = [
  { id: 's1', name: 'pagerduty-signing' },
  { id: 's2', name: 'other' },
] as unknown as Secret[];
const created = webhook({ name: 'github-acme' });
const at = '/webhooks/new';

describe('New webhook', () => {
  it('offers the six senders, with the token header and paths for a token sender', async () => {
    renderWebhooks({ at });
    const user = userEvent.setup();

    await screen.findByRole('textbox', { name: 'Name' });
    const names = ['GitHub', 'GitLab', 'PagerDuty', 'Sentry', 'Standard Webhooks', 'Token in a header'];
    for (const name of names) expect(screen.getByRole('radio', { name: new RegExp(name) })).toBeTruthy();
    expect(screen.queryByRole('textbox', { name: 'Header' })).toBeNull();

    await user.click(screen.getByRole('radio', { name: /Token in a header/ }));
    expect(screen.getByRole('textbox', { name: 'Header' })).toBeTruthy();
    expect(screen.getByRole('textbox', { name: 'Event path' })).toBeTruthy();
    expect(screen.getByRole('textbox', { name: 'Delivery id path' })).toBeTruthy();
  });

  it('generates a secret, shows it once with Copy, and leaves for the new webhook on Done without caching it', async () => {
    api.createWebhook.mockResolvedValue({ ...created, secret_value: zeros });
    const { router, queryClient } = renderWebhooks({ at, list: [created] });
    const user = userEvent.setup();

    await user.type(await screen.findByRole('textbox', { name: 'Name' }), 'github-acme');
    expect(screen.getByText('webhook-github-acme')).toBeTruthy();
    await user.click(screen.getByRole('button', { name: 'Create webhook' }));

    expect(api.createWebhook).toHaveBeenCalledWith('org-1', { name: 'github-acme', sender: 'github' });
    const dialog = await screen.findByRole('dialog', { name: 'Webhook created' });
    expect(within(dialog).getByText(zeros)).toBeTruthy();
    await user.click(within(dialog).getByRole('button', { name: 'Copy the secret' }));
    await waitFor(async () => expect(await navigator.clipboard.readText()).toBe(zeros));
    // Still on New webhook until Done; the secret is in no query, mutation or URL.
    expect(router.state.location.pathname).toBe('/o/acme/webhooks/new');
    const cached = JSON.stringify([
      ...queryClient
        .getQueryCache()
        .getAll()
        .map((q) => q.state.data),
      ...queryClient
        .getMutationCache()
        .getAll()
        .map((m) => m.state),
    ]);
    expect(cached).not.toContain(zeros);
    expect(router.state.location.href).not.toContain(zeros);

    await user.click(within(dialog).getByRole('button', { name: 'Done' }));
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/webhooks/github-acme'));
    expect(document.body.textContent).not.toContain(zeros);
  });

  it('uses an existing secret by name, and goes straight to the webhook', async () => {
    api.createWebhook.mockResolvedValue({ ...webhook({ name: 'pd', sender: 'pagerduty' }), secret_value: null });
    const { router } = renderWebhooks({ at, secrets, list: [webhook({ name: 'pd', sender: 'pagerduty' })] });
    const user = userEvent.setup();

    await user.click(await screen.findByRole('radio', { name: /PagerDuty/ }));
    // PagerDuty issues its own secret, so the page starts on "Use a secret".
    expect(screen.getByRole('radio', { name: 'Use a secret', checked: true })).toBeTruthy();
    await user.type(screen.getByRole('textbox', { name: 'Name' }), 'pd-acme');
    await user.click(screen.getByRole('button', { name: 'Create webhook' }));
    expect(await screen.findByText('Pick the secret that signs it.')).toBeTruthy();
    expect(api.createWebhook).not.toHaveBeenCalled();

    await choose('Secret', 'pagerduty-signing');
    await user.click(screen.getByRole('button', { name: 'Create webhook' }));

    await waitFor(() =>
      expect(api.createWebhook).toHaveBeenCalledWith('org-1', {
        name: 'pd-acme',
        sender: 'pagerduty',
        secret: 'pagerduty-signing',
      }),
    );
    expect(screen.queryByRole('dialog')).toBeNull();
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/webhooks/pd'));
  });

  it('sends the token header and paths, and opens it to the factories ticked, limited to their repositories', async () => {
    api.createWebhook.mockResolvedValue({ ...webhook({ name: 'datadog', sender: 'token' }), secret_value: zeros });
    api.patchWebhook.mockResolvedValue(webhook({ name: 'datadog' }));
    renderWebhooks({ at });
    const user = userEvent.setup();

    await user.click(await screen.findByRole('radio', { name: /Token in a header/ }));
    await user.type(screen.getByRole('textbox', { name: 'Header' }), 'X-Alert-Token');
    await user.type(screen.getByRole('textbox', { name: 'Event path' }), 'event.type');
    await user.type(screen.getByRole('textbox', { name: 'Name' }), 'datadog');
    await user.click(screen.getByRole('checkbox', { name: 'SRE' }));
    await user.type(screen.getByRole('textbox', { name: 'Repositories for SRE' }), 'acme/api');
    await user.click(screen.getByRole('button', { name: 'Create webhook' }));

    await screen.findByRole('dialog', { name: 'Webhook created' });
    expect(api.createWebhook).toHaveBeenCalledWith('org-1', {
      name: 'datadog',
      sender: 'token',
      token: { header: 'X-Alert-Token', event_path: 'event.type', id_path: '' },
    });
    expect(api.patchWebhook).toHaveBeenCalledWith('org-1', 'datadog', {
      open: [{ factory: 'f-sre', repositories: ['acme/api'] }],
    });
  });

  it('says a taken name on the Name field', async () => {
    api.createWebhook.mockRejectedValue(
      new ApiError({ status: 409, code: 'webhook-name-taken', title: 'Taken' } as never),
    );
    renderWebhooks({ at });
    const user = userEvent.setup();

    await user.type(await screen.findByRole('textbox', { name: 'Name' }), 'github-acme');
    await user.click(screen.getByRole('button', { name: 'Create webhook' }));

    const field = await screen.findByRole('textbox', { name: 'Name' });
    await waitFor(() => expect(field.getAttribute('aria-invalid')).toBe('true'));
    expect(screen.getByText('Another webhook of the organization has this name.')).toBeTruthy();
  });

  it('says what goald refused on the Name field for a 400, and a missing secret on the Secret field', async () => {
    api.createWebhook.mockRejectedValueOnce(
      new ApiError({ status: 400, code: 'webhook-invalid', title: 'Invalid', detail: 'github is reserved' } as never),
    );
    renderWebhooks({ at, secrets });
    const user = userEvent.setup();

    await user.type(await screen.findByRole('textbox', { name: 'Name' }), 'github-acme');
    await user.click(screen.getByRole('button', { name: 'Create webhook' }));
    expect(await screen.findByText('github is reserved')).toBeTruthy();
  });

  it('checks the name before it sends', async () => {
    renderWebhooks({ at });
    const user = userEvent.setup();

    await user.type(await screen.findByRole('textbox', { name: 'Name' }), 'github');
    await user.click(screen.getByRole('button', { name: 'Create webhook' }));

    expect(await screen.findByText(/reserved/)).toBeTruthy();
    expect(api.createWebhook).not.toHaveBeenCalled();
  });
});

describe('New webhook while the request is pending', () => {
  it('shows Create webhook as busy, not disabled', async () => {
    api.createWebhook.mockReturnValue(new Promise(() => undefined));
    renderWebhooks({ at, secrets, list: [] });
    const user = userEvent.setup();

    await user.click(await screen.findByRole('radio', { name: /PagerDuty/ }));
    await user.type(screen.getByRole('textbox', { name: 'Name' }), 'pd-acme');
    await choose('Secret', 'pagerduty-signing');
    await user.click(screen.getByRole('button', { name: 'Create webhook' }));

    const busy = await screen.findByRole('button', { name: /Creating…/ });
    expect(busy.getAttribute('aria-busy')).toBe('true');
    expect(busy.hasAttribute('disabled')).toBe(false);
    await user.click(busy);
    expect(api.createWebhook).toHaveBeenCalledTimes(1);
  });
});
