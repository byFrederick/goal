// @vitest-environment jsdom

import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { type ReactNode, useState } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, addEvalCase, listEvals, startEvalRun } from '../../api/client';
import { evalEstimateQuery, evalsQuery } from '../../api/queries';
import type { EvalCaseEntry, EvalList, Factory, OrgSummary, Version } from '../../api/types';
import { TopBar, TopBarSetContext, type TopBarState } from '../../app/top-bar';
import { renderFactoryPage, type Seed } from '../../test/route';
import { TooltipProvider } from '../../ui/tooltip';
import { EvalsPage } from './evals-page';

vi.mock('../../api/client', async (original) => ({
  ...(await original<typeof import('../../api/client')>()),
  listEvals: vi.fn(),
  startEvalRun: vi.fn(),
  addEvalCase: vi.fn(),
}));

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'Ac', factory_count: 1, role: 'owner' };

const editor: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'software',
  title: 'Software',
  work_types: ['feature'],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'editor',
  can: ['definition:edit', 'definition:publish', 'factory:read'],
};
const viewer: Factory = { ...editor, role: 'viewer', can: ['factory:read'] };

const cases: EvalCaseEntry[] = [
  {
    name: 'architect-frontend-lane',
    description: 'A request about the sidebar gets the frontend lane',
    agent: 'architect',
    tries: 3,
    checked_by: { kind: 'contract' },
    last_tries: [
      { eval_run_id: 'run-9', try: 3, state: 'pass', cost_usd: 0.1 },
      { eval_run_id: 'run-9', try: 2, state: 'pass', cost_usd: 0.1 },
      { eval_run_id: 'run-9', try: 1, state: 'pass', cost_usd: 0.1 },
    ],
    cost_usd: 0.3,
  },
  {
    name: 'architect-splits-oversized-request',
    description: 'A 900-line request becomes an assembly of parts',
    agent: 'architect',
    tries: 3,
    checked_by: { kind: 'judge', judge: 'spec-criteria-testable', title: 'Spec criteria testable' },
    last_tries: [
      { eval_run_id: 'run-9', try: 3, state: 'pass', cost_usd: 0.2 },
      { eval_run_id: 'run-9', try: 2, state: 'fail', cost_usd: 0.2 },
      { eval_run_id: 'run-9', try: 1, state: 'pass', cost_usd: 0.3 },
    ],
    cost_usd: 0.7,
  },
  {
    name: 'backend-adds-migration-never-edits',
    description: 'A schema change adds a migration and leaves old ones alone',
    agent: 'backend-engineer',
    tries: 3,
    checked_by: { kind: 'reference_patch', path: 'evals/patches/migration.patch' },
    last_tries: [
      { eval_run_id: 'run-9', try: 3, state: 'pass', cost_usd: 0.3 },
      { eval_run_id: 'run-9', try: 2, state: 'pass', cost_usd: 0.3 },
      { eval_run_id: 'run-9', try: 1, state: 'pass', cost_usd: 0.2 },
    ],
    cost_usd: 0.8,
  },
  {
    name: 'reviewer-approves-clean-change',
    description: 'A change with full evidence is approved first time',
    agent: 'reviewer',
    tries: 3,
    checked_by: { kind: 'contract' },
    last_tries: [
      { eval_run_id: 'run-9', try: 3, state: 'pass', cost_usd: 0.08 },
      { eval_run_id: 'run-9', try: 2, state: 'pass', cost_usd: 0.08 },
      { eval_run_id: 'run-9', try: 1, state: 'pass', cost_usd: 0.09 },
    ],
    cost_usd: 0.25,
  },
  {
    name: 'reviewer-rejects-unevidenced-criterion',
    description: 'A criterion without evidence is sent back with a clear reason',
    agent: 'reviewer',
    tries: 3,
    checked_by: { kind: 'judge', judge: 'review-reason-actionable', title: 'Review reason actionable' },
    last_tries: [
      { eval_run_id: 'run-9', try: 3, state: 'fail', cost_usd: 0.12 },
      { eval_run_id: 'run-9', try: 2, state: 'pass', cost_usd: 0.11 },
      { eval_run_id: 'run-9', try: 1, state: 'pass', cost_usd: 0.12 },
    ],
    cost_usd: 0.35,
  },
];

const list: EvalList = {
  summary: {
    version: 21,
    cases: 5,
    cases_passing: 3,
    tries: 15,
    tries_passed: 13,
    first_try_pass_rate: 0.87,
    latest_run_id: 'run-9',
    latest_run_at: new Date().toISOString(),
    latest_tries_per_case: 3,
  },
  cases,
};

const estimate = { usd: 2.4, unpriced: [] };

