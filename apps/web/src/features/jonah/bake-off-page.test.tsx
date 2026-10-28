// @vitest-environment jsdom

import type { Problem } from '@goal/api-client';
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { pictureBakeOff } from '../../../mock/bake-offs.ts';
import { ApiError, applyBakeOff, getBakeOff, listBakeOffs, rerunBakeOff } from '../../api/client';
import type { BakeOff, BakeOffList, Factory, OrgSummary } from '../../api/types';
import { renderFactoryPage } from '../../test/route';
import { BakeOffIndexPage } from './bake-off-index-page';
import { BakeOffPage } from './bake-off-page';

vi.mock('../../api/client', async (original) => ({
  ...(await original<typeof import('../../api/client')>()),
  listBakeOffs: vi.fn(),
  getBakeOff: vi.fn(),
  rerunBakeOff: vi.fn(),
  applyBakeOff: vi.fn(),
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

const BAKE_OFF_PATH = 'jonah/bake-offs/$bakeOff';
const bakeOffUrl = (id: string) => `/o/acme/f/software/jonah/bake-offs/${id}`;

/** The picture's bake-off, as the API answers it. */
const done = (): BakeOff => pictureBakeOff();

const listOf = (...bakeOffs: BakeOff[]): BakeOffList => ({ bake_offs: bakeOffs });

const problemError = (status: number, code: string) =>
  new ApiError({ title: code, status, code } as unknown as Problem);

/** The page at a bake-off's URL, with its list and the bake-off seeded as the page reads them. */
function renderBakeOff(
  bakeOff: BakeOff,
  { factory = editor, list = listOf(bakeOff) }: { factory?: Factory; list?: BakeOffList } = {},
) {
  vi.mocked(getBakeOff).mockResolvedValue(bakeOff);
  vi.mocked(listBakeOffs).mockResolvedValue(list);
  return renderFactoryPage({
    page: () => <BakeOffPage />,
    path: BAKE_OFF_PATH,
    location: `jonah/bake-offs/${bakeOff.id}`,
    org,
    factory,
    topBar: true,
  });
}

describe('the bake-off page', () => {
  it('shows the picture: title, subline, banner, plot, grid and results', async () => {
    renderBakeOff(done());

    expect(await screen.findByRole('heading', { level: 1, name: 'Which model for frontend-engineer?' })).toBeTruthy();
    expect(
      screen.getByText('3 models on the same 5 frontend cases, 3 tries each. Finished Oct 25 at 18:40, cost $14.20.'),
    ).toBeTruthy();

    // The banner: the recommended model against the current one.
    expect(
      screen.getByRole('heading', { name: 'claude-haiku-5-5: the same pass rate at a tenth of the cost' }),
    ).toBeTruthy();
    expect(
      screen.getByText('93% of tries passed, like claude-opus-5-5, for $0.24 an item instead of $2.40.'),
    ).toBeTruthy();

    // The plot names every point, and shows each point's label.
    const plot = screen.getByRole('img', {
      name: /haiku 5\.5 93% at \$0\.24.*sonnet 5\.5 87% at \$0\.90.*opus 5\.5 93% at \$2\.40/,
    });
    expect(plot).toBeTruthy();
    expect(within(plot).getByText('haiku 5.5')).toBeTruthy();
    expect(screen.getByText('93%, $0.24')).toBeTruthy();
    expect(screen.getByText('87%, $0.90')).toBeTruthy();
    expect(within(plot).getByText('Cost per item')).toBeTruthy();
    expect(screen.getByText('Up and left is better')).toBeTruthy();

    // The grid: the case name, and a number in every model's cell.
    expect(screen.getByText('Out of 3')).toBeTruthy();
    const row = screen.getByRole('rowheader', { name: 'board-card-move-menu' }).closest('tr');
    expect(row).toBeTruthy();
    expect(
      within(row as HTMLElement)
        .getAllByRole('cell')
        .map((c) => c.textContent),
    ).toEqual(['2', '2', '3']);

    // The results: the chips, the figures and the judged column.
    const table = screen.getByRole('table', { name: 'Results of the bake-off, one row per model' });
    expect(table.parentElement?.className).toContain('overflow-x-auto');
    expect(screen.getByRole('columnheader', { name: 'Judged plain language' })).toBeTruthy();
    const haiku = screen.getByRole('rowheader', { name: /claude-haiku-5-5/ }).closest('tr') as HTMLElement;
    expect(within(haiku).getByText('Recommended')).toBeTruthy();
    expect(within(haiku).getByText('$0.24')).toBeTruthy();
    expect(within(haiku).getByText('14 min')).toBeTruthy();
    expect(within(haiku).getByText('38')).toBeTruthy();
    expect(within(haiku).getByText('95%')).toBeTruthy();
    const opus = screen.getByRole('rowheader', { name: /claude-opus-5-5/ }).closest('tr') as HTMLElement;
    expect(within(opus).getByText('Now')).toBeTruthy();
    expect(within(opus).queryByText('Recommended')).toBeNull();
  });

  it('sets the breadcrumb to factory / Jonah / Bake-offs / the agent and its day', async () => {
    renderBakeOff(done());

    // The crumb is the page's own once the bake-off is read; until then it is the list's.
    await screen.findByText('frontend-engineer, Oct 25');
    const crumbs = screen.getByRole('navigation', { name: 'Breadcrumb' });
    expect(within(crumbs).getByRole('link', { name: 'Bake-offs' }).getAttribute('href')).toBe(
      '/o/acme/f/software/jonah/bake-offs',
    );
    expect(within(crumbs).getByText('frontend-engineer, Oct 25').getAttribute('aria-current')).toBe('page');
  });

  it('shows the Jonah tabs with Bake-offs as the current one and its count', async () => {
    renderBakeOff(done());

    const tabs = await screen.findByRole('navigation', { name: 'Jonah' });
    const current = within(tabs).getByRole('link', { name: /Bake-offs/ });
    expect(current.getAttribute('aria-current')).toBe('page');
    expect(within(tabs).getByRole('link', { name: /Evals/ }).getAttribute('aria-current')).toBeNull();
  });

  it('shows the loading state while the bake-off is read', async () => {
    vi.mocked(getBakeOff).mockReturnValue(new Promise(() => {}));
    vi.mocked(listBakeOffs).mockReturnValue(new Promise(() => {}));
    renderFactoryPage({
      page: () => <BakeOffPage />,
      path: BAKE_OFF_PATH,
      location: 'jonah/bake-offs/bake-off-1',
      org,
      factory: editor,
    });
    expect((await screen.findByText('Loading')).closest('[aria-busy="true"]')).toBeTruthy();
  });

  it('says the bake-off is not in the factory when goald does not know it', async () => {
    vi.mocked(getBakeOff).mockRejectedValue(problemError(404, 'eval-run-not-found'));
    vi.mocked(listBakeOffs).mockResolvedValue(listOf());
    renderFactoryPage({
      page: () => <BakeOffPage />,
      path: BAKE_OFF_PATH,
      location: 'jonah/bake-offs/missing',
      org,
      factory: editor,
    });
    expect(await screen.findByText('This bake-off is not in this factory')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Try again' })).toBeNull();
  });

  it('shows an error with Try again, and reads the bake-off again on it', async () => {
    vi.mocked(getBakeOff).mockRejectedValueOnce(problemError(503, 'evals-unavailable'));
    vi.mocked(listBakeOffs).mockResolvedValue(listOf());
    renderFactoryPage({
      page: () => <BakeOffPage />,
      path: BAKE_OFF_PATH,
      location: 'jonah/bake-offs/bake-off-1',
      org,
      factory: editor,
    });
    expect(await screen.findByText('The bake-off could not be loaded')).toBeTruthy();

    vi.mocked(getBakeOff).mockResolvedValueOnce(done());
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByRole('heading', { name: 'Which model for frontend-engineer?' })).toBeTruthy();
  });

  it('has no banner when no model is recommended', async () => {
    renderBakeOff({ ...done(), recommended_arm: null });

    await screen.findByRole('heading', { name: 'Which model for frontend-engineer?' });
    expect(screen.queryByText(/the same pass rate/)).toBeNull();
    expect(screen.queryByRole('button', { name: 'Apply to the definition' })).toBeNull();
  });

  it('has no banner when the recommended model is the current one', async () => {
    const bakeOff = done();
    renderBakeOff({ ...bakeOff, recommended_arm: 3 });

    await screen.findByRole('heading', { name: 'Which model for frontend-engineer?' });
    expect(screen.queryByRole('button', { name: 'Apply to the definition' })).toBeNull();
  });

  it('shows the status in the subline in place of the finish while the bake-off runs', async () => {
    const running = done();
    renderBakeOff({
      ...running,
      state: 'running',
      recommended_arm: null,
      arms: running.arms.map((a) => ({ ...a, summary: null })),
    });

    expect(await screen.findByText('Running')).toBeTruthy();
    expect(screen.queryByText(/Finished/)).toBeNull();
    expect(screen.queryByText(/the same pass rate/)).toBeNull();
    expect(screen.getByRole('img', { name: 'Scatter: no model has finished its tries yet' })).toBeTruthy();
  });

  it('leaves the Judged column out when no arm has a judged share', async () => {
    const bakeOff = done();
    renderBakeOff({
      ...bakeOff,
      arms: bakeOff.arms.map((a) => ({ ...a, summary: a.summary && { ...a.summary, judged: null } })),
    });

    await screen.findByRole('heading', { name: 'Which model for frontend-engineer?' });
    expect(screen.queryByRole('columnheader', { name: /Judged/ })).toBeNull();
  });

  it('runs the bake-off again and moves to the new one', async () => {
    const original = done();
    renderBakeOff(original);
    const queued = { ...original, id: 'bake-off-2', state: 'queued' as const };
    vi.mocked(rerunBakeOff).mockResolvedValue({ bake_off: queued, estimate: { usd: 15.6, unpriced: [] } });

    const run = await screen.findByRole('button', { name: 'Run again' });
    fireEvent.click(run);

    await waitFor(() => expect(rerunBakeOff).toHaveBeenCalledWith('org-1', 'factory-1', 'bake-off-1'));
    expect(await screen.findByText('Queued')).toBeTruthy();
    await waitFor(() => expect(screen.getByRole('link', { name: 'Bake-offs' })).toBeTruthy());
    expect(bakeOffUrl('bake-off-2')).toBe('/o/acme/f/software/jonah/bake-offs/bake-off-2');
  });

  it('writes the recommended model into the draft, then links to the draft', async () => {
    renderBakeOff(done());
    vi.mocked(applyBakeOff).mockResolvedValue({ id: 'draft-1' } as never);

    fireEvent.click(await screen.findByRole('button', { name: 'Apply to the definition' }));

    await waitFor(() => expect(applyBakeOff).toHaveBeenCalledWith('org-1', 'factory-1', 'bake-off-1', 1));
    const link = await screen.findByRole('link', { name: 'Open the draft' });
    expect(link.getAttribute('href')).toBe('/o/acme/f/software/definition');
    expect(screen.queryByRole('button', { name: 'Apply to the definition' })).toBeNull();
  });

  it('sends a person to the builder when the agent changed since the bake-off ran', async () => {
    renderBakeOff(done());
    vi.mocked(applyBakeOff).mockRejectedValue(problemError(412, 'draft-conflict'));

    fireEvent.click(await screen.findByRole('button', { name: 'Apply to the definition' }));

    const alert = await screen.findByText(/The agent changed after this bake-off ran/);
    expect(
      within(alert.closest('div') as HTMLElement)
        .getByRole('link', { name: 'builder' })
        .getAttribute('href'),
    ).toBe('/o/acme/f/software/definition');
  });

  it('disables Run again and Apply without definition:edit, and never calls the API', async () => {
    renderBakeOff(done(), { factory: viewer });

    expect((await screen.findByRole('button', { name: 'Run again' })) as HTMLButtonElement).toHaveProperty(
      'disabled',
      true,
    );
    expect(screen.getByRole('button', { name: 'Apply to the definition' })).toHaveProperty('disabled', true);
    fireEvent.click(screen.getByRole('button', { name: 'Run again' }));
    fireEvent.click(screen.getByRole('button', { name: 'Apply to the definition' }));
    expect(rerunBakeOff).not.toHaveBeenCalled();
    expect(applyBakeOff).not.toHaveBeenCalled();
  });

  it("links New bake-off to the factory's Agents screen", async () => {
    renderBakeOff(done());
    const link = await screen.findByRole('link', { name: 'New bake-off' });
    expect(link.getAttribute('href')).toBe('/o/acme/f/software/agents');
  });
});

describe('the bake-offs tab', () => {
  const renderIndex = (list: Promise<BakeOffList> | BakeOffList | Error) => {
    if (list instanceof Error) vi.mocked(listBakeOffs).mockRejectedValue(list);
    else if (list instanceof Promise) vi.mocked(listBakeOffs).mockReturnValue(list);
    else vi.mocked(listBakeOffs).mockResolvedValue(list);
    vi.mocked(getBakeOff).mockResolvedValue(done());
    return renderFactoryPage({
      page: () => <BakeOffIndexPage />,
      path: 'jonah/bake-offs/',
      org,
      factory: editor,
    });
  };

  it('opens the newest bake-off', async () => {
    const { router } = renderIndex(listOf(done()));
    await waitFor(() => expect(router.state.location.pathname).toBe(bakeOffUrl('bake-off-1')));
  });

  it('shows an empty state when the factory has no bake-off', async () => {
    renderIndex(listOf());
    expect(await screen.findByText('No bake-offs yet')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'New bake-off' }).getAttribute('href')).toBe('/o/acme/f/software/agents');
  });

  it('shows the loading state while the list is read', async () => {
    renderIndex(new Promise(() => {}));
    expect(await screen.findByText('Loading')).toBeTruthy();
  });

  it('shows an error with Try again when the list fails', async () => {
    renderIndex(problemError(503, 'evals-unavailable'));
    expect(await screen.findByText('Bake-offs could not be loaded')).toBeTruthy();
    expect(screen.getByText('Evals are not available on this goald yet.')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy();
  });
});
