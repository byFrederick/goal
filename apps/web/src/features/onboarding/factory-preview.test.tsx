// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { draftModelQuery, factoryQuery } from '../../api/queries';
import type { Factory } from '../../api/types';
import { FactoryCardOf } from './factory-preview';

afterEach(cleanup);

function card(factory: Pick<Factory, 'name' | 'title'>) {
  const client = new QueryClient();
  client.setQueryData(factoryQuery('org-1', 'support-replies').queryKey, factory as Factory);
  client.setQueryData(draftModelQuery('org-1', 'support-replies').queryKey, {
    revision: 1,
    model: { basics: {}, line: { stations: [] } },
    etags: {},
  } as never);
  render(
    <QueryClientProvider client={client}>
      <FactoryCardOf org="org-1" factory="support-replies" />
    </QueryClientProvider>,
  );
}

describe('FactoryCardOf', () => {
  it('names the factory by its title, not its slug', async () => {
    card({ name: 'support-replies', title: 'Support replies' });
    expect(await screen.findByText('Support replies')).toBeTruthy();
    expect(screen.queryByText('support-replies')).toBeNull();
  });
});
