// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { validationQuery } from '../../../api/queries';
import type { Validation } from '../../../api/types';
import type { LineSection, ModelStation } from '../../../shared/definition/model';
import { TooltipProvider } from '../../../ui/tooltip';
import { DraftSession } from '../draft/session';
import { DraftSessionContext } from '../draft/use-draft';
import { LineChecks } from './line-checks';
import { type LineEditor, LineEditorContext } from './line-editor';
import { StationDnd } from './station-dnd';
import { StationRow } from './station-row';

afterEach(cleanup);

const stations: ModelStation[] = [
  { id: 'inbox', name: 'Inbox', kind: 'queue' },
  { id: 'build', name: 'Build', kind: 'agent', agent: 'engineer', wip: 2 },
  { id: 'done', name: 'Done', kind: 'done' },
];

function renderLine(validation?: Validation) {
  const line: LineSection = { stations };
  const move = vi.fn();
  const insert = vi.fn();
  const editor: LineEditor = {
    place: 'definition',
    viewOnly: false,
    line,
    stations,
    agents: ['engineer'],
    workTypes: [],
    hasRepository: true,
    constraintNow: undefined,
    constraint: undefined,
    board: undefined,
    slots: undefined,
    isNew: () => false,
    baseOf: () => undefined,
    keyOf: (id: string) => id,
    renamed: () => {},
    edit: () => {},
    flush: () => {},
    move,
    insert,
    announcement: '',
    announce: () => {},
    lastFocused: { current: undefined },
  };
  const session = new DraftSession({ org: 'acme', factory: 'f1', refresh: () => {}, closed: () => {} });
  const client = new QueryClient();
  if (validation) client.setQueryData(validationQuery('acme', 'f1', 0).queryKey, validation);
  window.scrollTo = () => {};
  window.matchMedia = ((query: string) =>
    ({ matches: false, media: query }) as MediaQueryList) as typeof window.matchMedia;
  const root = createRootRoute({
    component: () => (
      <QueryClientProvider client={client}>
        <TooltipProvider>
          <DraftSessionContext value={session}>
            <LineEditorContext value={editor}>
              <StationDnd>
                <ol aria-label="Stations, in order">
                  {stations.map((s, i) => (
                    <StationRow key={s.id} station={s} index={i} selected={false} />
                  ))}
                </ol>
              </StationDnd>
              <LineChecks />
            </LineEditorContext>
          </DraftSessionContext>
        </TooltipProvider>
      </QueryClientProvider>
    ),
  });
  const route = createRoute({
    getParentRoute: () => root,
    path: '/o/$org/f/$factory/definition/$section',
    component: () => null,
  });
  const router = createRouter({
    routeTree: root.addChildren([route]),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  });
  render(<RouterProvider router={router} />);
  return { move, insert };
}

describe('the stations table', () => {
  it('shows one row per station with its name, id, kind and WIP limit', async () => {
    renderLine();
    const list = await screen.findByRole('list', { name: 'Stations, in order' });
    const rows = within(list).getAllByRole('listitem', { name: /station$/ });
    expect(rows).toHaveLength(3);
    expect(rows[1]?.getAttribute('aria-label')).toBe('2. Build, agent station');
    expect((screen.getByRole('textbox', { name: 'Name of station 2' }) as HTMLInputElement).value).toBe('Build');
    expect(screen.getByText('id: build')).toBeTruthy();
    expect(screen.getByRole('radio', { name: 'Agent', checked: true })).toBeTruthy();
    expect((screen.getByRole('textbox', { name: 'WIP limit of Build' }) as HTMLInputElement).value).toBe('2');
    expect(screen.getAllByText('Not limited')).toHaveLength(2);
  });

  it('keeps each station’s rules behind a collapsed disclosure', async () => {
    renderLine();
    const toggle = await screen.findByRole('button', { name: 'Rules of Build' });
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    fireEvent.click(toggle);
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
  });

  it('moves a station with its arrow button', async () => {
    const { move } = renderLine();
    fireEvent.click(await screen.findByRole('button', { name: 'Move Build up' }));
    expect(move).toHaveBeenCalledWith('build', 0);
  });

  it('moves a station with Alt+ArrowDown on its row', async () => {
    const { move } = renderLine();
    const name = await screen.findByRole('textbox', { name: 'Name of station 1' });
    fireEvent.keyDown(name, { key: 'ArrowDown', altKey: true });
    expect(move).toHaveBeenCalledWith('inbox', 1);
  });

  it('offers Insert above, Insert below, Duplicate and Delete in the More menu', async () => {
    renderLine();
    const trigger = await screen.findByRole('button', { name: 'More for Build' });
    await userEvent.click(trigger);
    expect(await screen.findByRole('menuitem', { name: 'Insert above' })).toBeTruthy();
    for (const name of ['Insert below', 'Duplicate', 'Delete…']) {
      expect(screen.getByRole('menuitem', { name })).toBeTruthy();
    }
  });
});

describe('Checks on this line', () => {
  const validation = {
    checks: [
      { id: 'order', section: 'line', status: 'passed', summary: 'Stations are in order' },
      { id: 'wip', section: 'line', status: 'failed', summary: 'Build has no WIP limit' },
      { id: 'other', section: 'agents', status: 'passed', summary: 'Agents are fine' },
    ],
    problems: [],
    counts: {},
  } as unknown as Validation;

  it('follows the stations table and lists the line’s checks with their icons', async () => {
    renderLine(validation);
    const list = await screen.findByRole('list', { name: 'Stations, in order' });
    const checks = screen.getByRole('region', { name: 'Checks on this line' });
    expect(list.compareDocumentPosition(checks) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    const items = within(checks).getAllByRole('listitem');
    expect(within(items[0] as HTMLElement).getByRole('img', { name: 'Passed' })).toBeTruthy();
    expect(within(items[0] as HTMLElement).getByText('Stations are in order')).toBeTruthy();
    expect(within(items[1] as HTMLElement).getByRole('img', { name: 'Problem' })).toBeTruthy();
    expect(within(items[1] as HTMLElement).getByText('Build has no WIP limit')).toBeTruthy();
    expect(screen.queryByText('Agents are fine')).toBeNull();
  });
});
