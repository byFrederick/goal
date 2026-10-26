// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { FactoryModel } from '../../../api/types';
import type { Model, ModelRule } from '../../../shared/definition/model';
import { DraftSession, type SessionApi } from '../draft/session';
import { DraftSessionContext } from '../draft/use-draft';
import { AgentRules } from './agent-rules';

afterEach(cleanup);

const rule = (description: string, appliesTo = '') =>
  `---\ndescription: ${description}${appliesTo ? `\nappliesTo:\n${appliesTo}` : ''}\n---\n\nBody.`;

const files: Record<string, string> = {
  'rules/secrets.md': rule('Keep secrets out of commits.'),
  'rules/tests.md': rule('Run the tests before a handoff.', '  stations:\n    - build\n  lanes:\n    - backend'),
  'rules/delivery.md': rule('Deliver by pull request.', '  agents:\n    - reviewer'),
};

function model(rules: ModelRule[]): Model {
  return {
    basics: { id: 'f1', name: 'goal', work_types: ['feature'] },
    line: {
      stations: [
        { id: 'build', name: 'Build', kind: 'agent', agent: 'engineer' },
        { id: 'done', kind: 'done' },
      ],
    },
    agents: { agents: [{ name: 'engineer' }, { name: 'reviewer' }], rules },
    connections: [],
    review: {},
  };
}

/** Renders the agent's rules over a draft holding `rules`, with their files read from `files`. */
function renderAgentRules(rules: ModelRule[]) {
  const api = {
    getDraftModel: vi.fn(),
    openDraft: vi.fn(),
    getDraftFile: async (_org: string, _f: string, path: string) => {
      const text = files[path];
      if (text === undefined) throw Object.assign(new Error('not found'), { code: 'file-not-found' });
      return { path, text, sha256: 'x' };
    },
    getVersionFile: vi.fn(),
  } as unknown as SessionApi;
  const session = new DraftSession({
    org: 'acme',
    factory: 'f1',
    api,
    refresh: () => {},
    closed: () => {},
    debounceMs: 0,
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
        <AgentRules agent="engineer" />
      </DraftSessionContext>
    </QueryClientProvider>,
  );
}

describe('the agent’s Rules', () => {
  it('lists the rules that apply to the agent, marking the ones kept to its stations and lanes', async () => {
    renderAgentRules([
      { name: 'secrets', source: './rules/secrets.md' },
      { name: 'tests', source: './rules/tests.md' },
      { name: 'delivery', source: './rules/delivery.md' },
    ]);

    const list = await screen.findByRole('list');
    const items = within(list).getAllByRole('listitem');
    expect(items).toHaveLength(2);
    expect(within(items[0] as HTMLElement).getByText('secrets')).toBeTruthy();
    expect(within(items[0] as HTMLElement).getByText('Keep secrets out of commits.')).toBeTruthy();
    expect(within(items[0] as HTMLElement).getByText('Always')).toBeTruthy();
    expect(within(items[1] as HTMLElement).getByText('tests')).toBeTruthy();
    expect(within(items[1] as HTMLElement).getByText('Only at Build · lanes backend')).toBeTruthy();
    expect(screen.queryByText('delivery')).toBeNull();
  });

  it('says so in one line when no rule applies to the agent', async () => {
    renderAgentRules([{ name: 'delivery', source: './rules/delivery.md' }]);

    expect(await screen.findByText('No factory rule applies to engineer yet.')).toBeTruthy();
    expect(screen.queryByRole('list')).toBeNull();
  });
});
