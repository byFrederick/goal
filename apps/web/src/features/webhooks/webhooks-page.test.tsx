// @vitest-environment jsdom
import type { QueryClient } from '@tanstack/react-query';
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { useState } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../api/client';
import { webhooksQuery } from '../../api/queries';
import type { Factory, OrgSummary } from '../../api/types';
import {
  createWebhookEndpoint,
  getWebhookDelivery,
  listWebhookDeliveries,
  listWebhookEndpoints,
  rotateWebhookSecret,
  type WebhookDelivery,
  type WebhookDeliveryDay,
  type WebhookDeliveryPage,
  type WebhookDeliveryRequest,
  type WebhookEndpoint,
} from '../../api/webhooks';
import { TopBar, TopBarSetContext, type TopBarState } from '../../app/top-bar';
import { renderFactoryPage, type Seed } from '../../test/route';
import { WebhooksPage } from './webhooks-page';

// The API module is replaced whole: the page's reads and writes are what each test asserts. The one
// secret a test sees is an all-zero fixture, never a real value.
vi.mock('../../api/webhooks', async (original) => ({
  ...(await original<typeof import('../../api/webhooks')>()),
  listWebhookEndpoints: vi.fn(),
  listWebhookDeliveries: vi.fn(),
  getWebhookDelivery: vi.fn(),
  createWebhookEndpoint: vi.fn(),
  rotateWebhookSecret: vi.fn(),
}));

const api = vi.mocked({
  listWebhookEndpoints,
  listWebhookDeliveries,
  getWebhookDelivery,
  createWebhookEndpoint,
  rotateWebhookSecret,
});

const zeros = '0'.repeat(64);
const ones = '1'.repeat(64);

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.restoreAllMocks();
});

beforeEach(() => {
  Object.defineProperty(navigator, 'clipboard', { value: { writeText: vi.fn(async () => {}) }, configurable: true });
});

const acme: OrgSummary = {
  id: 'org-1',
  slug: 'acme',
  name: 'Acme',
  mark: 'Ac',
  factory_count: 1,
  role: 'owner',
};

const software: Factory = {
  id: 'software-id',
  org_id: 'org-1',
  name: 'software',
  work_types: [],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'editor',
  can: ['factory:read', 'webhook:manage'],
};

const viewer: Factory = { ...software, role: 'viewer', can: ['factory:read'] };

/** Seven UTC days, oldest first; the picture's cf-deploys week has 7 delivered and 2 refused. */
function week(delivered: number[], refused: number[]): WebhookDeliveryDay[] {
  return delivered.map((d, i) => ({ day: `2026-10-${21 + i}`, delivered: d, refused: refused[i] ?? 0 }));
}

const cfDeploys: WebhookEndpoint = {
  id: 'cf-endpoint',
  name: 'cf-deploys',
  kind: 'generic',
  address: '/hooks/cf-endpoint',
  secret_name: 'cf-deploys-hook-secret',
  created_at: '2026-10-01T00:00:00Z',
  rotated_at: null,
  triggers: ['Deploy failed'],
  days: week([1, 2, 1, 1, 0, 1, 1], [0, 1, 0, 0, 0, 1, 0]),
};

const uptime: WebhookEndpoint = {
  id: 'uptime-endpoint',
  name: 'uptime',
  kind: 'generic',
  address: '/hooks/uptime-endpoint',
  secret_name: 'uptime-hook-secret',
  created_at: '2026-10-01T00:00:00Z',
  rotated_at: '2026-10-20T09:00:00Z',
  triggers: [],
  days: week([0, 1, 0, 2, 0, 0, 0], [0, 0, 0, 0, 0, 0, 0]),
};

const refused: WebhookDelivery = {
  id: 'd-refused',
  received_at: '2026-10-25T22:14:00Z',
  outcome: 'refused',
  reason: 'signature did not match',
  item: null,
};
const accepted: WebhookDelivery = {
  id: 'd-accepted',
  received_at: '2026-10-25T18:02:00Z',
  outcome: 'accepted',
  reason: '',
  item: null,
};
const created: WebhookDelivery = {
  id: 'd-created',
  received_at: '2026-10-24T11:40:00Z',
  outcome: 'created',
  reason: '',
  item: { id: 'item-131', number: 131 },
};

const page = (deliveries: WebhookDelivery[]): WebhookDeliveryPage => ({ deliveries, next_after: null });

