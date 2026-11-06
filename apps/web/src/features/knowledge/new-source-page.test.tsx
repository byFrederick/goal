// @vitest-environment jsdom
import { cleanup, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { factoriesQuery, knowledgeSourcesQuery, secretsQuery } from '../../api/queries';
import type { Factory } from '../../api/types';
import { NewSourcePage } from './new-source-page';
import { renderKnowledge, summary } from './test-harness';

const create = vi.hoisted(() => vi.fn((_org: string, _body: unknown) => new Promise(() => {})));
vi.mock('../../api/knowledge', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/knowledge')>()),
  createKnowledgeSource: create,
}));
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

  it('offers the on-demand schedule and sends 0 when it is chosen', async () => {
    renderKnowledge({ page: NewSourcePage, route: 'new', location: '/o/acme/knowledge/new?kind=repository', seed });

    const every = (await screen.findByLabelText('Sync every')) as HTMLSelectElement;
    expect(
      within(every)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['Every hour', 'Every day', 'Only when someone presses Sync']);
    await userEvent.selectOptions(every, 'Only when someone presses Sync');
    await userEvent.type(screen.getByLabelText('Name'), 'runbooks');
    await userEvent.type(screen.getByLabelText('When should agents read it?'), 'Before an alert.');
    await userEvent.type(screen.getByPlaceholderText('https://github.com/acme/docs'), 'https://github.com/acme/ops');
    await userEvent.click(screen.getByRole('button', { name: 'Add source' }));

    await waitFor(() => expect(create).toHaveBeenCalled());
    expect(create.mock.calls[0]?.[1]).toMatchObject({ sync_every_seconds: 0 });
  });

  it('shows Add source as busy, not disabled, while the source is created, and does not submit twice', async () => {
    renderKnowledge({ page: NewSourcePage, route: 'new', location: '/o/acme/knowledge/new?kind=repository', seed });

    await userEvent.type(await screen.findByLabelText('Name'), 'runbooks');
    await userEvent.type(screen.getByLabelText('When should agents read it?'), 'Before an alert.');
    await userEvent.type(screen.getByPlaceholderText('https://github.com/acme/docs'), 'https://github.com/acme/ops');
    const button = screen.getByRole('button', { name: 'Add source' });
    await userEvent.click(button);

    await waitFor(() => expect(button.getAttribute('aria-busy')).toBe('true'));
    expect(button.hasAttribute('disabled')).toBe(false);
    await userEvent.click(button);
    expect(create).toHaveBeenCalledTimes(1);
  });

  it('shows an optional Root page for Confluence and refuses an id that is not digits', async () => {
    renderKnowledge({ page: NewSourcePage, route: 'new', location: '/o/acme/knowledge/new?kind=confluence', seed });

    const root = await screen.findByLabelText('Root page (optional)');
    expect(screen.getByText(/Only that page and the pages under it are read/)).toBeTruthy();
    await userEvent.type(root, 'abc');
    await userEvent.click(screen.getByRole('button', { name: /^Add source/ }));
    expect(await screen.findByText('A page id is digits only, such as 12345.')).toBeTruthy();
  });
});
