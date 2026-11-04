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

  it('keeps Send "Not available yet" and sends no request', async () => {
    await renderBar();
    const ask = screen.getByRole('button', { name: 'Send' });
    expect(screen.queryByRole('button', { name: 'Ask' })).toBeNull();
    expect(ask.getAttribute('aria-disabled')).toBe('true');

    fireEvent.click(ask);
    fireEvent.keyDown(ask, { key: 'Enter' });
    expect(createDraftRequest).not.toHaveBeenCalled();
    ask.focus();
    expect((await screen.findAllByText('Not available yet')).length).toBeGreaterThan(0);
  });
});

describe('the ask bar layout', () => {
  it("has no glyph before the input; the only icon is the Send button's", async () => {
    const input = await renderBar();
    const icons = [...(input.parentElement?.querySelectorAll('svg') ?? [])];
    expect(icons.every((icon) => icon.closest('button'))).toBe(true);
  });

  it('draws Send 44 px square below md and 32 px from md up', async () => {
    await renderBar();
    const send = screen.getByRole('button', { name: 'Send' });
    expect(send.className).toContain('size-8');
    expect(send.className).toContain('max-md:size-11');
  });

  it('makes the input itself 44 px tall below md', async () => {
    const input = await renderBar();
    expect(input.className).toContain('max-md:min-h-11');
    expect(input.className).toContain('text-body');
  });

  it('keeps its hint, hidden below md so the bar stays one row on a phone', async () => {
    const input = await renderBar();
    const hint = screen.getByText('Becomes an item, reviewed like any change');
    // jsdom cannot measure layout: the classes stand in for it (hidden below md, shown from md up).
    expect(hint.className).toContain('hidden');
    expect(hint.className).toContain('md:inline');
    expect(input.parentElement?.className).not.toContain('flex-wrap');
  });
});