const request: WebhookDeliveryRequest = {
  ...accepted,
  headers: { 'content-type': 'application/json', 'user-agent': 'example-sender/1' },
  body: '{"event":"deploy.succeeded","service":"goal-api","commit":"a41f9c2"}',
  body_encoding: 'text',
};

/** The endpoints and their deliveries, each read the way the API answers. */
function answers() {
  api.listWebhookEndpoints.mockResolvedValue([cfDeploys, uptime]);
  api.listWebhookDeliveries.mockImplementation(async (_org, _factory, endpoint) =>
    endpoint === 'cf-endpoint' ? page([refused, accepted, created]) : page([accepted, accepted, refused]),
  );
  api.getWebhookDelivery.mockResolvedValue(request);
}

function seeds(endpoints: WebhookEndpoint[] = [cfDeploys, uptime]): Seed[] {
  return [[webhooksQuery(acme.id, software.id).queryKey, endpoints]];
}

/** The page under the shell's top bar, which is where New webhook sits. */
function WithTopBar() {
  const [state, setState] = useState<TopBarState | null>(null);
  return (
    <TopBarSetContext.Provider value={setState}>
      <TopBar state={state} />
      <WebhooksPage />
    </TopBarSetContext.Provider>
  );
}

function renderPage(factory: Factory = software, seed: Seed[] = seeds()) {
  return renderFactoryPage({
    page: () => <WithTopBar />,
    path: 'intake/webhooks',
    org: acme,
    factory,
    seed,
  });
}

/** Whether the secret is in the DOM or in any query's data. */
function secretIsKept(queryClient: QueryClient, secret: string): boolean {
  const inCache = queryClient
    .getQueryCache()
    .getAll()
    .some((query) => JSON.stringify(query.state.data ?? null).includes(secret));
  return inCache || (document.body.textContent?.includes(secret) ?? false);
}

describe('the Webhooks page states', () => {
  it('says it is loading, with aria-busy and no placeholder shape', async () => {
    api.listWebhookEndpoints.mockReturnValue(new Promise(() => {}));
    renderPage(software, []);

    expect(await screen.findByText('Loading webhooks')).toBeTruthy();
    expect(document.querySelector('[aria-busy="true"]')).not.toBeNull();
  });

  it('says the list could not load, and retries it', async () => {
    api.listWebhookEndpoints.mockRejectedValueOnce(new Error('network'));
    api.listWebhookEndpoints.mockResolvedValueOnce([cfDeploys]);
    answers();
    renderPage(software, []);

    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('Couldn’t load the webhooks');
    fireEvent.click(within(alert).getByRole('button', { name: 'Try again' }));

    expect(await screen.findByRole('heading', { name: 'cf-deploys' })).toBeTruthy();
  });

  it('shows the empty state with New webhook when the factory has no endpoints', async () => {
    renderPage(software, seeds([]));

    expect(await screen.findByText('No webhooks yet')).toBeTruthy();
    expect(screen.getAllByRole('button', { name: 'New webhook' }).length).toBeGreaterThan(0);
  });

  it('keeps New webhook out of the empty state for someone who cannot manage webhooks', async () => {
    renderPage(viewer, seeds([]));

    expect(await screen.findByText('No webhooks yet')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'New webhook' })).toBeNull();
  });

  it('shows each endpoint card, the test panel and the newest deliveries when ready', async () => {
    answers();
    renderPage();

    const card = await screen.findByRole('region', { name: 'cf-deploys' });
    expect(within(card).getByText('2 refused this week')).toBeTruthy();
    expect(within(card).getByText('HMAC SHA-256')).toBeTruthy();
    expect(within(card).getByText('cf-deploys-hook-secret')).toBeTruthy();
    expect(within(card).getByText('Deploy failed')).toBeTruthy();
    expect(within(card).getByText('7 delivered, 2 refused')).toBeTruthy();

    expect(await within(card).findByText('Refused: signature did not match')).toBeTruthy();
    expect(within(card).getByText('Accepted, no trigger matched')).toBeTruthy();
    expect(within(card).getByRole('link', { name: '#131' }).getAttribute('href')).toBe('/o/acme/f/software/items/131');
    expect(await within(card).findAllByRole('button', { name: /View request/ })).toHaveLength(3);

    expect(screen.getByRole('complementary', { name: 'Send a test' })).toBeTruthy();
  });

  it('says what an endpoint with no trigger feeds, with a way to create one', async () => {
    answers();
    renderPage();

    const card = await screen.findByRole('region', { name: 'uptime' });
    expect(within(card).getByText('Nothing. 3 deliveries were kept but created no items.')).toBeTruthy();
    const create = within(card).getByRole('link', { name: 'Create a trigger' });
    expect(create.getAttribute('href')).toBe('/o/acme/f/software/intake/new');
    expect(within(card).queryByText(/refused this week/)).toBeNull();
  });
});

