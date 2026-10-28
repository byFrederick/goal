// @vitest-environment jsdom
import { cleanup, fireEvent, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createDraftRequest } from '../../../api/definitions';
import type { Factory, OrgSummary } from '../../../api/types';
import { renderFactoryPage } from '../../../test/route';
import { AskBar } from './ask-bar';

vi.mock('../../../api/definitions', async (original) => ({
  ...(await original<typeof import('../../../api/definitions')>()),
  createDraftRequest: vi.fn(),
}));

afterEach(cleanup);
beforeEach(() => {
  vi.mocked(createDraftRequest).mockReset();
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: 'owner' };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'goal',
  title: 'Goal',
  work_types: ['feature'],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'editor',
  can: ['definition:edit'],
};

async function renderBar() {
  renderFactoryPage({ page: () => <AskBar />, path: 'definition/line', org, factory });
  // The router mounts the page after its match: the bar is there once its input is.
  return screen.findByRole('textbox', { name: 'Ask the factory to change its definition' });
}

describe('the Ask the factory bar, not built yet', () => {
  it('shows the input disabled, with its placeholder', async () => {
    const input = await renderBar();
    expect((input as HTMLInputElement).disabled).toBe(true);
    expect(input.getAttribute('placeholder')).toBe(
      'Ask the factory to change it, for example: give Review a 4th reviewer',
    );
  });

  it('keeps Ask "Not available yet" and sends no request', async () => {
    await renderBar();
    const ask = screen.getByRole('button', { name: 'Ask' });
    expect(ask.getAttribute('aria-disabled')).toBe('true');

    fireEvent.click(ask);
    fireEvent.keyDown(ask, { key: 'Enter' });
    expect(createDraftRequest).not.toHaveBeenCalled();
    ask.focus();
    expect((await screen.findAllByText('Not available yet')).length).toBeGreaterThan(0);
  });
});
