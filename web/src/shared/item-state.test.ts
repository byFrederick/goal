import { describe, expect, it } from 'vitest';
import {
  controlsFor,
  decisionsFor,
  describeState,
  itemStatus,
  pausedNow,
  pullRequestLabel,
  pullRequestsOf,
  waitingOnYou,
} from './item-state';

const none = { approve: false, reject: false, merge: false, retry: false };

describe('decisionsFor', () => {
  it('offers approve and send back for an item waiting at a human station', () => {
    expect(decisionsFor({ state: 'waiting_human' }, { kind: 'human' })).toEqual({
      ...none,
      approve: true,
      reject: true,
    });
  });

  it('offers merge instead of approve at a human station whose action is merge', () => {
    expect(decisionsFor({ state: 'waiting_human' }, { kind: 'human', action: 'merge' })).toEqual({
      ...none,
      merge: true,
      reject: true,
    });
  });

  it('offers only retry for a blocked item, and nothing while agents work', () => {
    expect(decisionsFor({ state: 'blocked' }, { kind: 'agent' })).toEqual({ ...none, retry: true });
    expect(decisionsFor({ state: 'in_progress' }, { kind: 'agent' })).toEqual(none);
    expect(decisionsFor({ state: 'waiting_human' }, { kind: 'agent' })).toEqual(none);
    expect(decisionsFor({ state: 'waiting_human' }, undefined)).toEqual(none);
  });
});

describe('controlsFor', () => {
  it('pauses and moves any open item, and sends back what waits, works or is blocked', () => {
    expect(controlsFor({ state: 'in_progress' }, 'triage')).toEqual({ pause: true, sendBack: true, move: true });
    expect(controlsFor({ state: 'waiting_human' }, 'triage')).toEqual({ pause: true, sendBack: true, move: true });
    expect(controlsFor({ state: 'blocked' }, 'triage')).toEqual({ pause: false, sendBack: true, move: true });
    expect(controlsFor({ state: 'queued' }, 'triage')).toEqual({ pause: true, sendBack: false, move: true });
  });

  it('sends back only where there is somewhere to send it, and controls nothing closed', () => {
    expect(controlsFor({ state: 'in_progress' }, null).sendBack).toBe(false);
    expect(controlsFor({ state: 'done' }, 'triage')).toEqual({ pause: false, sendBack: false, move: false });
  });
});

describe('pausedNow', () => {
  it('is true while the latest block is a person pausing the item', () => {
    expect(pausedNow('blocked', [{ kind: 'blocked' }, { kind: 'retried' }, { kind: 'paused' }])).toBe(true);
    expect(pausedNow('blocked', [{ kind: 'paused' }, { kind: 'retried' }, { kind: 'blocked' }])).toBe(false);
    expect(pausedNow('queued', [{ kind: 'paused' }])).toBe(false);
  });
});

describe('waitingOnYou', () => {
  it('counts items waiting for a person at human stations only', () => {
    const items = [
      { state: 'waiting_human' as const },
      { state: 'queued' as const },
      { state: 'waiting_human' as const },
    ];
    expect(waitingOnYou({ kind: 'human', items })).toBe(2);
    expect(waitingOnYou({ kind: 'agent', items })).toBe(0);
  });
});

describe('describeState', () => {
  it('says who is working, who it waits on, and when it was sent back', () => {
    expect(describeState({ state: 'in_progress', agent: 'infra-engineer', rework_count: 0 })).toEqual({
      text: 'Infra engineer working',
      tone: 'run',
    });
    expect(describeState({ state: 'waiting_human', agent: null, rework_count: 0 }).tone).toBe('you');
    expect(describeState({ state: 'queued', agent: null, rework_count: 2 })).toEqual({
      text: 'Sent back, waiting',
      tone: 'rework',
    });
    expect(describeState({ state: 'ready', agent: null, rework_count: 0 }, { atConstraint: true }).text).toBe(
      'Ready in buffer',
    );
  });

  it('waits on you only where you decide', () => {
    const waiting = { state: 'waiting_human', agent: null, rework_count: 0 } as const;
    expect(describeState(waiting, { decides: true })).toEqual({ text: 'Waiting on you', tone: 'you' });
    expect(describeState(waiting, { decides: false })).toEqual({ text: 'Waiting on an approver', tone: 'muted' });
  });
});

