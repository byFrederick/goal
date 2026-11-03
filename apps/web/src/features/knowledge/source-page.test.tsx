// @vitest-environment jsdom
import { cleanup, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { factoriesQuery, knowledgeFilesQuery, knowledgeReadsQuery, knowledgeSourceQuery } from '../../api/queries';
import type { Factory, KnowledgeFile, KnowledgeReadCount } from '../../api/types';
import { SourcePage } from './source-page';
import { renderKnowledge, sourceOf, summary } from './test-harness';

const draft = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }));
vi.mock('../../api/definitions', () => ({ getDraftFile: draft.get, putDraftFile: draft.put }));
vi.mock('../../app/top-bar', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../app/top-bar')>()),
  TopBarSlot: ({ actions }: { actions?: React.ReactNode }) => <section aria-label="Top bar">{actions}</section>,
}));

beforeEach(() => {
  vi.spyOn(globalThis, 'fetch').mockImplementation(() => new Promise(() => {}));
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.clearAllMocks();
});

const factories = [
  { id: 'f-sre', name: 'SRE' },
  { id: 'f-support', name: 'Support' },
] as unknown as Factory[];

const runbooks = sourceOf({
  name: 'runbooks',
  kind: 'repository',
  secret: 'github-token',
  config: { repository: 'https://github.com/acme/ops', include: 'docs/runbooks/**' },
  latest_snapshot: { files: 2, failed: 1, bytes: 7000, synced_at: '2026-10-27T14:00:00Z' },
  used_by: {
    factories: [{ id: 'f-sre', name: 'SRE' }],
    readers: [{ id: 'f-sre', name: 'SRE', agents: ['investigator'], stations: [], lanes: [], work_types: [] }],
  },
});
const files: KnowledgeFile[] = [
  { source: 'runbooks', path: 'db/disk-full.md', title: 'Disk full', bytes: 4200, version: 'a' },
  { source: 'runbooks', path: 'k8s/crashloop.md', title: 'Crash loops', bytes: 2800, version: 'b' },
];
const reads: KnowledgeReadCount[] = [
  {
    source: 'runbooks',
    path: 'db/disk-full.md',
    factory_id: 'f-sre',
    factory: 'SRE',
    day: '2026-10-27',
    reads: 14,
    searches: 0,
  },
];

function renderSource(can?: string[]) {
  return renderKnowledge({
    page: SourcePage,
    route: '$source',
    location: '/o/acme/knowledge/runbooks',
    seed: [
      [knowledgeSourceQuery(summary.id, 'runbooks').queryKey, runbooks],
      [knowledgeFilesQuery(summary.id, 'runbooks', '').queryKey, files],
      [knowledgeReadsQuery(summary.id, 'runbooks').queryKey, reads],
      [factoriesQuery(summary.id).queryKey, factories],
    ],
    ...(can ? { can } : {}),
  });
}

describe('the knowledge source page', () => {
  it('shows the files with their reads, the latest sync, the settings, the line agents see and who reads it', async () => {
    renderSource();

    const table = await screen.findByRole('table', { name: 'Files of runbooks' });
    const row = within(table).getByText('db/disk-full.md').closest('tr') as HTMLElement;
    expect(within(row).getByText('4.1 KiB')).toBeTruthy();
    expect(within(row).getByText('14')).toBeTruthy();

    const sync = screen.getByRole('region', { name: 'Latest sync' });
    expect(sync.textContent).toContain('2 files, 6.8 KiB.');
    expect(sync.textContent).toContain('1 file failed and kept their last good version');

    expect(screen.getByDisplayValue('Read before acting on an alert.')).toBeTruthy();
    const line = screen.getByRole('region', { name: 'What agents see' });
    expect(line.textContent).toContain('/knowledge/runbooks — Read before acting on an alert. 2 files: db/, k8s/.');

    const readBy = screen.getByRole('table', { name: /Factories and agents that read runbooks/ });
    expect(within(readBy).getByText('SRE')).toBeTruthy();
    expect(within(readBy).getByText('investigator')).toBeTruthy();
  });

  it('adds the source to a factory’s draft from Add to a factory, and never publishes', async () => {
    draft.get.mockResolvedValue({ text: 'spec:\n  knowledge:\n    sources: []\n', sha256: 'abc' });
    draft.put.mockResolvedValue({});
    renderSource();

    await userEvent.click(await screen.findByRole('button', { name: 'Add to a factory' }));
    // SRE reads it already, so only Support is offered.
    expect(screen.queryByRole('checkbox', { name: /SRE/ })).toBeNull();
    await userEvent.click(screen.getByRole('checkbox', { name: /Support/ }));
    await userEvent.click(screen.getByRole('button', { name: 'Add to drafts' }));

    await waitFor(() => expect(draft.put).toHaveBeenCalledTimes(1));
    expect(draft.put.mock.calls[0]?.slice(0, 3)).toEqual(['org-1', 'f-support', 'factory.yaml']);
    expect(draft.put.mock.calls[0]?.[3]).toContain('runbooks');
  });

  it('hides Add to a factory from someone who cannot manage knowledge', async () => {
    renderSource(['knowledge:read']);
    await screen.findByRole('table', { name: 'Files of runbooks' });
    expect(screen.queryByRole('button', { name: 'Add to a factory' })).toBeNull();
  });
});