function seeds(entries: EvalList = list, factory: Factory = editor): Seed[] {
  return [
    [evalsQuery(org.id, factory.id).queryKey, entries],
    [evalEstimateQuery(org.id, factory.id).queryKey, estimate],
  ];
}

// The page with the shell's top bar above it, so its TopBarSlot actions are on screen as they are in the app.
function TopBarFrame({ children }: { children: ReactNode }) {
  const [state, setState] = useState<TopBarState | null>(null);
  return (
    <TopBarSetContext.Provider value={setState}>
      <TopBar state={state} />
      {children}
    </TopBarSetContext.Provider>
  );
}

function renderEvals(factory: Factory = editor, seed: Seed[] = seeds(list, factory)) {
  return renderFactoryPage({
    page: () => (
      <TooltipProvider>
        <TopBarFrame>
          <EvalsPage />
        </TopBarFrame>
      </TooltipProvider>
    ),
    path: 'jonah/evals',
    org,
    factory,
    seed,
  });
}

describe('the Evals page, its four states', () => {
  it('is loading until the cases arrive: the app background, aria-busy and a Loading label', async () => {
    vi.mocked(listEvals).mockReturnValue(new Promise(() => {}));
    renderEvals(editor, []);

    const busy = (await screen.findByText('Loading')).closest('[aria-busy]');
    expect(busy?.getAttribute('aria-busy')).toBe('true');
    expect(screen.queryByRole('heading', { name: 'Jonah' })).toBeNull();
  });

  it('is empty when the version has no cases, and offers adding one', async () => {
    const empty: EvalList = {
      summary: { cases: 0, cases_passing: 0, tries: 0, tries_passed: 0 },
      cases: [],
    };
    renderEvals(editor, seeds(empty));

    expect(await screen.findByText('No eval cases yet')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Add a case from a past run' })).toBeTruthy();
    expect(screen.queryByRole('table')).toBeNull();
  });

  it('is an error when the cases cannot be read, and Try again reads them once more', async () => {
    vi.mocked(listEvals).mockRejectedValueOnce(
      new ApiError({ title: 'Service Unavailable', status: 503, code: 'evals-unavailable' }),
    );
    vi.mocked(listEvals).mockResolvedValueOnce(list);
    renderEvals(editor, []);

    expect(await screen.findByText('Eval cases could not be loaded')).toBeTruthy();
    expect(screen.getByText('Evals are not available on this goald yet.')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));

    expect(await screen.findByRole('table')).toBeTruthy();
    expect(listEvals).toHaveBeenCalledTimes(2);
  });

  it('is ready: the intro, the three tiles, the cases table and its footnote', async () => {
    renderEvals();

    expect(await screen.findByRole('heading', { level: 1, name: 'Jonah' })).toBeTruthy();
    expect(
      screen.getByText(
        'Eval cases replay a task an agent must get right, before any change to the definition is published.',
      ),
    ).toBeTruthy();

    expect(screen.getByText('Cases passing every try, v21')).toBeTruthy();
    expect(screen.getByText('3 of 5')).toBeTruthy();
    expect(screen.getByText('Pass on first try')).toBeTruthy();
    expect(screen.getByText('87%')).toBeTruthy();
    expect(screen.getByText('13 of 15 tries')).toBeTruthy();
    expect(screen.getByText('Every publish')).toBeTruthy();
    expect(screen.getByText('A failing case blocks the publish')).toBeTruthy();

    const table = screen.getByRole('table');
    expect(within(table).getAllByRole('row')).toHaveLength(6);
    expect(within(table).getByText('architect-frontend-lane')).toBeTruthy();
    expect(within(table).getByText('Judge: Spec criteria testable')).toBeTruthy();
    expect(within(table).getByText('Reference patch')).toBeTruthy();
    expect(within(table).getByText('$0.30')).toBeTruthy();
    expect(screen.getByText('In the definition under')).toBeTruthy();
    expect(
      screen.getByText(
        'Green passed, red failed. Add a case from any run where an agent got something wrong, and it is tested before every publish.',
      ),
    ).toBeTruthy();
  });

  it('says how each case’s last tries went in words, not only in colour', async () => {
    renderEvals();

    const table = await screen.findByRole('table');
    expect(within(table).getByRole('img', { name: '2 of 3 passed, last one failed' })).toBeTruthy();
    expect(within(table).getAllByRole('img', { name: '3 of 3 passed' })).toHaveLength(3);
  });
});

