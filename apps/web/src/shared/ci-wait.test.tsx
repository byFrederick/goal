// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ItemWaiting } from '../api/types';
import { CIWait } from './ci-wait';

afterEach(cleanup);

const repo = 'github.com/acme/app';
const waiting: ItemWaiting = {
  kind: 'ci',
  items: [],
  files: [],
  needs: [],
  ci: {
    commits: [{ repo, sha: '0123456789abcdef', url: 'https://github.com/acme/app/pull/7' }],
    checks: [
      {
        repo,
        name: 'build',
        state: 'finished',
        result: 'passed',
        url: 'https://ci.example.com/build',
        failed_jobs: [],
      },
      { repo, name: 'task web:e2e', state: 'running', failed_jobs: [] },
    ],
    finished: 1,
    total: 2,
  },
};

describe('CIWait', () => {
  it('shows the label, and behind About CI each repository with its commit and checks linked', async () => {
    render(<CIWait waiting={waiting} />);
    expect(screen.getByText('Waiting for CI · 1 of 2')).toBeTruthy();
    await userEvent.click(screen.getByRole('button', { name: 'About CI' }));
    const group = await screen.findByRole('group', { name: repo });
    expect(within(group).getByRole('link', { name: '0123456' }).getAttribute('href')).toBe(
      'https://github.com/acme/app/pull/7',
    );
    expect(within(group).getByRole('link', { name: 'build' }).getAttribute('href')).toBe(
      'https://ci.example.com/build',
    );
    expect(within(group).getByText('Passed')).toBeTruthy();
    // A check without a url is plain text.
    expect(within(group).queryByRole('link', { name: 'task web:e2e' })).toBeNull();
    expect(within(group).getByText('task web:e2e')).toBeTruthy();
    expect(within(group).getByText('Running')).toBeTruthy();
  });

  it('lists the failure as its own group beside the checks of the open commit', async () => {
    const base = waiting.ci as NonNullable<ItemWaiting['ci']>;
    const both: ItemWaiting = {
      ...waiting,
      ci: {
        ...base,
        failure: {
          repo,
          sha: 'fedcba9876543210',
          url: 'https://github.com/acme/app/pull/6',
          checks: [
            {
              repo,
              name: 'task web:e2e',
              state: 'finished',
              result: 'failed',
              url: 'https://ci.example.com/e2e-failed',
              failed_jobs: ['3/12'],
            },
          ],
        },
      },
    };
    render(<CIWait waiting={both} />);
    expect(screen.getByText('CI failed: task web:e2e (3/12)')).toBeTruthy();
    await userEvent.click(screen.getByRole('button', { name: 'About CI' }));
    const failed = await screen.findByRole('group', { name: `${repo}, failed` });
    expect(within(failed).getByRole('link', { name: 'fedcba9' }).getAttribute('href')).toBe(
      'https://github.com/acme/app/pull/6',
    );
    expect(within(failed).getByRole('link', { name: 'task web:e2e' }).getAttribute('href')).toBe(
      'https://ci.example.com/e2e-failed',
    );
    expect(within(failed).getByText('Failed')).toBeTruthy();
    expect(within(failed).getByText('(3/12)')).toBeTruthy();
    const open = screen.getByRole('group', { name: repo });
    expect(within(open).getByRole('link', { name: 'build' })).toBeTruthy();
  });

  it('shows a commit without a url as plain text', async () => {
    const bare: ItemWaiting = {
      ...waiting,
      ci: { ...(waiting.ci as NonNullable<ItemWaiting['ci']>), commits: [{ repo, sha: 'abcdef0123' }] },
    };
    render(<CIWait waiting={bare} />);
    await userEvent.click(screen.getByRole('button', { name: 'About CI' }));
    const group = await screen.findByRole('group', { name: repo });
    expect(within(group).getByText('abcdef0')).toBeTruthy();
    expect(within(group).queryByRole('link', { name: 'abcdef0' })).toBeNull();
  });

  it('stops pointerdown and keydown at the ⓘ, so a card is not dragged or moved', () => {
    const outer = vi.fn();
    render(
      // biome-ignore lint/a11y/noStaticElementInteractions: stands in for the card
      <div onPointerDown={outer} onKeyDown={outer}>
        <CIWait waiting={waiting} />
      </div>,
    );
    const button = screen.getByRole('button', { name: 'About CI' });
    fireEvent.pointerDown(button);
    fireEvent.keyDown(button, { key: 'ArrowRight' });
    expect(outer).not.toHaveBeenCalled();
  });
});
