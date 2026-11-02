// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import type { FactoryModel } from '../../../api/types';
import type { Model, ModelAgent } from '../../../shared/definition/model';
import { TooltipProvider } from '../../../ui/tooltip';
import { DraftSession, type SessionApi } from '../draft/session';
import { DraftSessionContext } from '../draft/use-draft';
import { PromptEditor } from './prompt-editor';

// The running version's file is read through the API module the queries call, not through the session.
const getVersionFile = vi.hoisted(() => vi.fn());
vi.mock(import('../../../api/definitions'), async (original) => ({ ...(await original()), getVersionFile }));

afterEach(cleanup);

// The code editor asks for the phone layout; jsdom has no matchMedia, so answer "not a phone".
beforeAll(() => {
  window.matchMedia = vi.fn((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    onchange: null,
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
});

const running = '# engineer\n\nBuild it.\n';
const agent: ModelAgent = { name: 'engineer', prompt: { path: './agents/engineer.md', sha256: 'run1' } } as ModelAgent;

function model(): Model {
  return {
    basics: { id: 'f1', name: 'goal', work_types: ['feature'] },
    line: { stations: [{ id: 'done', kind: 'done' }] },
    agents: { agents: [agent], rules: [] },
    connections: [],
    review: {},
  } as unknown as Model;
}

/**
 * Renders the agent's prompt over a draft. `draft` is the draft's text of the prompt file (undefined: no
 * edit yet), `runningText` what the running version holds, and `readOnly` the view-only definition.
 */
function renderPrompt({
  draft,
  runningText = running,
  readOnly = false,
  pending = false,
}: {
  draft?: string;
  runningText?: string | null;
  readOnly?: boolean;
  /** The running file never arrives: the read stays pending. */
  pending?: boolean;
}) {
  const api = {
    getDraftModel: vi.fn(),
    openDraft: vi.fn(),
    saveDraftSection: vi.fn(),
    revertDraftSection: vi.fn(),
    discardDraft: vi.fn(),
    putDraftFile: vi.fn(),
    getDraftFile: vi.fn(async (_org: string, _f: string, path: string) => ({ path, text: draft ?? '', sha256: 'd' })),
  } as unknown as SessionApi;
  getVersionFile.mockImplementation(async (_org: string, _f: string, _v: string, path: string) => {
    if (pending) return new Promise(() => {});
    if (runningText === null) throw Object.assign(new Error('not found'), { code: 'file-not-found' });
    return { path, text: runningText, sha256: 'run1' };
  });
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
    model: model() as unknown as FactoryModel,
    etags: { basics: 'b1', line: 'l1', agents: 'a1', connections: 'c1', review: 'r1' },
    edited: {},
    ranges: {},
  });
  if (draft !== undefined) {
    session.adoptFile('agents/engineer.md', { text: running, sha256: 'run1' });
    session.editFile('agents/engineer.md', draft);
  }
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <TooltipProvider>
        <DraftSessionContext value={session}>
          <PromptEditor agent={agent} index={0} />
        </DraftSessionContext>
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return { session };
}

/** Focus opens the tip; the running file loads first, so the tip is checked once it reads the reason. */
async function waitForReason(button: HTMLElement, reason: string) {
  await waitFor(() => {
    fireEvent.focus(button);
    expect(screen.getByText(reason)).toBeTruthy();
  });
}

describe('Reset to default on an agent prompt', () => {
  it('restores the running version’s text into the draft', async () => {
    const { session } = renderPrompt({ draft: '# engineer, edited\n' });
    const reset = await screen.findByRole('button', { name: 'Reset to default' });
    await waitFor(() => expect(reset.getAttribute('aria-disabled')).toBeNull());

    fireEvent.click(reset);

    await waitFor(async () => expect(await session.fileText('agents/engineer.md')).toBe(running));
  });

  it('is disabled with the reason when the draft already matches the running text', async () => {
    const { session } = renderPrompt({ draft: running });
    const reset = await screen.findByRole('button', { name: 'Reset to default' });

    await waitForReason(reset, 'The prompt already matches the running version');
    expect(reset.getAttribute('aria-disabled')).toBe('true');

    fireEvent.click(reset);
    expect(await session.fileText('agents/engineer.md')).toBe(running);
  });

  it('is disabled with the reason when the running version has no such file', async () => {
    renderPrompt({ draft: '# engineer, new\n', runningText: null });
    const reset = await screen.findByRole('button', { name: 'Reset to default' });

    await waitForReason(reset, 'The running version has no prompt file to restore');
    expect(reset.getAttribute('aria-disabled')).toBe('true');
  });

  it('is not shown in view-only mode', async () => {
    renderPrompt({ draft: '# engineer, edited\n', readOnly: true });
    await screen.findByText('Prompt');
    expect(screen.queryByRole('button', { name: 'Reset to default' })).toBeNull();
  });

  it('shows a loading skeleton while the prompt text waits', () => {
    renderPrompt({ pending: true });
    const busy = document.querySelector('[aria-busy="true"]');
    expect(busy).not.toBeNull();
    expect(busy?.textContent).toContain('Loading');
  });
});
