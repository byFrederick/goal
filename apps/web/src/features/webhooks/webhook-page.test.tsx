// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../api/client';
import {
  deleteWebhook,
  getWebhookDelivery,
  listWebhookDeliveries,
  patchWebhook,
  type WebhookDelivery,
} from '../../api/webhooks';
import { renderWebhooks, webhook } from './test-harness';

vi.mock('../../api/webhooks', async (original) => ({
  ...(await original<typeof import('../../api/webhooks')>()),
  listWebhookDeliveries: vi.fn(),
  getWebhookDelivery: vi.fn(),
  patchWebhook: vi.fn(),
  deleteWebhook: vi.fn(),
}));

const api = vi.mocked({ listWebhookDeliveries, getWebhookDelivery, patchWebhook, deleteWebhook });

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

beforeEach(() => {
  Object.defineProperty(navigator, 'clipboard', { value: { writeText: vi.fn(async () => {}) }, configurable: true });
  api.listWebhookDeliveries.mockResolvedValue({ deliveries: [], next_after: null });
});

const hook = webhook({
  name: 'github-acme',
  openings: [{ factory: 'f-software', repositories: ['acme/goal'] }, { factory: 'f-sre' }],
  triggers: [
    { factory: 'f-software', factory_name: 'software', trigger: 'check-fails' },
    { factory: 'f-sre', factory_name: 'sre', trigger: 'check-fails-anywhere' },
  ],
  pinged_at: '2026-11-03T08:55:00',
  setup_events: ['check_run', 'issues'],
});

const delivery = (over: Partial<WebhookDelivery> & { id: string }): WebhookDelivery => ({
  received_at: '2026-11-03T14:05:00',
  outcome: 'accepted',
  reason: '',
  item: null,
  event: 'check_run',
  receipt: 'accepted',
  duplicates: 0,
  evaluations: [],
  ...over,
});

const at = '/webhooks/github-acme';

describe('a webhook page: connection', () => {
  it('shows the address with Copy, the secret by name, the sender and GitHub’s ping', async () => {
    renderWebhooks({ at, list: [hook] });

    const connection = await screen.findByRole('region', { name: 'Connection' });
    expect(within(connection).getByText(`${window.location.origin}/hooks/id-github-acme`)).toBeTruthy();
    fireEvent.click(within(connection).getByRole('button', { name: 'Copy the address' }));
    await waitFor(() =>
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith(`${window.location.origin}/hooks/id-github-acme`),
    );
    const secret = within(connection).getAllByRole('link', { name: /webhook-github-acme/ })[0] as HTMLElement;
    expect(secret.getAttribute('href')).toBe('/o/acme/secrets/webhook-github-acme');
    expect(within(connection).getByRole('link', { name: 'Rotate' })).toBeTruthy();
    expect(within(connection).getByText('GitHub organization webhook')).toBeTruthy();
    expect(within(connection).getByRole('button', { name: 'About organization webhooks' })).toBeTruthy();
    expect(within(connection).getByText(/Received GitHub’s ping on Nov 3 at 08:55/)).toBeTruthy();
    expect(within(connection).getByText('Receiving')).toBeTruthy();
  });

  it('waits for GitHub’s ping until the API reports one', async () => {
    renderWebhooks({ at, list: [{ ...hook, pinged_at: null }] });

    expect(await screen.findByText('Waiting for GitHub’s ping')).toBeTruthy();
  });

  it('shows a token webhook’s header and paths, and no ping line', async () => {
    renderWebhooks({
      at: '/webhooks/datadog',
      list: [
        webhook({
          name: 'datadog',
          sender: 'token',
          token: { header: 'X-Alert-Token', event_path: 'event.type', id_path: 'id' },
        }),
      ],
    });

    const connection = await screen.findByRole('region', { name: 'Connection' });
    expect(within(connection).getByText('Header X-Alert-Token')).toBeTruthy();
    expect(within(connection).getByText('Event event.type')).toBeTruthy();
    expect(within(connection).queryByText(/ping/)).toBeNull();
  });
});

