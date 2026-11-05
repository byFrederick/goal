// @vitest-environment jsdom
import { cleanup, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { webhooksQuery } from '../../api/queries';
import { org, renderWebhooks, webhook } from './test-harness';

afterEach(cleanup);

const days = (n: number) =>
  [0, 0, 0, 0, 0, 0, n].map((delivered, i) => ({ day: `2026-11-0${i + 1}`, delivered, refused: 0 }));

const list = [
  webhook({
    name: 'github-acme',
    openings: [{ factory: 'f-software' }, { factory: 'f-sre', repositories: ['acme/api'] }],
    triggers: [{ factory: 'f-software', factory_name: 'software', trigger: 'check-fails' }],
    days: days(1204),
  }),
  webhook({ name: 'deploys', sender: 'standard', secret: 'deploys-key', openings: [], days: days(0) }),
];

describe('the Webhooks list', () => {
  it('shows each webhook with its sender, address, secret by name and the factories it is open to', async () => {
    renderWebhooks({ list });

    const link = await screen.findByRole('link', { name: /github-acme/ });
    expect(link.getAttribute('href')).toBe('/o/acme/webhooks/github-acme');
    expect(link.textContent).toContain('GitHub');
    expect(link.textContent).toContain('/hooks/id-github-acme');
    expect(link.textContent).toContain('webhook-github-acme');
    const row = link.closest('li') as HTMLElement;
    expect(within(row).getByText('Software')).toBeTruthy();
    expect(within(row).getByText('SRE')).toBeTruthy();
    expect(within(row).getByText('Receiving')).toBeTruthy();
    expect(within(row).getByText('1,204 this week')).toBeTruthy();

    const other = screen.getByRole('link', { name: /deploys/ }).closest('li') as HTMLElement;
    expect(within(other).getByText('Open to no factory')).toBeTruthy();
    expect(within(other).getByText('No trigger yet')).toBeTruthy();
    expect(within(other).getByText('None yet')).toBeTruthy();
  });

  it('offers New webhook to those who manage webhooks, and to nobody else', async () => {
    const first = renderWebhooks({ list });
    const create = await screen.findByRole('link', { name: 'New webhook' });
    expect(create.getAttribute('href')).toBe('/o/acme/webhooks/new');
    first.unmount();

    renderWebhooks({ list, can: ['webhook:read'] });
    await screen.findByRole('link', { name: /github-acme/ });
    expect(screen.queryByRole('link', { name: 'New webhook' })).toBeNull();
  });

  it('says there are none yet', async () => {
    renderWebhooks({ list: [] });

    expect(await screen.findByText('No webhooks yet')).toBeTruthy();
    expect(screen.getAllByRole('link', { name: 'New webhook' }).length).toBeGreaterThan(0);
  });

  it('shows its skeleton while the list loads', async () => {
    const { queryClient } = renderWebhooks({ list });
    queryClient.removeQueries({ queryKey: webhooksQuery(org.id).queryKey });

    await waitFor(() => expect(document.querySelector('[aria-busy="true"]')).not.toBeNull());
    expect(screen.getAllByText('Loading').length).toBeGreaterThan(0);
  });

  it('says it could not load, and offers Try again', async () => {
    const { queryClient } = renderWebhooks({ list });
    queryClient.setQueryData(webhooksQuery(org.id).queryKey, undefined);
    await queryClient.cancelQueries();
    queryClient
      .getQueryCache()
      .find({ queryKey: webhooksQuery(org.id).queryKey })
      ?.setState({
        status: 'error',
        error: new Error('down'),
        data: undefined,
        fetchStatus: 'idle',
      });

    expect(await screen.findByText(/Couldn’t load the webhooks/)).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy();
  });
});
