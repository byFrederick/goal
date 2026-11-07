// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { getVersion, getVersionFile } from '../../api/definitions';
import { LineStateChip } from './line-state-chip';

vi.mock('../../api/definitions', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/definitions')>()),
  getVersion: vi.fn(),
  getVersionFile: vi.fn(),
}));

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

function renderChip(size: 'md' | 'lg' = 'md', runningText: 'Running' | 'Line running' = 'Running') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <LineStateChip org="org-1" factory="f-1" size={size} runningText={runningText} />
    </QueryClientProvider>,
  );
}

describe('LineStateChip', () => {
  it('shows only a toneless placeholder while the version reads are pending', () => {
    vi.mocked(getVersion).mockReturnValue(new Promise(() => {}));
    const { container } = renderChip();

    expect(screen.queryByText('Running')).toBeNull();
    expect(screen.queryByText('Needs a model key')).toBeNull();
    expect(container.querySelector('.bg-ok')).toBeNull();
    expect(screen.getByText('Loading')).toBeTruthy();
  });

  it('says Needs a model key for a line with an environment file and no runtime.modelKey', async () => {
    vi.mocked(getVersion).mockResolvedValue({
      files: [
        { path: 'factory.yaml', sha256: 'sha-f', size: 1 },
        { path: 'environments/default.yaml', sha256: 'sha-d', size: 1 },
      ],
    } as never);
    vi.mocked(getVersionFile).mockResolvedValue({ text: 'spec:\n  runtime: {}\n' } as never);
    renderChip();

    expect(await screen.findByText('Needs a model key')).toBeTruthy();
    expect(screen.queryByText('Running')).toBeNull();
  });

  it('says Running for any other line, with the text it was given', async () => {
    vi.mocked(getVersion).mockResolvedValue({ files: [{ path: 'factory.yaml', sha256: 'sha-f', size: 1 }] } as never);
    vi.mocked(getVersionFile).mockResolvedValue({ text: 'spec: {}\n' } as never);
    renderChip('lg', 'Line running');

    expect(await screen.findByText('Line running')).toBeTruthy();
  });

  it('says Running when a read fails, and logs it once across re-renders', async () => {
    const log = vi.spyOn(console, 'error').mockImplementation(() => {});
    vi.mocked(getVersion).mockRejectedValue(new Error('goald is down'));
    const { rerender } = renderChip();

    expect(await screen.findByText('Running')).toBeTruthy();
    rerender(<div />);
    expect(log).toHaveBeenCalledTimes(1);
    log.mockRestore();
  });
});