describe('itemStatus', () => {
  it('shows working for an item in progress and a check for one that is ready or done', () => {
    expect(itemStatus({ state: 'in_progress' })).toBe('working');
    expect(itemStatus({ state: 'ready' })).toBe('ready');
    expect(itemStatus({ state: 'ready', in_buffer: false })).toBe('ready');
    expect(itemStatus({ state: 'done' })).toBe('ready');
  });

  it('shows a clock for an item that waits for another item or on a person', () => {
    expect(itemStatus({ state: 'queued' })).toBe('waiting');
    expect(itemStatus({ state: 'ready', in_buffer: true })).toBe('waiting');
    expect(itemStatus({ state: 'waiting_human' })).toBe('waiting');
  });

  it('shows no icon for a blocked, cancelled or assembly item, which its words say', () => {
    expect(itemStatus({ state: 'blocked' })).toBeNull();
    expect(itemStatus({ state: 'cancelled' })).toBeNull();
    expect(itemStatus({ state: 'assembly' })).toBeNull();
  });

  it('shows two crossing arrows for a queued or ready item that waits for the same files', () => {
    expect(itemStatus({ state: 'queued', waiting: { kind: 'files' } })).toBe('same-files');
    expect(itemStatus({ state: 'ready', in_buffer: false, waiting: { kind: 'files' } })).toBe('same-files');
  });

  it('shows a clock for a queued or ready item that starts after another item merges', () => {
    expect(itemStatus({ state: 'queued', waiting: { kind: 'after' } })).toBe('waiting');
    expect(itemStatus({ state: 'ready', waiting: { kind: 'after' } })).toBe('waiting');
  });

  it('keeps the icon of an item with no waiting reason as before', () => {
    expect(itemStatus({ state: 'queued', waiting: null })).toBe('waiting');
    expect(itemStatus({ state: 'ready', waiting: null })).toBe('ready');
  });
});

describe('pullRequestsOf', () => {
  const line = ['spec', 'build', 'review', 'merge'];

  it('takes every pull request the outputs name, station by station in line order, each once', () => {
    const outputs = {
      review: { decision: 'approve', pull_request: { html_url: 'https://github.com/acme/goal/pull/8' } },
      build: {
        pull_request: 'https://github.com/acme/goal/pull/7',
        pull_requests: [
          'https://gitlab.example.com/acme/infra/-/merge_requests/3',
          'https://github.com/acme/goal/pull/7',
        ],
      },
      spec: { summary: 'x' },
    };
    expect(pullRequestsOf(outputs, line)).toEqual([
      'https://github.com/acme/goal/pull/7',
      'https://gitlab.example.com/acme/infra/-/merge_requests/3',
      'https://github.com/acme/goal/pull/8',
    ]);
  });

  it('is empty when no output names a pull request', () => {
    expect(pullRequestsOf({ spec: { summary: 'x', pull_request: 'not a url', pull_requests: [3] } }, line)).toEqual([]);
  });
});

describe('pullRequestLabel', () => {
  it('names GitHub pull requests and GitLab merge requests, with their repository when asked', () => {
    expect(pullRequestLabel('https://github.com/acme/goal/pull/41')).toBe('PR #41');
    expect(pullRequestLabel('https://gitlab.example.com/acme/platform/infra/-/merge_requests/3', true)).toBe(
      'acme/platform/infra MR !3',
    );
    expect(pullRequestLabel('https://example.com/change/9')).toBe('Pull request');
  });
});