describe('an endpoint card', () => {
  it('copies the full address the sender posts to', async () => {
    answers();
    renderPage();

    const card = await screen.findByRole('region', { name: 'cf-deploys' });
    fireEvent.click(within(card).getByRole('button', { name: 'Copy' }));

    const url = `${window.location.origin}/hooks/cf-endpoint`;
    await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalledWith(url));
    expect(within(card).getByText(url)).toBeTruthy();
  });

  it('opens a request with its headers and its body', async () => {
    answers();
    renderPage();

    const card = await screen.findByRole('region', { name: 'cf-deploys' });
    fireEvent.click((await within(card).findAllByRole('button', { name: /View request/ }))[1] as HTMLElement);

    const dialog = await screen.findByRole('dialog', { name: 'Request' });
    expect(within(dialog).getByText('user-agent')).toBeTruthy();
    expect(within(dialog).getByText('example-sender/1')).toBeTruthy();
    expect(within(dialog).getByText(/"commit":"a41f9c2"/)).toBeTruthy();
    expect(api.getWebhookDelivery).toHaveBeenCalledWith(
      'org-1',
      'software-id',
      'cf-endpoint',
      'd-accepted',
      expect.anything(),
    );
  });

  it('labels a body that is not UTF-8 text as base64', async () => {
    answers();
    api.getWebhookDelivery.mockResolvedValue({ ...request, body: '/wD/', body_encoding: 'base64' });
    renderPage();

    const card = await screen.findByRole('region', { name: 'cf-deploys' });
    fireEvent.click((await within(card).findAllByRole('button', { name: /View request/ }))[0] as HTMLElement);

    const dialog = await screen.findByRole('dialog', { name: 'Request' });
    expect(await within(dialog).findByText('Not UTF-8 text: shown as base64.')).toBeTruthy();
    expect(within(dialog).getByText('/wD/')).toBeTruthy();
  });
});

describe('the Send a test panel', () => {
  it('shows a curl sample for the first address with its signature header, and the fields the trigger can read', async () => {
    answers();
    renderPage();

    const panel = await screen.findByRole('complementary', { name: 'Send a test' });
    const sample = panel.querySelector('pre')?.textContent ?? '';
    expect(sample).toContain('X-Goal-Signature');
    expect(sample).toContain('/hooks/cf-endpoint');
    for (const field of ['event', 'service', 'commit']) {
      expect(await within(panel).findByText(field)).toBeTruthy();
    }
    expect(within(panel).getByText('Fields reach the agent as untrusted data, never as instructions.')).toBeTruthy();
  });

  it('names the sample with its kind’s header for a GitHub endpoint', async () => {
    answers();
    renderPage(software, seeds([{ ...cfDeploys, kind: 'github' }, uptime]));

    const panel = await screen.findByRole('complementary', { name: 'Send a test' });
    expect(panel.querySelector('pre')?.textContent).toContain('X-Hub-Signature-256');
  });

  it('keeps Send a sample delivery disabled, with the not-available tip', async () => {
    answers();
    renderPage();

    const panel = await screen.findByRole('complementary', { name: 'Send a test' });
    const send = within(panel).getByRole('button', { name: 'Send a sample delivery' });
    expect(send.getAttribute('aria-disabled')).toBe('true');
    fireEvent.click(send);
    expect(api.createWebhookEndpoint).not.toHaveBeenCalled();
  });
});