describe('Run and Run all', () => {
  it('runs one case: startEvalRun with that case, then the evals read again', async () => {
    vi.mocked(startEvalRun).mockResolvedValue({ run: {} as never, estimate } as never);
    renderEvals();
    await screen.findByRole('table');

    fireEvent.click(screen.getByRole('button', { name: 'Run architect-frontend-lane' }));

    await waitFor(() =>
      expect(startEvalRun).toHaveBeenCalledWith(org.id, editor.id, { case: 'architect-frontend-lane' }),
    );
    await waitFor(() => expect(listEvals).toHaveBeenCalled());
  });

  it('runs every case from "Run all, about $2.40" with no case named', async () => {
    vi.mocked(startEvalRun).mockResolvedValue({ run: {} as never, estimate } as never);
    renderEvals();

    fireEvent.click(await screen.findByRole('button', { name: 'Run all, about $2.40' }));

    await waitFor(() => expect(startEvalRun).toHaveBeenCalledWith(org.id, editor.id, {}));
  });

  it('shows a refused start in place and does not hide the cases', async () => {
    vi.mocked(startEvalRun).mockRejectedValue(
      new ApiError({ title: 'Bad Request', status: 400, code: 'eval-case-unknown', detail: 'no such case' }),
    );
    renderEvals();
    await screen.findByRole('table');

    fireEvent.click(screen.getByRole('button', { name: 'Run reviewer-approves-clean-change' }));

    expect(await screen.findByText('Not started: no such case')).toBeTruthy();
    expect(screen.getByRole('table')).toBeTruthy();
  });

  it('renders both Run controls disabled without definition:edit and never calls the API', async () => {
    renderEvals(viewer);
    await screen.findByRole('table');

    const runAll = screen.getByRole('button', { name: 'Run all, about $2.40' });
    expect((runAll as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(runAll);
    fireEvent.click(screen.getByRole('button', { name: 'Run architect-frontend-lane' }));

    expect((screen.getByRole('button', { name: 'Run architect-frontend-lane' }) as HTMLButtonElement).disabled).toBe(
      true,
    );
    expect(startEvalRun).not.toHaveBeenCalled();
  });
});

describe('adding a case from a past run', () => {
  const added: Version = { id: 'version-22', number: 22 } as unknown as Version;

  it('adds the case with its run, name and tries, and closes', async () => {
    vi.mocked(addEvalCase).mockResolvedValue(added);
    renderEvals();
    await screen.findByRole('table');

    fireEvent.click(screen.getByRole('button', { name: 'Add a case from a past run' }));
    const dialog = await screen.findByRole('dialog', { name: 'Add a case from a past run' });
    fireEvent.change(within(dialog).getByLabelText('Past run'), { target: { value: 'run-41' } });
    fireEvent.change(within(dialog).getByLabelText('Case name'), { target: { value: 'reviewer-misses-tests' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add case' }));

    await waitFor(() =>
      expect(addEvalCase).toHaveBeenCalledWith(org.id, editor.id, {
        run: 'run-41',
        name: 'reviewer-misses-tests',
        tries: 1,
      }),
    );
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  });

  it('names a case that is not a slug before sending anything', async () => {
    renderEvals();
    await screen.findByRole('table');

    fireEvent.click(screen.getByRole('button', { name: 'Add a case from a past run' }));
    const dialog = await screen.findByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText('Past run'), { target: { value: 'run-41' } });
    fireEvent.change(within(dialog).getByLabelText('Case name'), { target: { value: 'Bad Name' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add case' }));

    expect(await within(dialog).findByText('Use 3 to 40 lowercase letters, digits and hyphens.')).toBeTruthy();
    expect(addEvalCase).not.toHaveBeenCalled();
  });

  it('shows a 409 refusal in place: the draft is open', async () => {
    vi.mocked(addEvalCase).mockRejectedValue(
      new ApiError({ title: 'Conflict', status: 409, code: 'draft-open', detail: 'a draft is open' }),
    );
    renderEvals();
    await screen.findByRole('table');

    fireEvent.click(screen.getByRole('button', { name: 'Add a case from a past run' }));
    const dialog = await screen.findByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText('Past run'), { target: { value: 'run-41' } });
    fireEvent.change(within(dialog).getByLabelText('Case name'), { target: { value: 'reviewer-misses-tests' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add case' }));

    expect(await within(dialog).findByText(/The factory has an open draft/)).toBeTruthy();
    expect(screen.getByRole('dialog')).toBeTruthy();
  });

  it('is disabled without definition:publish', async () => {
    renderEvals({ ...editor, can: ['definition:edit', 'factory:read'] });
    await screen.findByRole('table');

    const add = screen.getByRole('button', { name: 'Add a case from a past run' });
    expect((add as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(add);
    expect(screen.queryByRole('dialog')).toBeNull();
  });
});
