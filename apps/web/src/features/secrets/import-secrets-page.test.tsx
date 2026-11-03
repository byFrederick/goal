// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../api/client';
import { importSecrets } from '../../api/secrets';
import type { SecretImport } from '../../api/types';
import { leaks, NOW, renderSecrets, secret } from './test-harness';

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock('../../api/secrets', () => ({
  listSecrets: vi.fn(async () => []),
  getSecret: vi.fn(),
  listSecretActivity: vi.fn(),
  createSecret: vi.fn(),
  importSecrets: vi.fn(),
  patchSecret: vi.fn(),
  replaceSecretValue: vi.fn(),
  deleteSecret: vi.fn(),
}));

const TEXT = 'SENTRY_TOKEN=sntrys_value-1111\nLINEAR_API_KEY=lin_value-2222\nDD_API_KEY=dd_value-3333';

/** What the API answers: sentry-token and dd-api-key exist, linear-api-key is new. */
const answer = (body: SecretImport) => {
  const uses = (n: number) => Array.from({ length: n }, (_, i) => ({ kind: 'definition' as const, name: `f${i}` }));
  const names = [
    { name: 'sentry-token', exists: true, uses: uses(1) },
    { name: 'linear-api-key', exists: false, uses: [] },
    { name: 'dd-api-key', exists: true, uses: uses(2) },
  ];
  return {
    dry_run: body.dry_run ?? false,
    secrets: names.map((n) => ({
      ...n,
      ...(body.dry_run
        ? {}
        : {
            outcome: !n.exists
              ? ('created' as const)
              : body.on_conflict === 'replace'
                ? ('replaced' as const)
                : ('skipped' as const),
          }),
    })),
  };
};

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(NOW);
  vi.mocked(importSecrets).mockImplementation(async (_org, body) => answer(body));
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.clearAllMocks();
});

/** Pastes `text`; the dry run follows after its 300 ms. */
async function paste(text = TEXT) {
  fireEvent.change(await screen.findByLabelText('Paste .env or YAML'), { target: { value: text } });
}

describe('Import secrets', () => {
  it('runs a dry run after the text settles and lists each name as New or Already exists, with its uses', async () => {
    renderSecrets({ at: '/secrets/import' });
    await paste();

    await screen.findByText('3 found, 1 new, 2 already exist');
    expect(importSecrets).toHaveBeenCalledTimes(1);
    expect(importSecrets).toHaveBeenCalledWith('org-1', { format: 'env', text: TEXT, dry_run: true });
    const happens = screen.getByRole('region', { name: 'What happens' });
    expect(within(happens).getByText('New')).toBeTruthy();
    expect(within(happens).getAllByText('Already exists')).toHaveLength(2);
    expect(within(happens).getByText(/Used in 2 places/)).toBeTruthy();
    expect(within(happens).getByText('SENTRY_TOKEN')).toBeTruthy();
  });

  it('reads a YAML secrets map as YAML', async () => {
    renderSecrets({ at: '/secrets/import' });
    await paste('secrets:\n  sentry-token: x');

    await waitFor(() => expect(importSecrets).toHaveBeenCalled());
    expect(vi.mocked(importSecrets).mock.calls[0]?.[1].format).toBe('yaml');
  });

  it('skips existing names by default, and Replace counts them in', async () => {
    renderSecrets({ at: '/secrets/import' });
    await paste();

    expect(await screen.findByRole('button', { name: 'Import 1' })).toBeTruthy();
    expect((screen.getByRole('radio', { name: 'Skip' }) as HTMLInputElement).checked).toBe(true);
    fireEvent.click(screen.getByRole('radio', { name: 'Replace' }));
    expect(await screen.findByRole('button', { name: 'Import 3' })).toBeTruthy();
  });

  it('imports with the chosen conflict rule, returns to the list, and keeps the values out of the page, the URL and both caches', async () => {
    const view = renderSecrets({ at: '/secrets/import', list: [secret({ name: 'sentry-token' })] });
    await paste();
    fireEvent.click(await screen.findByRole('radio', { name: 'Replace' }));

    fireEvent.click(await screen.findByRole('button', { name: 'Import 3' }));

    await waitFor(() =>
      expect(importSecrets).toHaveBeenLastCalledWith('org-1', {
        format: 'env',
        text: TEXT,
        on_conflict: 'replace',
        dry_run: false,
      }),
    );
    await waitFor(() => expect(view.router.state.location.pathname).toBe('/o/acme/secrets'));
    for (const value of ['sntrys_value-1111', 'lin_value-2222', 'dd_value-3333']) {
      expect(leaks(value, view)).toEqual([]);
    }
  });

  it('shows goald’s detail when the text does not parse, and offers nothing to import', async () => {
    vi.mocked(importSecrets).mockRejectedValue(
      new ApiError({ title: 'Bad Request', status: 400, detail: 'the import is empty', code: 'secret-invalid' }),
    );
    renderSecrets({ at: '/secrets/import' });
    await paste('nonsense');

    expect(await screen.findByText('the import is empty')).toBeTruthy();
    expect((screen.getByRole('button', { name: 'Import 0' }) as HTMLButtonElement).disabled).toBe(true);
  });

  it('reads a dropped file into the text', async () => {
    renderSecrets({ at: '/secrets/import' });
    const area = await screen.findByLabelText('Paste .env or YAML');

    fireEvent.drop(area, { dataTransfer: { files: [{ text: async () => TEXT }] } });

    await waitFor(() => expect((area as HTMLTextAreaElement).value).toBe(TEXT));
  });

  it('is for those who manage secrets', async () => {
    renderSecrets({ at: '/secrets/import', can: ['secret:read'] });

    expect(await screen.findByText(/Owners and admins of Acme import secrets/)).toBeTruthy();
    expect(screen.queryByLabelText('Paste .env or YAML')).toBeNull();
  });
});
