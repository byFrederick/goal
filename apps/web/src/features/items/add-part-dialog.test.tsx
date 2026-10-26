// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { toast } from 'sonner';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, addItemPart } from '../../api/client';
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
import { renderItemPage } from '../../test/route';
import { AssemblyPage } from './assembly-page';

vi.mock(import('../../api/client'), async (original) => ({
  ...(await original()),
  addItemPart: vi.fn(async () => parts),
}));

const success = vi.spyOn(toast, 'success');

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
  role: 'editor',
  can: ['item:create', 'item:operate', 'item:decide'],
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
  title: 'Split the assembly page',
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
  touches: { count: 0, planned: 0, unplanned: 0, list: [] },
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
    station: 'architect',
    state: 'queued',
    needs: [],
    waiting_on: [],
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
  total: 3,
  lead_time_seconds: 3600,
  parts: [
    part(1, { state: 'done', done_at: '2026-10-22T00:00:00Z' }),
    part(2, { state: 'in_progress', agent: 'infra-engineer' }),
    part(3, { wave: 1, waiting_on: ['goal#42'] }),
  ],
};

const lanes: BoardLane[] = [
  { key: 'infra-engineer', kind: 'agent', role: 'Infra engineer', model: null, model_label: null, slots: 1, busy: 0 },
  { key: 'reviewer', kind: 'agent', role: 'Reviewer', model: null, model_label: null, slots: 1, busy: 0 },
] as BoardLane[];
const board = { stations: [{ id: 'architect', items: [], lanes }] } as unknown as Board;

/** Renders the Assembly page with its header, then opens Add a part. */
async function openDialog() {
  renderItemPage({ page: () => <AssemblyPage />, org, factory, item: assembly, parts, board });
  await screen.findByRole('heading', { level: 1 });
  fireEvent.click(screen.getByRole('button', { name: 'Add a part' }));
  return screen.getByRole('dialog', { name: 'Add a part' });
}

/** The text of the message the field's aria-describedby names, or undefined when it names none. */
function describedText(control: HTMLElement): string | undefined {
  const ids = control.getAttribute('aria-describedby')?.split(' ') ?? [];
  return ids
    .map((id) => document.getElementById(id)?.textContent)
    .find((t) => t === 'Enter a title' || t === 'Enter a brief');
}

describe('Add a part dialog', () => {
  it('requires a title and a brief, and sends nothing until both are given', async () => {
    const dialog = await openDialog();

    fireEvent.click(within(dialog).getByRole('button', { name: 'Add part' }));

    const title = within(dialog).getByLabelText('Title');
    const brief = within(dialog).getByLabelText('Brief');
    expect(within(dialog).getByText('Enter a title')).toBeTruthy();
    expect(within(dialog).getByText('Enter a brief')).toBeTruthy();
    expect(title.getAttribute('aria-invalid')).toBe('true');
    expect(brief.getAttribute('aria-invalid')).toBe('true');
    expect(describedText(title)).toBe('Enter a title');
    expect(describedText(brief)).toBe('Enter a brief');
    expect(addItemPart).not.toHaveBeenCalled();
  });

  it('lists every existing part by title and ref, and sends the keys of the ones checked', async () => {
    const dialog = await openDialog();

    for (const n of [1, 2, 3]) {
      expect(within(dialog).getByText(`Part ${n} title`)).toBeTruthy();
      expect(within(dialog).getByText(`goal#${40 + n}`)).toBeTruthy();
    }
    fireEvent.click(within(dialog).getByRole('checkbox', { name: /Part 3 title/ }));
    fireEvent.click(within(dialog).getByRole('checkbox', { name: /Part 1 title/ }));
    fireEvent.change(within(dialog).getByLabelText('Title'), { target: { value: 'Warm the cache' } });
    fireEvent.change(within(dialog).getByLabelText('Brief'), { target: { value: 'Keep it warm.' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add part' }));

    await waitFor(() =>
      expect(addItemPart).toHaveBeenCalledWith('org-1', 'goal#40', {
        title: 'Warm the cache',
        brief: 'Keep it warm.',
        needs: ['p1', 'p3'],
      }),
    );
  });

  it('offers the lanes of the parts and the station, and sends the one picked', async () => {
    const dialog = await openDialog();

    const lane = within(dialog).getByLabelText('Lane');
    const options = within(lane)
      .getAllByRole('option')
      .map((o) => o.textContent);
    expect(options).toEqual(['Any agent', 'Infra engineer', 'Reviewer']);
    fireEvent.change(lane, { target: { value: 'reviewer' } });
    fireEvent.change(within(dialog).getByLabelText('Title'), { target: { value: 'Review the cache' } });
    fireEvent.change(within(dialog).getByLabelText('Brief'), { target: { value: 'Read it twice.' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add part' }));

    await waitFor(() =>
      expect(addItemPart).toHaveBeenCalledWith('org-1', 'goal#40', {
        title: 'Review the cache',
        brief: 'Read it twice.',
        lane: 'reviewer',
      }),
    );
  });

  it('shows a 409 refusal inline, keeps the dialog open and says nothing by toast', async () => {
    vi.mocked(addItemPart).mockRejectedValueOnce(
      new ApiError({ title: 'Conflict', status: 409, code: 'edge-cycle', detail: 'goal#41 would wait for itself' }),
    );
    const dialog = await openDialog();
    fireEvent.change(within(dialog).getByLabelText('Title'), { target: { value: 'Loop' } });
    fireEvent.change(within(dialog).getByLabelText('Brief'), { target: { value: 'Loops.' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add part' }));

    expect((await within(dialog).findByRole('alert')).textContent).toBe('goal#41 would wait for itself');
    expect(screen.getByRole('dialog', { name: 'Add a part' })).toBeTruthy();
    expect(success).not.toHaveBeenCalled();
  });

  it('closes on success, says so, and leaves no error behind', async () => {
    const dialog = await openDialog();
    fireEvent.change(within(dialog).getByLabelText('Title'), { target: { value: 'Ship the cache' } });
    fireEvent.change(within(dialog).getByLabelText('Brief'), { target: { value: 'Warm it.' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add part' }));

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(success).toHaveBeenCalledWith('Added Ship the cache.');
  });

  it('Esc closes the dialog without a request', async () => {
    const dialog = await openDialog();

    fireEvent.keyDown(dialog, { key: 'Escape' });

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(addItemPart).not.toHaveBeenCalled();
  });
});
