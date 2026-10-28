// @vitest-environment jsdom

import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { type ReactNode, useState } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { softwareJudges } from '../../../mock/fixtures.ts';
import { ApiError, createJudge, getJudge, listJudges, updateJudge } from '../../api/client';
import { builderMetaQuery, judgesQuery } from '../../api/queries';
import type { Factory, JudgeList, OrgSummary } from '../../api/types';
import { TopBar, TopBarSetContext, type TopBarState } from '../../app/top-bar';
import { renderFactoryPage, type Seed } from '../../test/route';
import { TooltipProvider } from '../../ui/tooltip';
import { JudgesPage } from './judges-page';

vi.mock('../../api/client', async (original) => ({
  ...(await original<typeof import('../../api/client')>()),
  listJudges: vi.fn(),
  getJudge: vi.fn(),
  updateJudge: vi.fn(),
  tryJudge: vi.fn(),
  createJudge: vi.fn(),
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

const list: JudgeList = {
  version: 21,
  judges: softwareJudges.map(({ slug, title, criterion, agents, sample, pass_mark, enabled, summary }) => ({
    slug,
    title,
    criterion,
    agents,
    sample,
    pass_mark,
    enabled,
    summary,
  })),
  judge_cost_usd: 11.6,
};

const models = {
  models: [{ id: 'claude-sonnet-5-5', label: 'Claude Sonnet 5.5', default: true }],
} as never;

function seeds(entries: JudgeList = list, factory: Factory = editor): Seed[] {
  return [
    [judgesQuery(org.id, factory.id).queryKey, entries],
    [builderMetaQuery(org.id).queryKey, models],
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

function renderJudges(factory: Factory = editor, seed: Seed[] = seeds(list, factory)) {
  return renderFactoryPage({
    page: () => (
      <TooltipProvider>
        <TopBarFrame>
          <JudgesPage />
        </TopBarFrame>
      </TooltipProvider>
    ),
    path: 'jonah/judges',
    org,
    factory,
    seed,
  });
}

describe('the Judges page, its four states', () => {
  it('is loading until the judges arrive: the app background, aria-busy and a Loading label', async () => {
    vi.mocked(listJudges).mockReturnValue(new Promise(() => {}));
    renderJudges(editor, []);

    const busy = (await screen.findByText('Loading')).closest('[aria-busy]');
    expect(busy?.getAttribute('aria-busy')).toBe('true');
    expect(screen.queryByRole('heading', { name: 'Jonah' })).toBeNull();
  });

  it('is empty before the first publish, and offers a new judge', async () => {
    renderJudges(editor, seeds({ version: null, judges: [], judge_cost_usd: 0 }));

    expect(await screen.findByText('No judges yet')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'New judge' })).toBeTruthy();
    expect(screen.queryByRole('table')).toBeNull();
  });

  it('is an error when the judges cannot be read, and Try again reads them once more', async () => {
    vi.mocked(listJudges).mockRejectedValueOnce(
      new ApiError({ title: 'Service Unavailable', status: 503, code: 'judges-unavailable' }),
    );
    vi.mocked(listJudges).mockResolvedValueOnce(list);
    renderJudges(editor, []);

    expect(await screen.findByText('Judges could not be loaded')).toBeTruthy();
    expect(screen.getByText('Judges are not available on this goald yet.')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));

    expect(await screen.findByRole('table')).toBeTruthy();
    expect(listJudges).toHaveBeenCalledTimes(2);
  });

  it('is ready: the intro, the three steps, the table with the 30-day cost and its footnote', async () => {
    renderJudges();

    expect(await screen.findByRole('heading', { level: 1, name: 'Jonah' })).toBeTruthy();
    expect(screen.getByText('Judges read a sample of runs and score them against a rubric you write.')).toBeTruthy();
    const steps = within(screen.getByRole('region', { name: 'How judges work' }));
    expect(steps.getByText('Sample')).toBeTruthy();
    expect(steps.getByText('Score')).toBeTruthy();
    expect(steps.getByText('Learn')).toBeTruthy();

    const table = screen.getByRole('table');
    expect(within(table).getAllByRole('row')).toHaveLength(5);
    for (const heading of ['Judge', 'Scores', 'Sample', 'Pass at', 'Last 30 days', 'Cost']) {
      expect(within(table).getByRole('columnheader', { name: heading })).toBeTruthy();
    }
    expect(within(table).getByRole('link', { name: 'Review reason actionable' })).toBeTruthy();
    expect(within(table).getByText('$3.20')).toBeTruthy();
    expect(within(table).getByText('81%')).toBeTruthy();
    expect(screen.getByText('$11.60 in the last 30 days')).toBeTruthy();
    expect(screen.getByText('evals/judges/')).toBeTruthy();
  });
});

describe('the judges switch', () => {
  it('reads the judge, flips its enabled flag and publishes it against the version the page saw', async () => {
    const judge = softwareJudges.find((j) => j.slug === 'plain-language');
    if (!judge) throw new Error('the picture has the plain language judge');
    vi.mocked(getJudge).mockResolvedValue(judge);
    vi.mocked(updateJudge).mockResolvedValue({ ...judge, enabled: false });
    renderJudges();

    fireEvent.click(await screen.findByRole('switch', { name: 'Plain language on' }));

    await waitFor(() => expect(updateJudge).toHaveBeenCalledTimes(1));
    expect(updateJudge).toHaveBeenCalledWith(
      org.id,
      editor.id,
      'plain-language',
      expect.objectContaining({
        title: 'Plain language',
        enabled: false,
        pass_mark: 0.8,
        expected_current_version_id: 'version-21',
      }),
    );
    expect(getJudge).toHaveBeenCalledWith(org.id, editor.id, 'plain-language', expect.anything());
  });

  it('does not show a refused flip as flipped, and says why', async () => {
    const judge = softwareJudges.find((j) => j.slug === 'plain-language');
    if (!judge) throw new Error('the picture has the plain language judge');
    vi.mocked(getJudge).mockResolvedValue(judge);
    vi.mocked(updateJudge).mockRejectedValue(
      new ApiError({ title: 'Conflict', status: 409, code: 'draft-open', detail: 'a draft is open' }),
    );
    renderJudges();

    const toggle = await screen.findByRole('switch', { name: 'Plain language on' });
    fireEvent.click(toggle);

    expect(await screen.findByText(/The factory has an open draft/)).toBeTruthy();
    expect(screen.getByRole('switch', { name: 'Plain language on' }).getAttribute('aria-checked')).toBe('true');
  });

  it('is disabled and never calls the API without definition:publish', async () => {
    renderJudges(viewer);

    const toggle = await screen.findByRole('switch', { name: 'Plain language on' });
    expect(toggle.getAttribute('aria-disabled')).toBe('true');
    fireEvent.click(toggle);

    expect(getJudge).not.toHaveBeenCalled();
    expect(updateJudge).not.toHaveBeenCalled();
    expect((screen.getByRole('button', { name: 'New judge' }) as HTMLButtonElement).disabled).toBe(true);
  });
});

describe('New judge', () => {
  it('creates a judge from the dialog and reads the new one', async () => {
    vi.mocked(createJudge).mockResolvedValue({ ...softwareJudges[0], slug: 'tone' } as never);
    renderJudges();

    fireEvent.click(await screen.findByRole('button', { name: 'New judge' }));
    const dialog = await screen.findByRole('dialog', { name: 'New judge' });
    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Tone' } });
    fireEvent.change(within(dialog).getByLabelText('File name'), { target: { value: 'tone' } });
    fireEvent.change(within(dialog).getByLabelText('What it checks'), { target: { value: 'Reads kindly' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'New judge' }));

    await waitFor(() => expect(createJudge).toHaveBeenCalledTimes(1));
    expect(createJudge).toHaveBeenCalledWith(
      org.id,
      editor.id,
      expect.objectContaining({ slug: 'tone', title: 'Tone', criterion: 'Reads kindly', pass_mark: 0.7 }),
    );
  });
});