describe('New webhook', () => {
  it('creates the endpoint, shows its secret once and reads the list again', async () => {
    answers();
    api.createWebhookEndpoint.mockResolvedValue({
      ...uptime,
      id: 'new-endpoint',
      name: 'github-ci',
      kind: 'github',
      secret: zeros,
    });
    const { queryClient } = renderPage();
    await screen.findByRole('region', { name: 'cf-deploys' });
    const reads = api.listWebhookEndpoints.mock.calls.length;

    fireEvent.click(screen.getByRole('button', { name: 'New webhook' }));
    fireEvent.change(screen.getByRole('textbox', { name: 'Name' }), { target: { value: 'github-ci' } });
    fireEvent.click(screen.getByRole('radio', { name: 'GitHub' }));
    fireEvent.click(screen.getByRole('button', { name: 'Create webhook' }));

    const dialog = await screen.findByRole('dialog', { name: 'Webhook created' });
    expect(within(dialog).getByText(zeros)).toBeTruthy();
    expect(api.createWebhookEndpoint).toHaveBeenCalledWith('org-1', 'software-id', {
      name: 'github-ci',
      kind: 'github',
    });

    fireEvent.click(within(dialog).getByRole('button', { name: 'Done' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(document.body.textContent).not.toContain(zeros);
    expect(secretIsKept(queryClient, zeros)).toBe(false);
    await waitFor(() => expect(api.listWebhookEndpoints.mock.calls.length).toBeGreaterThan(reads));
  });

  it('says a name that is taken, without opening the secret', async () => {
    answers();
    api.createWebhookEndpoint.mockRejectedValue(
      new ApiError({ status: 409, title: 'Conflict', code: 'webhook-name-taken', detail: 'taken' } as never),
    );
    renderPage();
    await screen.findByRole('region', { name: 'cf-deploys' });

    fireEvent.click(screen.getByRole('button', { name: 'New webhook' }));
    fireEvent.change(screen.getByRole('textbox', { name: 'Name' }), { target: { value: 'cf-deploys' } });
    fireEvent.click(screen.getByRole('button', { name: 'Create webhook' }));

    expect(await screen.findByText('Another webhook in this factory has this name.')).toBeTruthy();
    expect(screen.queryByRole('dialog', { name: 'Webhook created' })).toBeNull();
  });

  it('asks for a name a slug can hold before it sends the form', async () => {
    answers();
    renderPage();
    await screen.findByRole('region', { name: 'cf-deploys' });

    fireEvent.click(screen.getByRole('button', { name: 'New webhook' }));
    fireEvent.change(screen.getByRole('textbox', { name: 'Name' }), { target: { value: 'Cloudflare Deploys' } });
    fireEvent.click(screen.getByRole('button', { name: 'Create webhook' }));

    expect(await screen.findByText('Use lower-case letters, digits and single hyphens')).toBeTruthy();
    expect(api.createWebhookEndpoint).not.toHaveBeenCalled();
  });
});

describe('Rotate', () => {
  it('rotates after a confirmation, shows the new secret once and keeps it out of the cache', async () => {
    answers();
    api.rotateWebhookSecret.mockResolvedValue({ ...cfDeploys, secret: ones, rotated_at: '2026-10-27T10:00:00Z' });
    const { queryClient } = renderPage();
    const card = await screen.findByRole('region', { name: 'cf-deploys' });
    const reads = api.listWebhookEndpoints.mock.calls.length;

    fireEvent.click(within(card).getByRole('button', { name: 'Rotate' }));
    const confirm = await screen.findByRole('dialog', { name: /Rotate the signing secret/ });
    fireEvent.click(within(confirm).getByRole('button', { name: 'Rotate secret' }));

    const dialog = await screen.findByRole('dialog', { name: 'Secret rotated' });
    expect(within(dialog).getByText(ones)).toBeTruthy();
    expect(api.rotateWebhookSecret).toHaveBeenCalledWith('org-1', 'software-id', 'cf-endpoint');

    fireEvent.click(within(dialog).getByRole('button', { name: 'Done' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(document.body.textContent).not.toContain(ones);
    expect(secretIsKept(queryClient, ones)).toBe(false);
    await waitFor(() => expect(api.listWebhookEndpoints.mock.calls.length).toBeGreaterThan(reads));
  });

  it('copies the one-time secret from its dialog', async () => {
    answers();
    api.rotateWebhookSecret.mockResolvedValue({ ...cfDeploys, secret: ones });
    renderPage();
    const card = await screen.findByRole('region', { name: 'cf-deploys' });

    fireEvent.click(within(card).getByRole('button', { name: 'Rotate' }));
    fireEvent.click(
      within(await screen.findByRole('dialog', { name: /Rotate the signing secret/ })).getByRole('button', {
        name: 'Rotate secret',
      }),
    );
    const dialog = await screen.findByRole('dialog', { name: 'Secret rotated' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Copy' }));

    await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalledWith(ones));
  });

  it('does not offer Rotate to someone who cannot manage webhooks', async () => {
    answers();
    renderPage(viewer);

    const card = await screen.findByRole('region', { name: 'cf-deploys' });
    expect(within(card).queryByRole('button', { name: 'Rotate' })).toBeNull();
  });
});