describe('a webhook page: openings, triggers and setup', () => {
  it('lists the openings, the triggers that use it and the setup steps with the events its triggers use', async () => {
    renderWebhooks({ at, list: [hook] });

    const open = await screen.findByRole('region', { name: 'Open to' });
    expect(within(open).getByText('acme/goal')).toBeTruthy();
    expect(within(open).getByText('All repositories')).toBeTruthy();
    expect(within(open).getByText('check-fails').closest('a')?.getAttribute('href')).toBe(
      '/o/acme/f/software/intake/check-fails',
    );
    expect(within(open).getByText('check-fails-anywhere')).toBeTruthy();

    const setup = screen.getByRole('region', { name: 'Set it up in GitHub' });
    expect(within(setup).getByText('check_run')).toBeTruthy();
    expect(within(setup).getByText('issues')).toBeTruthy();
  });

  it('asks the sender to send every event when a trigger names none', async () => {
    renderWebhooks({ at, list: [{ ...hook, setup_events: [], setup_all_events: true }] });

    const setup = await screen.findByRole('region', { name: 'Set it up in GitHub' });
    expect(within(setup).getByText('every event')).toBeTruthy();
  });

  it('opens it to another factory, limits it, closes one and renames it, in one request', async () => {
    api.patchWebhook.mockResolvedValue({ ...hook, name: 'github-acme-2' });
    const { router } = renderWebhooks({ at, list: [hook] });
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Edit' }));
    const dialog = await screen.findByRole('dialog', { name: 'Edit openings' });
    await user.clear(within(dialog).getByRole('textbox', { name: 'Name' }));
    await user.type(within(dialog).getByRole('textbox', { name: 'Name' }), 'github-acme-2');
    await user.click(within(dialog).getByRole('checkbox', { name: 'Support' }));
    await user.type(within(dialog).getByRole('textbox', { name: 'Repositories for Support' }), 'acme/web, acme/docs');
    await user.click(within(dialog).getByRole('checkbox', { name: 'SRE' }));
    await user.click(within(dialog).getByRole('button', { name: 'Save' }));

    await waitFor(() =>
      expect(api.patchWebhook).toHaveBeenCalledWith('org-1', 'github-acme', {
        name: 'github-acme-2',
        open: [{ factory: 'f-support', repositories: ['acme/web', 'acme/docs'] }],
        close: ['f-sre'],
      }),
    );
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/webhooks/github-acme-2'));
  });

  it('lists the triggers, each with its factory, when a close is refused with 409 webhook-in-use', async () => {
    api.patchWebhook.mockRejectedValue(
      new ApiError({
        status: 409,
        code: 'webhook-in-use',
        title: 'In use',
        triggers: [
          { factory: 'f-sre', factory_name: 'sre', trigger: 'check-fails-anywhere' },
          { factory: 'f-software', factory_name: 'software', trigger: 'check-fails' },
        ],
      } as never),
    );
    renderWebhooks({ at, list: [hook] });
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Edit' }));
    const dialog = await screen.findByRole('dialog', { name: 'Edit openings' });
    await user.click(within(dialog).getByRole('checkbox', { name: 'SRE' }));
    await user.click(within(dialog).getByRole('button', { name: 'Save' }));

    const alert = await within(dialog).findByText(/Published triggers still use this webhook/);
    const list = alert.closest('[role="alert"]') as HTMLElement;
    expect(within(list).getByText('check-fails-anywhere')).toBeTruthy();
    expect(within(list).getByText('SRE')).toBeTruthy();
    expect(within(list).getByText('Software')).toBeTruthy();
  });

  it('shows no Edit or Delete without webhook:manage', async () => {
    renderWebhooks({ at, list: [hook], can: ['webhook:read'] });

    await screen.findByRole('region', { name: 'Open to' });
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull();
  });

  it('deletes it after a confirmation, and returns to the list', async () => {
    api.deleteWebhook.mockResolvedValue();
    const { router } = renderWebhooks({ at, list: [hook] });
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Delete' }));
    await user.click(await screen.findByRole('button', { name: 'Delete webhook' }));

    await waitFor(() => expect(api.deleteWebhook).toHaveBeenCalledWith('org-1', 'github-acme'));
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/webhooks'));
  });
});

