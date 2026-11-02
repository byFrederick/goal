// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ChangesCard } from './changes-card';

vi.mock('./use-review', () => ({
  useComparison: () => ({ data: undefined, isError: false }),
  useEditorFacts: () => ({ org: 'acme', factory: 'f' }),
}));

afterEach(cleanup);

describe('ChangesCard', () => {
  it('shows a skeleton while the comparison loads', () => {
    render(<ChangesCard pair={{ from: 'current', to: 'draft' }} />);
    expect(screen.getByText('Loading').closest('[aria-busy="true"]')).not.toBeNull();
  });
});
