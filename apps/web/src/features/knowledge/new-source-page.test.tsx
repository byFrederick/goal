// @vitest-environment jsdom
import { cleanup, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { factoriesQuery, knowledgeSourcesQuery, secretsQuery } from '../../api/queries';
import type { Factory } from '../../api/types';
import { NewSourcePage } from './new-source-page';
import { renderKnowledge, summary } from './test-harness';

vi.mock('../../app/top-bar', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../app/top-bar')>()),
  TopBarSlot: () => null,
}));

beforeEach(() => {
  vi.spyOn(globalThis, 'fetch').mockImplementation(() => new Promise(() => {}));
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const seed = [
  [knowledgeSourcesQuery(summary.id).queryKey, []],
  [factoriesQuery(summary.id).queryKey, [{ id: 'f-sre', name: 'SRE' }] as unknown as Factory[]],
  [secretsQuery(summary.id).queryKey, []],
] as const;

describe('Add a source', () => {
  it('offers Upload, Repository and Confluence, and shows Website, Notion and Google Drive as later', async () => {
    renderKnowledge({ page: NewSourcePage, route: 'new', location: '/o/acme/knowledge/new', seed });

    const group = await screen.findByRole('group', { name: 'Where the files are' });
    for (const kind of ['Upload', 'Repository', 'Confluence']) {
      expect((within(group).getByRole('radio', { name: new RegExp(kind) }) as HTMLInputElement).disabled).toBe(false);
    }
    for (const kind of ['Website', 'Notion', 'Google Drive']) {
      expect((within(group).getByRole('radio', { name: new RegExp(kind) }) as HTMLInputElement).disabled).toBe(true);
    }
  });

  it('asks "When should agents read it?" and lists the factories as checkboxes', async () => {
    renderKnowledge({ page: NewSourcePage, route: 'new', location: '/o/acme/knowledge/new?kind=repository', seed });

    expect(await screen.findByLabelText('When should agents read it?')).toBeTruthy();
    expect(screen.getByRole('checkbox', { name: /SRE/ })).toBeTruthy();
    expect(screen.getByRole('radio', { name: /Repository/ })).toHaveProperty('checked', true);
    expect(screen.getByRole('button', { name: 'Check' })).toHaveProperty('disabled', true);
    await userEvent.type(screen.getByLabelText('Name'), 'runbooks');
    expect(screen.getByText('Its folder in every run: /knowledge/runbooks. It never changes.')).toBeTruthy();
  });
});
