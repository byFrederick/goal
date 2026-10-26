// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { FactoryModel, SaveResult, SectionSave } from '../../../api/types';
import type { Model } from '../../../shared/definition/model';
import { DraftSession, type SessionApi } from '../draft/session';
import { DraftSessionContext } from '../draft/use-draft';
import { RulesEditor } from './rules-editor';

afterEach(cleanup);

const secretsText = '---\ndescription: Keep secrets out of commits.\n---\n\nNever commit one.';

function model(rules: Model['agents']['rules'] = []): Model {
  return {
    basics: { id: 'f1', name: 'goal', work_types: ['feature'] },
    line: {
      stations: [
        { id: 'build', kind: 'agent', agent: 'engineer', route: { backend: 'engineer' } },
        { id: 'done', kind: 'done' },
      ],
    },
    agents: {
      agents: [
        { name: 'engineer', prompt: { path: './agents/engineer.md' } },
        { name: 'reviewer', prompt: { path: './agents/reviewer.md' } },
      ],
      rules,
    },
    connections: [],
    review: {},
  };
}

const result = (revision: number): SaveResult => ({
  revision,
  etag: `e${revision}`,
  updated_at: '2026-10-26T09:00:00Z',
  change_count: 1,
  problem_count: 0,
  sections: [],
});

/** Renders the rules area over a draft holding `rules` and the rule files in `files`; `readOnly` shows the definition only. */
function renderRules({
  rules = [],
  files = {},
  readOnly = false,
}: {
  rules?: Model['agents']['rules'];
  files?: Record<string, string>;
  readOnly?: boolean;
} = {}) {
  const saveDraftSection = vi.fn(async (_org: string, _f: string, _s: string, _e: string, _save: SectionSave) =>
    result(2),
  );
  const api = {
    getDraftModel: vi.fn(),
    openDraft: vi.fn(),
    saveDraftSection,
    revertDraftSection: vi.fn(),
    discardDraft: vi.fn(),
    putDraftFile: vi.fn(async () => result(2)),
    getDraftFile: vi.fn(async (_org: string, _f: string, path: string) => {
      const text = files[path];
      if (text === undefined) throw Object.assign(new Error('not found'), { code: 'file-not-found' });
      return { path, text, sha256: 'x' };
    }),
    getVersionFile: vi.fn(),
  } as unknown as SessionApi;
  const session = new DraftSession({
    org: 'acme',
    factory: 'f1',
    api,
    refresh: () => {},
    closed: () => {},
    debounceMs: 0,
    readOnly,
  });
  session.adopt({
    revision: 1,
    model: model(rules) as unknown as FactoryModel,
    etags: { basics: 'b1', line: 'l1', agents: 'a1', connections: 'c1', review: 'r1' },
    edited: {},
    ranges: {},
  });
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <DraftSessionContext value={session}>
        <RulesEditor />
      </DraftSessionContext>
    </QueryClientProvider>,
  );
  return { saveDraftSection };
}

/** The one agents save the editor sent. */
async function sentSave(saveDraftSection: ReturnType<typeof renderRules>['saveDraftSection']): Promise<SectionSave> {
  await waitFor(() => expect(saveDraftSection).toHaveBeenCalledTimes(1));
  const [, , section, , save] = saveDraftSection.mock.calls[0] ?? [];
  expect(section).toBe('agents');
  return save as SectionSave;
}

const value = (save: SectionSave) => save.value as { rules?: { name: string; source: string }[] };

describe('the Rules area', () => {
  it('adds a rule: its entry and its file with frontmatter, in one agents save', async () => {
    const { saveDraftSection } = renderRules();
    expect(screen.getByText('No rules yet')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Add a rule' }));
    const dialog = await screen.findByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'secrets' } });
    fireEvent.change(within(dialog).getByLabelText('Description'), {
      target: { value: 'Keep secrets out of commits.' },
    });
    fireEvent.click(within(dialog).getByRole('checkbox', { name: 'engineer' }));
    fireEvent.change(within(dialog).getByLabelText('Paths'), { target: { value: 'src/**' } });
    fireEvent.change(within(dialog).getByLabelText('Rule text'), { target: { value: 'Never commit one.' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add rule' }));

    const save = await sentSave(saveDraftSection);
    expect(value(save).rules).toEqual([{ name: 'secrets', source: './rules/secrets.md' }]);
    expect(save.files).toEqual({
      'rules/secrets.md': {
        text: '---\ndescription: Keep secrets out of commits.\nappliesTo:\n  agents:\n    - engineer\npaths:\n  - src/**\n---\n\nNever commit one.',
      },
    });
  });

  it('edits a rule: rewrites its file and leaves the rules list as it was', async () => {
    const rules = [{ name: 'secrets', source: './rules/secrets.md' }];
    const { saveDraftSection } = renderRules({ rules, files: { 'rules/secrets.md': secretsText } });
    expect(await screen.findByText('Keep secrets out of commits.')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Edit secrets' }));
    const dialog = await screen.findByRole('dialog');
    const description = await within(dialog).findByLabelText('Description');
    fireEvent.change(description, { target: { value: 'Keep secrets out of git.' } });
    fireEvent.change(within(dialog).getByLabelText('Rule text'), { target: { value: 'Rotate on any leak.' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save rule' }));

    const save = await sentSave(saveDraftSection);
    expect(value(save).rules).toEqual(rules);
    expect(save.files).toEqual({
      'rules/secrets.md': { text: '---\ndescription: Keep secrets out of git.\n---\n\nRotate on any leak.' },
    });
  });

  it('deletes a rule after confirming: drops its entry and removes its file', async () => {
    const rules = [{ name: 'secrets', source: './rules/secrets.md' }];
    const { saveDraftSection } = renderRules({ rules, files: { 'rules/secrets.md': secretsText } });
    expect(await screen.findByText('Keep secrets out of commits.')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Delete secrets' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Delete rule' }));

    const save = await sentSave(saveDraftSection);
    expect(value(save).rules).toEqual([]);
    expect(save.remove_files).toEqual(['rules/secrets.md']);
  });

  it('shows who a rule applies to, and shows no editing in view-only mode', async () => {
    const rules = [{ name: 'secrets', source: './rules/secrets.md' }];
    renderRules({ rules, files: { 'rules/secrets.md': secretsText }, readOnly: true });
    expect(await screen.findByText('Every agent')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Add a rule' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Edit secrets' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Delete secrets' })).toBeNull();
  });
});
