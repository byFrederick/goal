// @vitest-environment jsdom
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { LineSection, ModelStation } from '../../../shared/definition/model';
import { DraftSession } from '../draft/session';
import { DraftSessionContext } from '../draft/use-draft';
import { type LineEditor, LineEditorContext } from './line-editor';
import { StationRules } from './station-rules';

afterEach(cleanup);

const review: ModelStation = {
  id: 'review',
  name: 'Review',
  kind: 'agent',
  agent: 'reviewer',
  gate: { on_reject: 'build', max_rework: 2 },
};
const build: ModelStation = { id: 'build', name: 'Build', kind: 'agent', agent: 'engineer' };
const human: ModelStation = { id: 'approve', name: 'Approve', kind: 'human' };

/** Renders one station's detail strip with a line around it; `edit` records every edit made. */
function renderRules(
  station: ModelStation,
  {
    hasRepository = true,
    invalid = () => false,
    describedBy = () => undefined,
  }: {
    hasRepository?: boolean;
    invalid?: (field: string) => boolean;
    describedBy?: (field: string) => string | undefined;
  } = {},
) {
  const line: LineSection = { stations: [build, station, { id: 'done', kind: 'done' }] };
  const edit = vi.fn();
  const editor: LineEditor = {
    place: 'definition',
    viewOnly: false,
    line,
    stations: line.stations ?? [],
    agents: ['engineer', 'reviewer'],
    workTypes: [],
    hasRepository,
    constraintNow: undefined,
    constraint: undefined,
    board: undefined,
    slots: undefined,
    isNew: () => false,
    baseOf: () => undefined,
    keyOf: (id: string) => id,
    renamed: () => {},
    edit,
    flush: () => {},
    move: () => {},
    insert: () => {},
    announcement: '',
    announce: () => {},
    lastFocused: { current: undefined },
  };
  const session = new DraftSession({ org: 'acme', factory: 'factory-1', refresh: () => {}, closed: () => {} });
  const index = line.stations?.indexOf(station) ?? 0;

  // Rows link to Basics, so the detail strip renders inside a router; jsdom has no window.scrollTo.
  window.scrollTo = () => {};
  const root = createRootRoute({
    component: () => (
      <DraftSessionContext value={session}>
        <LineEditorContext value={editor}>
          <StationRules
            station={station}
            index={index}
            invalid={(f) => invalid(f)}
            describedBy={(f) => describedBy(f)}
          />
        </LineEditorContext>
      </DraftSessionContext>
    ),
  });
  const definition = createRoute({
    getParentRoute: () => root,
    path: '/o/$org/f/$factory/definition/$section',
    component: () => null,
  });
  const wizard = createRoute({ getParentRoute: () => root, path: '/o/$org/new/$step', component: () => null });
  const router = createRouter({
    routeTree: root.addChildren([definition, wizard]),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  });
  const rendered = render(<RouterProvider router={router} />);
  return { edit, ...rendered };
}

/** The station the recorded edit leaves in the line, by id. */
function savedStation(edit: ReturnType<typeof vi.fn>, id: string): ModelStation | undefined {
  const [value] = edit.mock.calls.at(-1) as [{ value: LineSection }];
  return value.value.stations?.find((s) => s.id === id);
}

const mergeSwitch = (name: RegExp) => screen.getByRole('switch', { name });

describe('the merge step of an agent station with a gate', () => {
  it('shows the switch on a gate, and says what approving does when it is on', async () => {
    renderRules({ ...review, action: 'merge' });
    expect(await screen.findByText('Approving the gate merges the pull request (no person)')).toBeDefined();
    expect(mergeSwitch(/merge step/i).getAttribute('aria-checked')).toBe('true');
  });

  it('turns merging on for a gate and off again, saving action on the station', async () => {
    const { edit } = renderRules(review);
    expect(await screen.findByText('Approving the gate leaves the pull request open')).toBeDefined();

    fireEvent.click(mergeSwitch(/merge step/i));
    expect(savedStation(edit, 'review')).toMatchObject({ id: 'review', gate: review.gate, action: 'merge' });
  });

  it('removes action when the switch is turned off', async () => {
    const { edit } = renderRules({ ...review, action: 'merge' });
    fireEvent.click(await screen.findByRole('switch', { name: /merge step/i }));
    expect(savedStation(edit, 'review')).toEqual(review);
  });

  it('removes the merge step with the gate, so the draft stays valid', async () => {
    const { edit } = renderRules({ ...review, action: 'merge' });
    fireEvent.click(await screen.findByRole('button', { name: 'Remove the rework rule of Review' }));
    expect(savedStation(edit, 'review')).toEqual({ id: 'review', name: 'Review', kind: 'agent', agent: 'reviewer' });
  });

  it('has no switch on an agent station without a gate', async () => {
    renderRules(build);
    await screen.findByRole('button', { name: 'Add a note to Build' });
    expect(screen.queryByRole('switch', { name: /merge step/i })).toBeNull();
  });
});

describe('the merge step of a human station', () => {
  it('says a person merges when on, and that nothing merges when off', async () => {
    const { edit } = renderRules(human);
    expect(await screen.findByText('People approve here; nothing is merged')).toBeDefined();

    fireEvent.click(mergeSwitch(/merge step/i));
    expect(savedStation(edit, 'approve')).toMatchObject({ id: 'approve', action: 'merge' });
  });

  it('says a person merges the pull request when it is on', async () => {
    renderRules({ ...human, action: 'merge' });
    expect(await screen.findByText('A person merges the pull request')).toBeDefined();
  });
});

describe('the merge step without a repository', () => {
  it('cannot be turned on, and says where the repository is set', async () => {
    const { edit } = renderRules(human, { hasRepository: false });
    expect(await screen.findByRole('link', { name: 'Basics' })).toBeDefined();
    expect(screen.getByText(/Set a repository in/)).toBeDefined();

    const off = mergeSwitch(/merge step/i);
    expect(off.getAttribute('aria-disabled')).toBe('true');
    fireEvent.click(off);
    expect(edit).not.toHaveBeenCalled();
  });

  it('can still be turned off', async () => {
    const { edit } = renderRules({ ...review, action: 'merge' }, { hasRepository: false });
    const on = await screen.findByRole('switch', { name: /merge step/i });
    expect(on.getAttribute('aria-disabled')).toBeNull();
    fireEvent.click(on);
    expect(savedStation(edit, 'review')).toEqual(review);
  });
});

describe('the merge step with a problem', () => {
  it('marks the switch invalid and points it at the problem, on a gate and on a human station', async () => {
    const problems = {
      invalid: (f: string) => f === 'action',
      describedBy: (f: string) => (f === 'action' ? 'problems-7' : undefined),
    };
    const { unmount } = renderRules(review, problems);
    await screen.findByRole('switch', { name: /merge step/i });
    expect(mergeSwitch(/merge step/i).getAttribute('aria-invalid')).toBe('true');
    expect(mergeSwitch(/merge step/i).getAttribute('aria-describedby')).toContain('problems-7');
    unmount();

    renderRules(human, problems);
    await screen.findByRole('switch', { name: /merge step/i });
    expect(mergeSwitch(/merge step/i).getAttribute('aria-invalid')).toBe('true');
    expect(mergeSwitch(/merge step/i).getAttribute('aria-describedby')).toContain('problems-7');
  });
});
