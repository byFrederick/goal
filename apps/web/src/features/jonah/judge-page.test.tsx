// @vitest-environment jsdom

import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { type ReactNode, useState } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { softwareJudges } from '../../../mock/fixtures.ts';
import { ApiError, getJudge, tryJudge, updateJudge } from '../../api/client';
import { builderMetaQuery, judgeQuery, versionModelQuery } from '../../api/queries';
import type { Factory, Judge, OrgSummary } from '../../api/types';
import { TopBar, TopBarSetContext, type TopBarState } from '../../app/top-bar';
import { renderFactoryPage, type Seed } from '../../test/route';
import { TooltipProvider } from '../../ui/tooltip';
import { JudgePage } from './judge-page';

vi.mock('../../api/client', async (original) => ({
  ...(await original<typeof import('../../api/client')>()),
  getJudge: vi.fn(),
  updateJudge: vi.fn(),
  tryJudge: vi.fn(),
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
  current: { id: 'version-21', number: 21 } as unknown as Factory['current'],
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'editor',
  can: ['definition:edit', 'definition:publish', 'factory:read'],
};
const viewer: Factory = { ...editor, role: 'viewer', can: ['factory:read'] };

const review = softwareJudges.find((j) => j.slug === 'review-reason-actionable') as Judge;

const models = {
  models: [
    { id: 'claude-sonnet-5-5', label: 'Claude Sonnet 5.5', default: true },
    { id: 'claude-haiku-5-5', label: 'Claude Haiku 5.5', default: false },
  ],
} as never;
const agentsOfVersion = {
  model: { agents: { agents: [{ name: 'architect' }, { name: 'reviewer' }] } },
  ranges: {},
} as never;

function seeds(judge: Judge = review, factory: Factory = editor): Seed[] {
  return [
    [judgeQuery(org.id, factory.id, judge.slug).queryKey, judge],
    [builderMetaQuery(org.id).queryKey, models],
    [versionModelQuery(org.id, factory.id, 'version-21').queryKey, agentsOfVersion],
  ];
}

// The page with the shell's top bar above it, so its Save and Try actions are on screen as they are in the app.
function TopBarFrame({ children }: { children: ReactNode }) {
  const [state, setState] = useState<TopBarState | null>(null);
  return (
    <TopBarSetContext.Provider value={setState}>
      <TopBar state={state} />
      {children}
    </TopBarSetContext.Provider>
  );
}

function renderJudge(
  factory: Factory = editor,
  seed: Seed[] = seeds(review, factory),
  slug = 'review-reason-actionable',
) {
  return renderFactoryPage({
    page: () => (
      <TooltipProvider>
        <TopBarFrame>
          <JudgePage slug={slug} />
        </TopBarFrame>
      </TooltipProvider>
    ),
    path: 'jonah/judges/$judge',
    location: `jonah/judges/${slug}`,
    org,
    factory,
    seed,
  });
}

describe('the judge page, its four states', () => {
  it('is loading until the judge arrives: the app background, aria-busy and a Loading label', async () => {
    vi.mocked(getJudge).mockReturnValue(new Promise(() => {}));
    renderJudge(editor, []);

    const busy = (await screen.findByText('Loading')).closest('[aria-busy]');
    expect(busy?.getAttribute('aria-busy')).toBe('true');
  });

  it('says so when the judge is not in the current version', async () => {
    vi.mocked(getJudge).mockRejectedValue(new ApiError({ title: 'Not Found', status: 404, code: 'judge-not-found' }));
    renderJudge(editor, []);

    expect(await screen.findByText('This judge is not in the current version')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Back to judges' })).toBeTruthy();
  });

  it('is an error when the judge cannot be read, and Try again reads it once more', async () => {
    vi.mocked(getJudge)
      .mockRejectedValueOnce(new ApiError({ title: 'Service Unavailable', status: 503 }))
      .mockResolvedValueOnce(review);
    renderJudge(editor, []);

    expect(await screen.findByText('The judge could not be loaded')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));

    expect(await screen.findByRole('heading', { level: 1, name: 'Review reason actionable' })).toBeTruthy();
    expect(getJudge).toHaveBeenCalledTimes(2);
  });

  it('is ready: the title, the summary line, the file, the outcomes, the sliders and the runs', async () => {
    renderJudge();

    expect(await screen.findByRole('heading', { level: 1, name: 'Review reason actionable' })).toBeTruthy();
    expect(screen.getByText('Scored 46 runs in 30 days, 81% passed, $3.20')).toBeTruthy();
    expect(screen.getByRole('textbox', { name: 'What it checks' })).toBeTruthy();
    expect(screen.getByText('Agents it scores')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Remove reviewer' })).toBeTruthy();
    expect(screen.getByText('evals/judges/review-reason-actionable.md')).toBeTruthy();
    expect(screen.getByLabelText('Outcome 1 label')).toBeTruthy();
    expect(screen.getByLabelText('Outcome 3 label')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Add an outcome' })).toBeTruthy();
    expect(screen.getByRole('slider', { name: 'Pass mark' }).getAttribute('value')).toBe('0.7');
    expect(screen.getByRole('slider', { name: 'Sample' }).getAttribute('value')).toBe('25');
    expect(screen.getByRole('combobox', { name: 'Judge model' })).toBeTruthy();
    expect(screen.getByText('#149 Empty state for Intake')).toBeTruthy();
    expect(screen.getByText('Runs on')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Try on 10 recent runs' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Save changes' })).toBeTruthy();
  });
});

describe('editing and saving', () => {
  it('saves the edited file as a new version, against the version the page saw', async () => {
    vi.mocked(updateJudge).mockResolvedValue({ ...review, version: 22 });
    renderJudge();

    const criterion = await screen.findByRole('textbox', { name: 'What it checks' });
    fireEvent.change(criterion, { target: { value: 'A sent-back reason names the fix' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }));

    await waitFor(() => expect(updateJudge).toHaveBeenCalledTimes(1));
    expect(updateJudge).toHaveBeenCalledWith(
      org.id,
      editor.id,
      'review-reason-actionable',
      expect.objectContaining({
        title: 'Review reason actionable',
        criterion: 'A sent-back reason names the fix',
        pass_mark: 0.7,
        sample: 0.25,
        enabled: true,
        expected_current_version_id: 'version-21',
      }),
    );
    // A judge with no model sends none: the factory's runtime model runs it.
    expect(vi.mocked(updateJudge).mock.calls[0]?.[3]).not.toHaveProperty('model');
  });

  it('shows a refused save in place and keeps the edits', async () => {
    vi.mocked(updateJudge).mockRejectedValue(
      new ApiError({ title: 'Conflict', status: 409, code: 'evals-pending', detail: 'a run is pending' }),
    );
    renderJudge();

    fireEvent.change(await screen.findByRole('textbox', { name: 'What it checks' }), {
      target: { value: 'Changed' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }));

    expect(await screen.findByText(/It publishes once that run passes/)).toBeTruthy();
    expect((screen.getByRole('textbox', { name: 'What it checks' }) as HTMLInputElement).value).toBe('Changed');
  });

  it('refuses to save an outcome with no label, and says which', async () => {
    renderJudge();

    fireEvent.change(await screen.findByLabelText('Outcome 2 label'), { target: { value: '' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }));

    expect(await screen.findByText('Every outcome needs a label.')).toBeTruthy();
    expect(updateJudge).not.toHaveBeenCalled();
  });

  it('adds an outcome and removes one', async () => {
    renderJudge();

    fireEvent.click(await screen.findByRole('button', { name: 'Add an outcome' }));
    expect(screen.getByLabelText('Outcome 4 label')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Remove outcome 4' }));
    expect(screen.queryByLabelText('Outcome 4 label')).toBeNull();
  });

  it('tries the judge on the 10 newest runs and says so', async () => {
    vi.mocked(tryJudge).mockResolvedValue({ tries: [] });
    vi.mocked(getJudge).mockResolvedValue(review);
    renderJudge();

    fireEvent.click(await screen.findByRole('button', { name: 'Try on 10 recent runs' }));

    await waitFor(() => expect(tryJudge).toHaveBeenCalledWith(org.id, editor.id, 'review-reason-actionable'));
    expect(await screen.findByText(/Queued on the 10 newest runs/)).toBeTruthy();
  });
});

describe('without definition:publish', () => {
  it('renders Save, Try and the form read-only and never calls the API', async () => {
    renderJudge(viewer);

    await screen.findByRole('heading', { level: 1, name: 'Review reason actionable' });
    expect((screen.getByRole('button', { name: 'Save changes' }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole('button', { name: 'Try on 10 recent runs' }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByRole('switch', { name: 'Review reason actionable on' }).getAttribute('aria-disabled')).toBe(
      'true',
    );
    // The form's fields sit in a disabled fieldset, so the browser disables every control in it.
    const form = screen.getByRole('textbox', { name: 'What it checks' }).closest('form');
    expect(form?.querySelector('fieldset')?.disabled).toBe(true);
    expect(within(form as HTMLElement).getByRole('button', { name: 'Add an outcome' })).toBeTruthy();

    expect(updateJudge).not.toHaveBeenCalled();
    expect(tryJudge).not.toHaveBeenCalled();
  });
});
