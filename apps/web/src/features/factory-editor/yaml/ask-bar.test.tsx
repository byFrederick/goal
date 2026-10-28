// @vitest-environment jsdom
import type { Problem } from '@goal/api-client';
import { cleanup, fireEvent, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../../api/client';
import { createDraftRequest, listDraftRequests } from '../../../api/definitions';
import { draftRequestQuery, draftRequestsQuery } from '../../../api/queries';
import type { DraftRequest, Factory, OrgSummary } from '../../../api/types';
import { renderFactoryPage, type Seed } from '../../../test/route';
import { AskBar } from './ask-bar';

vi.mock('../../../api/definitions', async (original) => ({
  ...(await original<typeof import('../../../api/definitions')>()),
  createDraftRequest: vi.fn(),
  listDraftRequests: vi.fn(),
}));

afterEach(cleanup);
beforeEach(() => {
  vi.mocked(createDraftRequest).mockReset();
  vi.mocked(listDraftRequests).mockReset();
  vi.mocked(listDraftRequests).mockResolvedValue({ requests: [] });
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: 'owner' };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'goal',
  title: 'Goal',
  work_types: ['feature'],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'editor',
  can: ['definition:edit'],
};

function request(overrides: Partial<DraftRequest>): DraftRequest {
  return {
    id: 'request-1',
    text: 'give Review a 4th reviewer',
    agent: '',
    status: 'queued',
    summary: '',
    changed_paths: [],
    cost_usd: 0,
    turns: 0,
    error: '',
    requester: { kind: 'user', id: 'local', name: 'Local user' },
    created_at: '2026-10-28T09:00:00Z',
    ...overrides,
  } as DraftRequest;
}

/** The bar on the definition screen, with the requests it reads as query data (so no request leaves the test). */
async function renderBar({ requests = [], current }: { requests?: DraftRequest[]; current?: DraftRequest } = {}) {
  const seed: Seed[] = [[draftRequestsQuery(org.id, factory.id).queryKey, { requests }]];
  if (current) seed.push([draftRequestQuery(org.id, factory.id, current.id).queryKey, current]);
  const rendered = renderFactoryPage({
    page: () => <AskBar />,
    path: 'definition/line',
    org,
    factory,
    seed,
    search: current ? { request: current.id } : undefined,
  });
  // The router mounts the page after its match: the bar is there once its input is.
  await screen.findByRole('textbox', { name: 'Ask the factory to change its definition' });
  return rendered;
}

const input = () => screen.getByRole('textbox', { name: 'Ask the factory to change its definition' });
const ask = () => screen.getByRole('button', { name: 'Ask' });

describe('the Ask bar, ready', () => {
  it('has the label and placeholder the screen shows, with Ask off until there is text', async () => {
    await renderBar();
    expect(input().getAttribute('placeholder')).toBe(
      'Ask the factory to change it, for example: give Review a 4th reviewer',
    );
    expect((input() as HTMLInputElement).disabled).toBe(false);
    expect((ask() as HTMLButtonElement).disabled).toBe(true);

    fireEvent.change(input(), { target: { value: 'give Review a 4th reviewer' } });
    expect((ask() as HTMLButtonElement).disabled).toBe(false);
  });

  it('sends the trimmed text as a request, and the bar then shows it queued with the input disabled', async () => {
    const created = request({ id: 'request-2', text: 'give Review a 4th reviewer' });
    vi.mocked(createDraftRequest).mockResolvedValue(created);
    vi.mocked(listDraftRequests).mockResolvedValue({ requests: [created] });
    await renderBar();

    fireEvent.change(input(), { target: { value: '  give Review a 4th reviewer  ' } });
    fireEvent.click(ask());

    expect(await screen.findByText('Queued')).toBeTruthy();
    expect(createDraftRequest).toHaveBeenCalledWith('org-1', 'factory-1', { text: 'give Review a 4th reviewer' });
    expect((input() as HTMLInputElement).disabled).toBe(true);
    expect((ask() as HTMLButtonElement).disabled).toBe(true);
  });

  it('shows a request that is running, with the input disabled', async () => {
    await renderBar({ requests: [request({ status: 'running' })] });
    expect(await screen.findByText('Running')).toBeTruthy();
    expect((input() as HTMLInputElement).disabled).toBe(true);
  });

  it('shows the summary of a done request, and the input is open again', async () => {
    await renderBar({
      requests: [request({ status: 'done', summary: 'Added a fourth reviewer to Review.' })],
    });
    expect(await screen.findByText('Done')).toBeTruthy();
    expect(screen.getByText('Added a fourth reviewer to Review.')).toBeTruthy();
    expect((input() as HTMLInputElement).disabled).toBe(false);
  });

  it('shows the error of a failed request', async () => {
    await renderBar({
      requests: [request({ status: 'failed', error: 'The agent stopped before it changed the draft.' })],
    });
    expect(await screen.findByText('The agent stopped before it changed the draft.')).toBeTruthy();
    expect((input() as HTMLInputElement).disabled).toBe(false);
  });

  it('shows the request the URL names, even while the newest one runs and keeps the input off', async () => {
    const named = request({ id: 'request-old', text: 'rename Build', status: 'done', summary: 'Renamed Build.' });
    await renderBar({ requests: [request({ id: 'request-new', status: 'running' })], current: named });
    expect(await screen.findByText('rename Build')).toBeTruthy();
    expect(screen.getByText('Renamed Build.')).toBeTruthy();
    expect((input() as HTMLInputElement).disabled).toBe(true);
  });

  it('reads a draft request query without a refetch interval: the stream refreshes it', () => {
    expect('refetchInterval' in draftRequestsQuery(org.id, factory.id)).toBe(false);
    expect('refetchInterval' in draftRequestQuery(org.id, factory.id, 'request-1')).toBe(false);
  });
});

describe('the Ask bar, refusals and loading', () => {
  it('says a request is already running when goald answers 409 draft-request-open', async () => {
    vi.mocked(createDraftRequest).mockRejectedValue(
      new ApiError({ title: 'Conflict', status: 409, code: 'draft-request-open' } as unknown as Problem),
    );
    await renderBar();
    fireEvent.change(input(), { target: { value: 'give Review a 4th reviewer' } });
    fireEvent.click(ask());

    expect(await screen.findByText(/A request is already running/)).toBeTruthy();
  });

  it('shows the detail of another refusal, such as a factory.yaml that does not parse', async () => {
    vi.mocked(createDraftRequest).mockRejectedValue(
      new ApiError({
        title: 'Conflict',
        status: 409,
        code: 'yaml-invalid',
        detail: 'factory.yaml does not parse at line 3',
      } as unknown as Problem),
    );
    await renderBar();
    fireEvent.change(input(), { target: { value: 'give Review a 4th reviewer' } });
    fireEvent.click(ask());

    expect(await screen.findByText('Not asked: factory.yaml does not parse at line 3')).toBeTruthy();
    expect(screen.queryByText(/already running/)).toBeNull();
  });

  it('is loading until the requests are read: an sr-only Loading, aria-busy, and the input off', async () => {
    vi.mocked(listDraftRequests).mockReturnValue(new Promise(() => {}));
    const { container } = renderFactoryPage({
      page: () => <AskBar />,
      path: 'definition/line',
      org,
      factory,
    });
    await screen.findByRole('textbox', { name: 'Ask the factory to change its definition' });
    expect(screen.getByText('Loading')).toBeTruthy();
    expect(container.querySelector('[aria-busy="true"]')).not.toBeNull();
    expect((input() as HTMLInputElement).disabled).toBe(true);
  });

  it('says the requests could not be read when the read fails', async () => {
    vi.mocked(listDraftRequests).mockRejectedValue(new Error('network'));
    renderFactoryPage({ page: () => <AskBar />, path: 'definition/line', org, factory });
    await screen.findByRole('textbox', { name: 'Ask the factory to change its definition' });
    expect(await screen.findByText(/could not be read/)).toBeTruthy();
  });
});
