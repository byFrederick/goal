// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { KnowledgeCheck, KnowledgeCheckInput } from '../../api/types';
import { CheckPanel } from './check-panel';

const check = vi.hoisted(() => vi.fn());
vi.mock('../../api/knowledge', () => ({ checkKnowledgeSource: check }));
vi.mock('../../shared/org', () => ({ useOrg: () => ({ id: 'org-1', slug: 'acme', name: 'Acme' }) }));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

const limits = { files: 20000, bytes: 50 * 1024 * 1024, file_bytes: 5 * 1024 * 1024 };
// The settings as typed on the page, not saved.
const input: KnowledgeCheckInput = {
  name: 'runbooks',
  kind: 'repository',
  secret: 'github-token',
  config: { repository: 'https://github.com/acme/ops', branch: 'main' },
  read_when: 'Before acting on an alert.',
  factories: ['f-sre'],
};

function renderPanel(value: KnowledgeCheckInput | null) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <CheckPanel input={value} />
    </QueryClientProvider>,
  );
}

describe('the Check panel', () => {
  it('posts the unsaved settings and shows access, size against the limits, each warning with its factory and the agent line', async () => {
    const answer: KnowledgeCheck = {
      access: { ok: true },
      files: 212,
      first_files: ['db/disk.md', 'k8s/pods.md'],
      bytes: 3 * 1024 * 1024,
      limits,
      over_limit: ['files'],
      warnings: [{ code: 'knowledge-copies-checkout', factory: 'SRE', message: 'SRE already checks out acme/ops.' }],
      line: '/knowledge/runbooks — Before acting on an alert. 212 files: db/, k8s/.',
    };
    check.mockResolvedValue(answer);
    renderPanel(input);

    await userEvent.click(screen.getByRole('button', { name: 'Check' }));

    await screen.findByText('Access works');
    expect(check).toHaveBeenCalledWith('org-1', input);
    expect(screen.getByText(/212 files, 3\.0 MiB/)).toBeTruthy();
    expect(screen.getByText(/Limits: 20,000 files, 50 MiB, 5\.0 MiB a file/)).toBeTruthy();
    expect(screen.getByText('Over the file limit')).toBeTruthy();
    expect(screen.getByText('SRE: SRE already checks out acme/ops.')).toBeTruthy();
    expect(screen.getByText(answer.line)).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Check again' })).toBeTruthy();
  });

  it('says why the source could not be read and shows no size', async () => {
    check.mockResolvedValue({
      access: { ok: false, error: 'the token was refused' },
      files: 0,
      first_files: [],
      limits,
      over_limit: [],
      warnings: [],
      line: '/knowledge/runbooks',
    } satisfies KnowledgeCheck);
    renderPanel(input);

    await userEvent.click(screen.getByRole('button', { name: 'Check' }));

    await screen.findByText('Couldn’t read the source');
    expect(screen.getByText('the token was refused')).toBeTruthy();
    expect(screen.queryByText(/Limits:/)).toBeNull();
  });

  it('cannot check settings that are not complete yet', async () => {
    renderPanel(null);
    expect((screen.getByRole('button', { name: 'Check' }) as HTMLButtonElement).disabled).toBe(true);
    await waitFor(() => expect(check).not.toHaveBeenCalled());
  });

  describe('First files', () => {
    const base: KnowledgeCheck = {
      access: { ok: true },
      files: 212,
      first_files: ['db/disk.md', 'k8s/pods.md'],
      limits,
      over_limit: [],
      warnings: [],
      line: '/knowledge/runbooks — x.',
    };

    it('lists the paths and says how many more there are', async () => {
      check.mockResolvedValue(base);
      renderPanel(input);
      await userEvent.click(screen.getByRole('button', { name: 'Check' }));
      const card = await screen.findByRole('region', { name: 'First files' });
      expect(card.textContent).toContain('db/disk.md');
      expect(card.textContent).toContain('k8s/pods.md');
      expect(card.textContent).toContain('and 210 more');
    });

    it('says nothing more when the list holds every file', async () => {
      check.mockResolvedValue({ ...base, files: 2 });
      renderPanel(input);
      await userEvent.click(screen.getByRole('button', { name: 'Check' }));
      const card = await screen.findByRole('region', { name: 'First files' });
      expect(card.textContent).not.toContain('more');
    });

    it('is hidden when access failed or the list is empty', async () => {
      check.mockResolvedValueOnce({ ...base, access: { ok: false, error: 'No' } });
      renderPanel(input);
      await userEvent.click(screen.getByRole('button', { name: 'Check' }));
      await screen.findByText('Couldn’t read the source');
      expect(screen.queryByRole('region', { name: 'First files' })).toBeNull();
      check.mockResolvedValueOnce({ ...base, first_files: [] });
      await userEvent.click(screen.getByRole('button', { name: 'Check again' }));
      await screen.findByText('Access works');
      expect(screen.queryByRole('region', { name: 'First files' })).toBeNull();
    });

    it('shows its skeleton while the check runs', async () => {
      check.mockReturnValue(new Promise(() => {}));
      renderPanel(input);
      await userEvent.click(screen.getByRole('button', { name: 'Check' }));
      const card = await screen.findByRole('region', { name: 'First files' });
      expect(card.querySelector('[aria-busy="true"]')).not.toBeNull();
    });
  });
});
