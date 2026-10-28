// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { DefinitionProblem, FactoryModel } from '../../../api/types';
import { TooltipProvider } from '../../../ui/tooltip';
import { DraftSession, type SessionApi } from '../draft/session';
import { DraftSessionContext } from '../draft/use-draft';
import { Outline, ProblemBlock, ProblemsPanel } from './panels';

// A session holding a draft whose runtime size is xlarge, with no save sent: the fix is applied to it.
function draftSession(): DraftSession {
  const session = new DraftSession({ org: 'acme', factory: 'f1', api: {} as SessionApi });
  session.adopt({
    revision: 1,
    model: {
      basics: { id: 'f1', name: 'goal', size: 'xlarge' },
      line: { stations: [] },
      agents: { agents: [] },
      connections: [],
      review: {},
    } as unknown as FactoryModel,
    etags: { basics: '', line: '', agents: '', connections: '', review: '' },
    edited: {},
    ranges: {},
  });
  return session;
}

let session: DraftSession;

function renderInDraft(ui: ReactNode) {
  return render(
    <TooltipProvider>
      <DraftSessionContext.Provider value={session}>{ui}</DraftSessionContext.Provider>
    </TooltipProvider>,
  );
}

/** matchMedia answers the phone query (under 768 px with a coarse pointer) with `phone`. */
function setPhone(phone: boolean) {
  window.matchMedia = ((query: string) => ({
    matches: phone,
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  })) as unknown as typeof window.matchMedia;
}

beforeEach(() => {
  session = draftSession();
  setPhone(false);
});

afterEach(() => {
  cleanup();
  session.dispose();
  vi.restoreAllMocks();
  delete (window as { matchMedia?: unknown }).matchMedia;
});

const problem = (over: Partial<DefinitionProblem> = {}): DefinitionProblem =>
  ({
    code: 'enum',
    severity: 'error',
    message: 'spec.runtime.size: must be small, medium or large',
    line: 13,
    column: 1,
    end_line: 13,
    end_column: 0,
    file: 'factory.yaml',
    section: 'basics',
    pointer: '/basics/size',
    fixes: [{ label: 'Use large', section: 'basics', patch: [{ op: 'replace', path: '/size', value: 'large' }] }],
    ...over,
  }) as unknown as DefinitionProblem;

describe('ProblemsPanel', () => {
  it('counts the errors, names where each is, and says the checks run as you type', () => {
    renderInDraft(<ProblemsPanel problems={[problem()]} onGo={vi.fn()} headingId="p" />);
    expect(screen.getByRole('region', { name: 'Problems' }).textContent).toContain(
      'Checked as you type, the same checks as publish',
    );
    expect(screen.getByRole('button', { name: 'factory.yaml, line 13' })).toBeTruthy();
  });

  it('moves to the problem when its location is pressed', () => {
    const onGo = vi.fn();
    const p = problem();
    renderInDraft(<ProblemsPanel problems={[p]} onGo={onGo} headingId="p" />);
    fireEvent.click(screen.getByRole('button', { name: 'factory.yaml, line 13' }));
    expect(onGo).toHaveBeenCalledWith(p);
  });

  it('shows the quick fix in the row only on a phone, where the editor has no line blocks', () => {
    const { unmount } = renderInDraft(<ProblemsPanel problems={[problem()]} onGo={vi.fn()} headingId="p" />);
    expect(screen.queryByRole('button', { name: 'Use large' })).toBeNull();
    unmount();

    setPhone(true);
    renderInDraft(<ProblemsPanel problems={[problem()]} onGo={vi.fn()} headingId="p" />);
    expect(screen.getByRole('region', { name: 'Problems' }).textContent).toContain('Use large');
    expect(screen.getByRole('button', { name: 'Use large' })).toBeTruthy();
  });

  it('says so when there are no problems', () => {
    renderInDraft(<ProblemsPanel problems={[]} onGo={vi.fn()} headingId="p" />);
    expect(screen.getByText('No problems.')).toBeTruthy();
  });
});

describe('ProblemBlock', () => {
  it('applies the fix to the draft through applyFix', async () => {
    renderInDraft(<ProblemBlock problems={[problem()]} />);
    fireEvent.click(screen.getByRole('button', { name: 'Use large' }));
    await waitFor(() => expect(session.getSnapshot().sections.basics.local).toMatchObject({ size: 'large' }));
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('shows the error when the apply fails, and leaves the draft as it was', async () => {
    vi.spyOn(session, 'flush').mockRejectedValue(new Error('down'));
    renderInDraft(<ProblemBlock problems={[problem()]} />);
    fireEvent.click(screen.getByRole('button', { name: 'Use large' }));
    expect((await screen.findByRole('alert')).textContent).toBe('goald can’t be reached');
    expect(session.getSnapshot().sections.basics.local).toMatchObject({ size: 'xlarge' });
  });

  it('shows no button for a problem without fixes', () => {
    renderInDraft(<ProblemBlock problems={[problem({ fixes: [] } as Partial<DefinitionProblem>)]} />);
    expect(screen.getByText('spec.runtime.size: must be small, medium or large')).toBeTruthy();
    expect(screen.queryByRole('button')).toBeNull();
  });
});

describe('Outline', () => {
  it("jumps to a key's line when it is pressed, and names its problems", () => {
    const onGo = vi.fn();
    render(
      <Outline
        items={[
          { key: 'runtime', line: 6, depth: 1, problems: 1 },
          { key: 'connections', line: 9, depth: 1, count: 3, problems: 0 },
        ]}
        onGo={onGo}
      />,
    );
    const outline = screen.getByRole('complementary', { name: 'Outline' });
    expect(outline.textContent).toContain('1 problem');
    expect(outline.textContent).toContain('3');
    fireEvent.click(screen.getByRole('button', { name: /^runtime/ }));
    expect(onGo).toHaveBeenCalledWith(6);
  });
});
