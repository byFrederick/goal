// @vitest-environment jsdom
import { cleanup, render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Version, Watch } from '../../../api/types';
import { VersionRow } from './version-history';

vi.mock('./use-review', () => ({ useEditorFacts: () => ({ hasDraft: false }) }));
vi.mock('../search-link', () => ({ useSetSearch: () => vi.fn() }));

afterEach(cleanup);

const watch = (over: Partial<Watch>): Watch => ({
  station: 'build',
  metric: 'sent_back_rate',
  before: { items: 20, value: 0.4 },
  after: { items: 12, value: 0.2 },
  interval: null,
  verdict: 'helped',
  opened_at: '2026-10-24T10:00:00Z',
  closed_at: '2026-10-30T10:00:00Z',
  ...over,
});

const version = (watches: Watch[] | undefined): Version =>
  ({
    id: 'v20',
    number: 20,
    revision: 1,
    state: 'published',
    message: 'Reviewer asks for screenshots',
    source: 'jonah',
    published_at: '2026-10-24T10:00:00Z',
    published_by: null,
    current: false,
    ...(watches ? { watches } : {}),
  }) as unknown as Version;

describe('VersionRow watches', () => {
  it('shows each watch with its numbers and verdict, and Watching while it is open', () => {
    render(
      <ul>
        <VersionRow
          version={version([
            watch({}),
            watch({
              metric: 'cost_per_item',
              before: { items: 20, value: 2 },
              after: { items: 3, value: 2.5 },
              verdict: 'too_early',
              closed_at: null,
            }),
          ])}
          comparing={undefined}
        />
      </ul>,
    );
    const list = screen.getByRole('list', { name: 'Watch of v20' });
    const rows = within(list).getAllByRole('listitem');
    expect(rows).toHaveLength(2);
    expect(rows[0]?.textContent).toContain('→');
    expect(within(rows[0] as HTMLElement).getByText('Helped')).toBeTruthy();
    expect(within(rows[1] as HTMLElement).getByText('Watching')).toBeTruthy();
  });

  it('shows no watch list for a change nobody watches', () => {
    render(
      <ul>
        <VersionRow version={version([])} comparing={undefined} />
      </ul>,
    );
    expect(screen.queryByRole('list', { name: /Watch of/ })).toBeNull();
  });
});