describe('a webhook page: the delivery log', () => {
  const rows = [
    delivery({
      id: 'd1',
      evaluations: [
        {
          factory: 'f-sre',
          factory_name: 'sre',
          state: 'done',
          outcome: 'created',
          reason: '',
          item: { id: 'i1', number: 88, ref: 'sre#88' },
        },
        {
          factory: 'f-software',
          factory_name: 'software',
          state: 'done',
          outcome: 'skipped',
          reason: 'not acme/goal',
          item: null,
        },
        {
          factory: 'f-support',
          factory_name: 'support',
          state: 'failed',
          outcome: 'failed',
          reason: 'no station',
          item: null,
        },
      ],
    }),
    delivery({ id: 'd2', received_at: '2026-11-03T13:41:00', receipt: 'duplicate', duplicates: 0 }),
    delivery({
      id: 'd3',
      received_at: '2026-11-03T09:12:00',
      event: 'push',
      outcome: 'refused',
      receipt: 'refused',
      reason: 'signature did not match',
    }),
    delivery({ id: 'd4', received_at: '2026-11-03T08:00:00', event: 'star' }),
    delivery({ id: 'd5', received_at: '2026-11-03T07:00:00', event: 'star' }),
  ];

  it('shows each receipt and, under it, what each factory made of the delivery', async () => {
    api.listWebhookDeliveries.mockResolvedValue({ deliveries: rows, next_after: null });
    renderWebhooks({ at, list: [hook] });

    const log = await screen.findByRole('region', { name: 'Deliveries' });
    await within(log).findByText('Created');
    const first = within(log).getAllByRole('listitem')[0] as HTMLElement;
    expect(within(first).getByText('Accepted')).toBeTruthy();
    expect(within(first).getByRole('link', { name: 'sre#88' }).getAttribute('href')).toBe('/o/acme/f/sre/items/88');
    expect(within(first).getByText('Skipped: not acme/goal')).toBeTruthy();
    expect(within(first).getByText('Failed: no station')).toBeTruthy();
    expect(within(log).getByText('Duplicate')).toBeTruthy();
    expect(within(log).getByText('Refused')).toBeTruthy();
    expect(within(log).getByText('signature did not match')).toBeTruthy();
    expect(within(log).getByText('2 more that no trigger names, counted only.')).toBeTruthy();
  });

  it('pages with next_after', async () => {
    api.listWebhookDeliveries
      .mockResolvedValueOnce({ deliveries: [rows[2] as WebhookDelivery], next_after: 'cursor-1' })
      .mockResolvedValueOnce({ deliveries: [rows[1] as WebhookDelivery], next_after: null });
    renderWebhooks({ at, list: [hook] });
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Show older' }));

    await waitFor(() => expect(screen.getByText('Duplicate')).toBeTruthy());
    expect(api.listWebhookDeliveries).toHaveBeenLastCalledWith(
      'org-1',
      'github-acme',
      { limit: 20, after: 'cursor-1' },
      expect.anything(),
    );
    expect(screen.queryByRole('button', { name: 'Show older' })).toBeNull();
  });

  it('opens a delivery’s headers and body for webhook:manage', async () => {
    api.listWebhookDeliveries.mockResolvedValue({ deliveries: [rows[2] as WebhookDelivery], next_after: null });
    api.getWebhookDelivery.mockResolvedValue({
      ...(rows[2] as WebhookDelivery),
      headers: { 'x-github-event': 'push' },
      body: '{"ref":"refs/heads/main"}',
      body_encoding: 'text',
    });
    renderWebhooks({ at, list: [hook] });
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: /View request/ }));

    const dialog = await screen.findByRole('dialog', { name: 'Request' });
    expect(await within(dialog).findByText('{"ref":"refs/heads/main"}')).toBeTruthy();
    expect(within(dialog).getByText('x-github-event')).toBeTruthy();
    expect(api.getWebhookDelivery).toHaveBeenCalledWith('org-1', 'github-acme', 'd3', expect.anything());
  });

  it('offers no way to open a body without webhook:manage', async () => {
    api.listWebhookDeliveries.mockResolvedValue({ deliveries: rows, next_after: null });
    renderWebhooks({ at, list: [hook], can: ['webhook:read'] });

    const log = await screen.findByRole('region', { name: 'Deliveries' });
    await within(log).findByText('Refused');
    expect(screen.queryByRole('button', { name: /View request/ })).toBeNull();
    expect(screen.queryByText(/Headers/)).toBeNull();
    expect(api.getWebhookDelivery).not.toHaveBeenCalled();
  });

  it('says it could not load the log, and keeps the rest of the page', async () => {
    api.listWebhookDeliveries.mockRejectedValue(new Error('down'));
    renderWebhooks({ at, list: [hook] });

    expect(await screen.findByText(/Couldn’t load the deliveries/)).toBeTruthy();
    expect(screen.getByRole('region', { name: 'Connection' })).toBeTruthy();
  });
});
