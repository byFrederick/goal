// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ChangesCard } from './changes-card';

const comparison = vi.hoisted(() => ({ data: undefined as unknown }));

vi.mock('./use-review', () => ({
  useComparison: () => ({ data: comparison.data, isError: false }),
  useEditorFacts: () => ({ org: 'acme', factory: 'f' }),
}));

afterEach(() => {
  cleanup();
  comparison.data = undefined;
});

describe('ChangesCard', () => {
  it('shows a skeleton while the comparison loads', () => {
    render(<ChangesCard pair={{ from: 'current', to: 'draft' }} />);
    expect(screen.getByText('Loading').closest('[aria-busy="true"]')).not.toBeNull();
  });

  it('says how many changes in how many sections', () => {
    comparison.data = {
      from: { number: 1 },
      to: { number: 2 },
      counts: { changes: 3, sections: 2 },
      files: [],
      changes: [
        {
          op: 'changed',
          section: 'basics',
          summary: 'Name changes',
          target: { kind: 'x', id: 'a' },
          from_lines: [],
          to_lines: [],
        },
        {
          op: 'changed',
          section: 'basics',
          summary: 'Purpose changes',
          target: { kind: 'x', id: 'b' },
          from_lines: [],
          to_lines: [],
        },
        {
          op: 'added',
          section: 'line',
          summary: 'Adds a station',
          target: { kind: 'x', id: 'c' },
          from_lines: [],
          to_lines: [],
        },
      ],
    };
    render(<ChangesCard pair={{ from: 'source', to: 'draft' }} />);
    expect(screen.getByText('3 changes in 2 sections')).toBeTruthy();
  });
});
