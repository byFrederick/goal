// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, cancelItem, pauseItem, retryItem } from '../../api/client';
import type {
  AssemblyPart,
  AssemblyParts,
  Board,
  BoardLane,
  Factory,
  Item,
  ItemFlow,
  OrgSummary,
} from '../../api/types';
import { expectBusyOnClick } from '../../test/busy';
import { renderItemPage } from '../../test/route';
import { TopBarProbe } from '../../test/top-bar-probe';
import { AssemblyPage } from './assembly-page';

vi.mock(import('../../api/client'), async (original) => ({
  ...(await original()),
  pauseItem: vi.fn(async () => assembly),
  retryItem: vi.fn(async () => assembly),
  cancelItem: vi.fn(async () => ({ ...assembly, state: 'cancelled' as const })),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'goal',
  title: 'Goal',
  work_types: ['feature'],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'approver',
  can: ['item:operate', 'item:decide'],
};

const flow: ItemFlow = {
  spans: [],
  target_seconds: null,
  rework_target: null,
  budget: { per_item_usd: 4, max_turns_per_task: null },
  current_run: null,
};

const assembly: Item = {
  id: 'item-40',
  ref: 'goal#40',
  factory_id: 'factory-1',
  number: 40,
  type: 'feature',
  title: 'Split the assembly page into its parts and their waves across the board and the item',
  body: 'Ship the assembly page.',
  station: 'architect',
  state: 'assembly',
  class_of_service: 'standard',
  priority: 0,
  rework_count: 0,
  cost_usd: 0.5,
  outputs: {},
  created_at: '2026-10-20T00:00:00Z',
  updated_at: '2026-10-25T00:00:00Z',
  touches: { count: 3, planned: 0, unplanned: 3, list: [] },
  flow,
};

function part(n: number, over: Partial<AssemblyPart>): AssemblyPart {
  return {
    id: `part-${n}`,
    ref: `goal#${40 + n}`,
    number: 40 + n,
    index: n,
    key: `p${n}`,
    title: `Part ${n} title`,
    paths: [],
    station: 'build',
    state: 'queued',
    needs: [],
    waiting_on: [],
    waiting: null,
    wave: 0,
    cost_usd: 1,
    pr_url: null,
    agent: null,
    created_at: '2026-10-20T00:00:00Z',
    done_at: null,
    ...over,
  } as AssemblyPart;
}

const parts: AssemblyParts = {
  done: 1,
  total: 4,
  lead_time_seconds: 3 * 24 * 3600,
  parts: [
    part(1, {
      state: 'done',
      done_at: '2026-10-22T00:00:00Z',
      pr_url: 'https://github.com/acme/goal/pull/101',
      cost_usd: 2,
    }),
    part(2, { state: 'in_progress', wave: 0, station: 'build', agent: 'infra-engineer' }),
    part(3, { wave: 1, waiting_on: ['goal#41'], state: 'queued' }),
    part(4, { wave: 1, waiting_on: ['goal#41', 'goal#42'], state: 'queued' }),
  ],
};

const lanes: BoardLane[] = [
  {
    key: 'infra-engineer',
    kind: 'agent',
    role: 'Infra engineer',
    model: 'claude-opus-5-5',
    model_label: 'Opus 5.5',
    slots: 1,
  },
] as BoardLane[];
const board = { stations: [{ id: 'build', items: [], lanes }] } as unknown as Board;

/** The text of one figure in the progress card's strip, by its label (the strip's column holds label, figure and meta). */
function stat(label: string) {
  const column = within(screen.getByRole('region', { name: 'Assembly figures' })).getByText(label).parentElement;
  return column?.textContent?.replace(label, '') ?? '';
}

/** Renders the page once the router has mounted it (the route resolves asynchronously). */
async function renderAssembly(over: { item?: Item; parts?: AssemblyParts; factory?: Factory } = {}) {
  renderItemPage({
    page: () => (
      <TopBarProbe>
        <AssemblyPage />
      </TopBarProbe>
    ),
    org,
    factory: over.factory ?? factory,
    item: over.item ?? assembly,
    parts: over.parts ?? parts,
    board,
  });
  await screen.findByRole('heading', { level: 1 });
}

describe('AssemblyPage actions in the top bar', () => {
  it('draws Add a part, Pause and Cancel assembly in the top bar and none in the page', async () => {
    await renderAssembly({ factory: { ...factory, can: ['item:create', 'item:operate', 'item:decide'] } });

    const bar = within(screen.getByLabelText('Top bar'));
    for (const name of ['Add a part', 'Pause', 'Cancel assembly']) {
      expect(bar.getByRole('button', { name })).toBeTruthy();
      expect(screen.getAllByRole('button', { name })).toHaveLength(1);
    }
  });

  it('puts Resume, the primary action, last', async () => {
    await renderAssembly({ parts: pausedParts });

    const bar = within(screen.getByLabelText('Top bar'));
    const names = bar.getAllByRole('button').map((b) => b.textContent);
    expect(names.at(-1)).toMatch(/^Resume/);
  });
});

function stubWidth(phone: boolean) {
  window.matchMedia = (query: string) =>
    ({
      matches: phone && query === '(max-width: 767px)',
      media: query,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }) as unknown as MediaQueryList;
}

describe('AssemblyPage breadcrumb', () => {
  afterEach(() => {
    delete (window as { matchMedia?: unknown }).matchMedia;
  });

  it('draws no in-page breadcrumb or board link: the top bar carries the trail', async () => {
    stubWidth(false);
    await renderAssembly();

    expect(screen.queryByRole('navigation', { name: 'Breadcrumb' })).toBeNull();
    expect(screen.queryByRole('link', { name: /board/i })).toBeNull();
  });

  it('puts Back to Board in the phone top bar', async () => {
    stubWidth(true);
    await renderAssembly();

    const back = within(screen.getByLabelText('Top bar')).getByRole('link', { name: 'Back to Board' });
    expect(back.getAttribute('href')).toBe('/o/acme/f/goal/board');
  });
});

describe('AssemblyPage header', () => {
  it('shows the assembly label, the key, the title and the brief, and no Add a part without item:create', async () => {
    await renderAssembly();

    expect(screen.getByText('Assembly')).toBeTruthy();
    expect(screen.getAllByText('goal#40').length).toBeGreaterThan(0);
    const heading = screen.getByRole('heading', { level: 1, name: assembly.title });
    expect(heading.getAttribute('title')).toBe(assembly.title);
    expect(screen.getByText('Ship the assembly page.')).toBeTruthy();

    expect(screen.queryByRole('button', { name: /add a part/i })).toBeNull();
  });
});

describe('AssemblyPage v3 header', () => {
  it('has no in-page breadcrumb or back link, and no action row in the content', async () => {
    await renderAssembly();

    expect(screen.queryByRole('navigation', { name: 'Breadcrumb' })).toBeNull();
    expect(screen.queryByRole('link', { name: /Goal board/ })).toBeNull();
    expect(screen.getByRole('heading', { level: 1 }).closest('header')?.querySelector('button')).toBeNull();
  });

  it('collapses a long brief under Show more and opens it', async () => {
    await renderAssembly({ item: { ...assembly, body: 'A long brief. '.repeat(40) } });

    const text = (): number => screen.queryAllByText(/A long brief\./).length;
    const more = screen.getByRole('button', { name: 'Show more' });
    expect(more.getAttribute('aria-expanded')).toBe('false');
    expect(text()).toBe(1);
    fireEvent.click(more);
    const less = screen.getByRole('button', { name: 'Show less' });
    expect(less.getAttribute('aria-expanded')).toBe('true');
    expect(text()).toBe(1);
    fireEvent.click(less);
    expect(text()).toBe(1);
  });

  it('wraps a long unbroken word inside the brief box', async () => {
    await renderAssembly({ item: { ...assembly, body: 'x'.repeat(300) } });

    // jsdom cannot measure layout: the wrap class stands in for it.
    expect(screen.getByText(/^x+$/).className).toContain('[overflow-wrap:anywhere]');
  });

  it('holds a long collapsed brief to the column width', async () => {
    await renderAssembly({ item: { ...assembly, body: 'y'.repeat(120) + ' tail '.repeat(60) } });

    const text = screen.getByText(/^y{120}/);
    expect(text.className).toContain('[overflow-wrap:anywhere]');
    const box = text.closest('.motion-disclosure');
    expect(box?.className).toContain('grid-cols-[minmax(0,1fr)]');
    expect(box?.className).toContain('min-w-0');
    expect(box?.parentElement?.className).toContain('w-full min-w-0');
  });

  it('shows a short brief whole, with no button', async () => {
    await renderAssembly();

    expect(screen.queryByRole('button', { name: /Show (more|less)/ })).toBeNull();
  });

  it('shows a done assembly with its parts view and none of the actions', async () => {
    await renderAssembly({ item: { ...assembly, state: 'done' } });

    expect(screen.getByRole('region', { name: 'Progress' })).toBeTruthy();
    expect(screen.getByRole('table')).toBeTruthy();
    expect(screen.getByRole('region', { name: 'Plan' })).toBeTruthy();
    for (const name of ['Add a part', 'Pause', 'Resume', 'Cancel assembly']) {
      expect(screen.queryByRole('button', { name })).toBeNull();
    }
  });
});

describe('AssemblyPage progress', () => {
  it('counts the merged parts, shows the budget and the human touches', async () => {
    await renderAssembly();

    expect(
      within(screen.getByRole('region', { name: 'Progress' })).getByRole('img', { name: '1 of 4 parts merged' }),
    ).toBeTruthy();
    const strip = within(screen.getByRole('region', { name: 'Assembly figures' }));
    for (const label of ['Lead time', 'Spend', 'Human touches', 'Time to finish']) {
      expect(strip.getByText(label)).toBeTruthy();
    }
    expect(stat('Lead time')).toContain('3 d');
    // The assembly's $0.50 plus the parts' $2, $1, $1 and $1; the per-item budget is one task's, so no target.
    expect(stat('Spend')).toContain('$5.50');
    expect(stat('Spend')).not.toContain('of $');
    expect(stat('Human touches')).toContain('3');
    // Merged parts took 2 days; the two waves still open take 2 days each.
    expect(stat('Time to finish')).toContain('About 4 d');
  });

  it('shows no budget when the factory sets none', async () => {
    const unbudgeted: Item = {
      ...assembly,
      flow: { ...flow, budget: { per_item_usd: null, max_turns_per_task: null } },
    };
    await renderAssembly({ item: unbudgeted });

    expect(stat('Spend')).toContain('$5.50');
    expect(stat('Spend')).not.toContain('of $');
  });
});

/** Renders the assembly with part 3 holding waiting. */
async function renderWaits(waiting: AssemblyPart['waiting']) {
  await renderAssembly({
    parts: { ...parts, parts: parts.parts.map((p) => (p.index === 3 ? { ...p, waiting_on: [], waiting } : p)) },
  });
}

/** The Plan card's row of the part titled title. */
function planRow(title: string): HTMLElement {
  const plan = screen.getAllByRole('region').find((r) => r.getAttribute('aria-label')?.startsWith('Wave'));
  const link = within(plan?.parentElement ?? document.body)
    .getAllByRole('link', { name: title })
    .find((l) => l.closest('section[aria-label^="Wave"]'));
  const row = link?.closest('li');
  if (!row) throw new Error(`no plan row for ${title}`);
  return row;
}

describe('AssemblyPage plan', () => {
  it('groups parts into waves: a work row names the station and role, waiting rows name their needs', async () => {
    await renderAssembly();

    const wave1 = screen.getByRole('region', { name: 'Wave 1' });
    expect(within(wave1).getByRole('link', { name: 'Part 2 title' }).getAttribute('href')).toBe(
      '/o/acme/f/goal/items/42',
    );
    expect(within(wave1).getByText('Build, Infra engineer')).toBeTruthy();

    const wave2 = screen.getByRole('region', { name: 'Wave 2' });
    expect(within(wave2).getByText('Waiting for goal#41, goal#42').getAttribute('title')).toBe(
      'Waiting for goal#41, goal#42',
    );
    expect(within(wave2).getByRole('link', { name: 'Part 3 title' })).toBeTruthy();
  });

  it('gives every part title link a title attribute with the whole title', async () => {
    const long = 'A very long part title that the row has to truncate rather than spill into the pull request link';
    await renderAssembly({
      parts: { ...parts, parts: parts.parts.map((p) => (p.index === 3 ? { ...p, title: long } : p)) },
    });

    const links = screen.getAllByRole('link', { name: long });
    expect(links.length).toBe(2);
    for (const link of links) expect(link.getAttribute('title')).toBe(long);
  });

  it('shows a part waiting for CI with its counts and an ⓘ in its row', async () => {
    await renderWaits({
      kind: 'ci',
      items: [],
      files: [],
      needs: [],
      ci: { commits: [{ repo: 'github.com/acme/goal', sha: 'abc1234' }], checks: [], finished: 9, total: 14 },
    });

    const row = planRow('Part 3 title');
    expect(within(row).getByText('Waiting for CI · 9 of 14')).toBeTruthy();
    expect(within(row).getByRole('button', { name: 'About CI' })).toBeTruthy();
  });

  it('shows a part waiting for its merge turn in its row', async () => {
    await renderWaits({
      kind: 'turn',
      items: ['goal#41'],
      files: [],
      needs: [{ ref: 'goal#41', title: 'Part 1 title', paths: [] }],
      target: 'goal/a40-split',
    });

    expect(within(planRow('Part 3 title')).getByText('Waits for goal#41 to land')).toBeTruthy();
  });

  it('shows a part waiting for its needs in its row', async () => {
    await renderWaits({
      kind: 'after',
      items: ['goal#41', 'goal#42'],
      files: [],
      needs: [
        { ref: 'goal#41', title: 'Part 1 title', paths: [] },
        { ref: 'goal#42', title: 'Part 2 title', paths: [] },
      ],
    });

    expect(within(planRow('Part 3 title')).getByText('Waits for goal#41 +1')).toBeTruthy();
  });

  it('keeps the state name of a part that waits for nothing', async () => {
    await renderAssembly({
      parts: { ...parts, parts: [...parts.parts, part(5, { wave: 0, state: 'ready' })] },
    });

    const row = planRow('Part 5 title');
    expect(within(row).getByText('Ready')).toBeTruthy();
    expect(within(row).queryByText(/Wait/)).toBeNull();
  });

  it('shows a merged part as a row, not as work', async () => {
    await renderAssembly();

    const wave1 = screen.getByRole('region', { name: 'Wave 1' });
    expect(within(wave1).getByText('Merged')).toBeTruthy();
  });
});

describe('AssemblyPage parts table', () => {
  it('lists every part in plan order, each linking to its own item page', async () => {
    await renderAssembly();

    const table = screen.getByRole('table', { name: 'Parts of goal#40, in plan order' });
    expect(within(table).getAllByRole('row').slice(1)).toHaveLength(4);
    const first = within(table).getByRole('link', { name: 'Part 1 title' });
    expect(first.getAttribute('href')).toBe('/o/acme/f/goal/items/41');
    expect(first.closest('tr')?.textContent).toContain('1 of 4');
    expect(within(table).getByRole('link', { name: 'PR #101' }).getAttribute('href')).toBe(
      'https://github.com/acme/goal/pull/101',
    );
    const third = within(table).getByRole('link', { name: 'Part 3 title' });
    expect(third.getAttribute('href')).toBe('/o/acme/f/goal/items/43');
    expect(third.closest('tr')?.textContent).toContain('3 of 4');
  });
});

/** The parts with every open one paused (blocked), as a pause leaves them. */
const pausedParts: AssemblyParts = {
  ...parts,
  parts: parts.parts.map((p) => (p.state === 'done' ? p : { ...p, state: 'blocked' as const })),
};

describe('AssemblyPage actions', () => {
  it('Pause calls pause at once, with no dialog', async () => {
    await renderAssembly();

    fireEvent.click(screen.getByRole('button', { name: 'Pause' }));

    await waitFor(() => expect(pauseItem).toHaveBeenCalledWith('org-1', 'goal#40', undefined));
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('Resume shows when every open part is paused, and calls retry', async () => {
    await renderAssembly({ parts: pausedParts });

    expect(screen.queryByRole('button', { name: 'Pause' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Resume' }));

    await waitFor(() => expect(retryItem).toHaveBeenCalledWith('org-1', 'goal#40'));
    expect(pauseItem).not.toHaveBeenCalled();
  });

  it('hides Pause, Resume and Cancel assembly from a caller who may do none of them', async () => {
    await renderAssembly({ factory: { ...factory, can: [] } });

    expect(screen.queryByRole('button', { name: 'Pause' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Cancel assembly' })).toBeNull();
  });

  it('Resume and Cancel assembly need item:decide, Pause only item:operate', async () => {
    await renderAssembly({ parts: pausedParts, factory: { ...factory, can: ['item:operate'] } });

    expect(screen.queryByRole('button', { name: 'Resume' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Cancel assembly' })).toBeNull();
  });

  it('hides both actions on a cancelled assembly', async () => {
    await renderAssembly({ item: { ...assembly, state: 'cancelled' } });

    expect(screen.queryByRole('button', { name: 'Pause' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Cancel assembly' })).toBeNull();
  });

  it('the cancel dialog names the open parts and confirms with cancel', async () => {
    await renderAssembly();

    fireEvent.click(screen.getByRole('button', { name: 'Cancel assembly' }));
    const dialog = screen.getByRole('dialog', {
      name: 'Cancel this assembly? Its 3 open parts are cancelled too.',
    });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel assembly' }));

    await waitFor(() => expect(cancelItem).toHaveBeenCalledWith('org-1', 'goal#40'));
  });

  it('names a single open part in the singular', async () => {
    const oneOpen: AssemblyParts = {
      ...parts,
      parts: parts.parts.map((p) => (p.id === 'part-2' ? p : { ...p, state: 'done' as const })),
    };
    await renderAssembly({ parts: oneOpen });

    fireEvent.click(screen.getByRole('button', { name: 'Cancel assembly' }));

    expect(
      screen.getByRole('dialog', { name: 'Cancel this assembly? Its 1 open part is cancelled too.' }),
    ).toBeTruthy();
  });

  it('Esc closes the cancel dialog and cancels nothing', async () => {
    await renderAssembly();

    fireEvent.click(screen.getByRole('button', { name: 'Cancel assembly' }));
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(cancelItem).not.toHaveBeenCalled();
  });

  it('shows a 409 refusal inline in the dialog and keeps it open', async () => {
    vi.mocked(cancelItem).mockRejectedValueOnce(
      new ApiError({ title: 'Conflict', status: 409, code: 'item-closed', detail: 'goal#40 is done' }),
    );
    await renderAssembly();

    fireEvent.click(screen.getByRole('button', { name: 'Cancel assembly' }));
    const dialog = screen.getByRole('dialog');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel assembly' }));

    expect((await within(dialog).findByRole('alert')).textContent).toBe('goal#40 is done');
    expect(screen.getByRole('dialog')).toBeTruthy();
  });
});

describe('AssemblyPage Add a part action', () => {
  const creator: Factory = { ...factory, can: ['item:create', 'item:operate', 'item:decide'] };

  it('shows Add a part first among the header actions, before Pause', async () => {
    await renderAssembly({ factory: creator });

    const add = screen.getByRole('button', { name: 'Add a part' });
    const pause = screen.getByRole('button', { name: 'Pause' });
    expect(add.compareDocumentPosition(pause) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('hides Add a part from a caller whose factory role cannot create items', async () => {
    await renderAssembly({ factory: { ...creator, can: ['item:operate', 'item:decide'] } });

    expect(screen.queryByRole('button', { name: 'Add a part' })).toBeNull();
  });

  it('hides Add a part on a cancelled assembly', async () => {
    await renderAssembly({ factory: creator, item: { ...assembly, state: 'cancelled' } });

    expect(screen.queryByRole('button', { name: 'Add a part' })).toBeNull();
  });

  it('shows Pause as busy while the request is pending and sends it once', async () => {
    vi.mocked(pauseItem).mockImplementationOnce(() => new Promise(() => {}));
    await renderAssembly();

    await expectBusyOnClick(screen.getByRole('button', { name: 'Pause' }), 'Pausing…');

    expect(pauseItem).toHaveBeenCalledTimes(1);
  });
});
