// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { SectionSummary } from '../../../api/types';
import { SectionTiles } from './section-tiles';

vi.mock('@tanstack/react-router', () => ({
  useLinkProps: (o: { params: { section: string } }) => ({
    href: `/definition/${o.params.section}`,
    onClick: () => {},
    'aria-current': undefined,
  }),
}));
vi.mock('../../../shared/factory', () => ({ useFactoryParams: () => ({ org: 'acme', factory: 'f' }) }));
vi.mock('../draft/use-draft', () => ({ useModel: () => ({ line: {}, agents: {}, connections: [], basics: {} }) }));

Element.prototype.scrollIntoView = () => {};
afterEach(cleanup);

const summaries: SectionSummary[] = [
  { section: 'basics', changes: 0, problems: 0, lines: [1, 2] },
  { section: 'connections', changes: 1, problems: 1, lines: [3, 4] },
];

const props = {
  current: 'basics' as const,
  summaries,
  changeCount: 1,
  problemCount: 1,
  hasDraft: true,
  yaml: false,
  parseError: null,
};

describe('the section nav', () => {
  it('lists five links, marks the current one and names the changes and the problem', () => {
    render(<SectionTiles {...props} />);
    const nav = screen.getByRole('navigation', { name: 'Definition sections' });
    expect(nav.querySelectorAll('a')).toHaveLength(5);
    const current = nav.querySelectorAll('a[aria-current="location"]');
    expect(current).toHaveLength(1);
    expect(current[0]?.textContent).toContain('Basics');
    const connections = screen.getByRole('link', { name: /^Connections/ });
    expect(connections.textContent).toContain('1 change');
    expect(connections.textContent).toContain('1 problem');
  });

  it('view only shows four links', () => {
    render(<SectionTiles {...props} viewOnly />);
    expect(screen.getByRole('navigation').querySelectorAll('a')).toHaveLength(4);
  });
});
