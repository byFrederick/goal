// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { DraftSession } from '../draft/session';
import { DraftSessionContext } from '../draft/use-draft';
import { TokenField } from './token-field';

afterEach(cleanup);

describe('TokenField phone tap targets', () => {
  it('makes Remove <noun> <value> 44 px below md and 24 px from md', () => {
    const session = new DraftSession({ org: 'acme', factory: 'factory-1', refresh: () => {}, closed: () => {} });
    render(
      <DraftSessionContext value={session}>
        <TokenField
          label="Work types"
          noun="work type"
          values={['feature']}
          placeholder="Add"
          check={() => undefined}
          help="Help"
          onAdd={vi.fn()}
          onRemove={vi.fn()}
        />
      </DraftSessionContext>,
    );
    const button = screen.getByRole('button', { name: 'Remove work type feature' });
    expect(button.classList.contains('size-11')).toBe(true);
    expect(button.classList.contains('md:size-6')).toBe(true);
    expect(button.classList.contains('size-6')).toBe(false);
  });
});
