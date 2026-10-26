// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { attentionQuery, factoriesQuery } from '../api/queries';
import type { Factory, OrgSummary } from '../api/types';
import { useFactorySquares } from './factory-squares';

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 2, role: 'owner' };

const factory = (name: string, extra: Partial<Factory> = {}): Factory => ({
  id: `${name}-id`,
  org_id: 'org-1',
  name,
  work_types: [],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'editor',
  can: [],
  ...extra,
});

/** The rail's squares, by the factory names it shows. */
function Squares() {
  const squares = useFactorySquares(org);
  return (
    <ul aria-label="Squares">
      {squares.map((square) => (
        <li key={square.factory.id}>{square.factory.name}</li>
      ))}
    </ul>
  );
}

function renderSquares(list: Factory[]) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(factoriesQuery(org.id).queryKey, list);
  client.setQueryData(attentionQuery(org.id).queryKey, { waiting_on_you: 0, factories: [] });
  const rendered = render(
    <QueryClientProvider client={client}>
      <Squares />
    </QueryClientProvider>,
  );
  return { ...rendered, client };
}

afterEach(cleanup);

describe('the rail’s factory squares', () => {
  it('leaves an archived factory out', () => {
    renderSquares([factory('goal'), factory('sre', { archived_at: '2026-10-25T09:00:00Z' })]);

    const squares = screen.getByRole('list', { name: 'Squares' });
    expect(squares.textContent).toContain('goal');
    expect(squares.textContent).not.toContain('sre');
  });

  it('drops a factory from the rail once the list no longer has it, with no reload', async () => {
    const { client } = renderSquares([factory('goal'), factory('sre')]);
    expect(screen.getByText('sre')).toBeTruthy();

    // An archive in this tab: its refetched list leaves the factory out.
    client.setQueryData(factoriesQuery(org.id).queryKey, [factory('goal')]);

    await waitFor(() => expect(screen.queryByText('sre')).toBeNull());
    expect(screen.getByText('goal')).toBeTruthy();
  });
});
