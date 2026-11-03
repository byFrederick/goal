// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../api/client';
import { createSecret } from '../../api/secrets';
import { iso, leaks, NOW, renderSecrets, secret } from './test-harness';

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock('../../api/secrets', () => ({
  listSecrets: vi.fn(async () => []),
  getSecret: vi.fn(),
  listSecretActivity: vi.fn(),
  createSecret: vi.fn(async (_org: string, body: { name: string }) => ({ ...secret({ name: body.name }) })),
  importSecrets: vi.fn(),
  patchSecret: vi.fn(),
  replaceSecretValue: vi.fn(),
  deleteSecret: vi.fn(),
}));

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(NOW);
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.clearAllMocks();
});

const VALUE = 'sntrys_super-secret-9876';

/** Opens the sheet from the list's New secret button. */
async function openSheet(over: Parameters<typeof renderSecrets>[0] = {}) {
  const view = renderSecrets({ list: [secret({ name: 'github-token' })], ...over });
  fireEvent.click((await screen.findAllByRole('button', { name: /New secret/ }))[0] as HTMLElement);
  const sheet = await screen.findByRole('dialog', { name: 'New secret' });
  return { ...view, sheet };
}

describe('the New secret sheet', () => {
  it('opens with a pasted, write-only value and no source chooser', async () => {
    const { sheet } = await openSheet();

    expect(within(sheet).getByLabelText('Name')).toBeTruthy();
    expect((within(sheet).getByLabelText('Value') as HTMLInputElement).type).toBe('password');
    expect(within(sheet).getByText('Write-only')).toBeTruthy();
    expect(within(sheet).getByLabelText(/Description/)).toBeTruthy();
    expect(within(sheet).getByRole('button', { name: /Software/ })).toBeTruthy();
    expect(within(sheet).getByLabelText(/Expires/)).toBeTruthy();
    expect(within(sheet).queryByText(/source/i)).toBeNull();
  });

  it('asks for a valid name and a value before it sends anything', async () => {
    const { sheet } = await openSheet();

    fireEvent.change(within(sheet).getByLabelText('Name'), { target: { value: 'Bad_Name' } });
    fireEvent.click(within(sheet).getByRole('button', { name: 'Add secret' }));

    expect(await within(sheet).findByText(/3 to 40 lowercase letters/)).toBeTruthy();
    expect(within(sheet).getByText('Paste the value.')).toBeTruthy();
    expect(createSecret).not.toHaveBeenCalled();
  });

  it('sends the name, value, scope and expiry, closes, and keeps the value out of the page, the URL and both caches', async () => {
    const view = await openSheet();
    const { sheet } = view;

    fireEvent.change(within(sheet).getByLabelText('Name'), { target: { value: 'sentry-token' } });
    fireEvent.change(within(sheet).getByLabelText('Value'), { target: { value: VALUE } });
    fireEvent.click(within(sheet).getByRole('button', { name: /SRE/ }));
    fireEvent.change(within(sheet).getByLabelText(/Expires/), { target: { value: '2027-11-02' } });
    fireEvent.click(within(sheet).getByRole('button', { name: 'Add secret' }));

    await waitFor(() => expect(createSecret).toHaveBeenCalledTimes(1));
    expect(createSecret).toHaveBeenCalledWith('org-1', {
      name: 'sentry-token',
      value: VALUE,
      description: '',
      scope: { all_factories: false, factories: ['f-sre'] },
      expires_at: iso(Date.parse('2027-11-02T00:00:00Z')),
      remind_days: 14,
    });
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'New secret' })).toBeNull());
    expect(view.router.state.location.search).not.toHaveProperty('new');
    expect(leaks(VALUE, view)).toEqual([]);
  });

  it('sends All factories as the scope', async () => {
    const { sheet } = await openSheet();

    fireEvent.change(within(sheet).getByLabelText('Name'), { target: { value: 'linear-key' } });
    fireEvent.change(within(sheet).getByLabelText('Value'), { target: { value: VALUE } });
    fireEvent.click(within(sheet).getByRole('checkbox', { name: /All factories/ }));
    fireEvent.click(within(sheet).getByRole('button', { name: 'Add secret' }));

    await waitFor(() => expect(createSecret).toHaveBeenCalled());
    expect(vi.mocked(createSecret).mock.calls[0]?.[1].scope).toEqual({ all_factories: true, factories: [] });
  });

  it('shows goald’s detail on the form for a 409 and keeps the sheet open', async () => {
    vi.mocked(createSecret).mockRejectedValueOnce(
      new ApiError({
        title: 'Conflict',
        status: 409,
        detail: 'secret "sentry-token" already exists',
        code: 'secret-name-taken',
      }),
    );
    const { sheet } = await openSheet();

    fireEvent.change(within(sheet).getByLabelText('Name'), { target: { value: 'sentry-token' } });
    fireEvent.change(within(sheet).getByLabelText('Value'), { target: { value: VALUE } });
    fireEvent.click(within(sheet).getByRole('button', { name: 'Add secret' }));

    expect(await within(sheet).findByRole('alert')).toBeTruthy();
    expect(within(sheet).getByText('secret "sentry-token" already exists')).toBeTruthy();
    expect(screen.getByRole('dialog', { name: 'New secret' })).toBeTruthy();
  });

  it('is not offered to those who cannot manage secrets, even by its URL', async () => {
    renderSecrets({ list: [secret({ name: 'github-token' })], at: '/secrets?new=true', can: ['secret:read'] });

    await screen.findByRole('table');
    expect(screen.queryByRole('dialog', { name: 'New secret' })).toBeNull();
  });
});
