import { describe, expect, it } from 'vitest';
import type { WebhookDelivery } from '../../api/webhooks';
import { evaluationText, nameProblem, pingText, receiptNote, receiptOf, statusOf, weekText } from './words';

describe('statusOf', () => {
  it('says Receiving only while a trigger names the webhook', () => {
    expect(statusOf({ enabled: true, triggers: [{ factory: 'f', factory_name: 'f', trigger: 't' }] }).label).toBe(
      'Receiving',
    );
    expect(statusOf({ enabled: true, triggers: [] }).label).toBe('No trigger yet');
    expect(statusOf({ enabled: false, triggers: [] }).label).toBe('Switched off');
  });
});

describe('weekText', () => {
  it('sums delivered and refused over the week', () => {
    const days = [{ day: 'd', delivered: 1000, refused: 4 }];
    expect(weekText({ days: days as never })).toBe('1,004 this week');
    expect(weekText({ days: [] })).toBe('None yet');
  });
});

describe('pingText', () => {
  it('speaks of GitHub’s ping for GitHub only', () => {
    expect(pingText({ sender: 'gitlab', pinged_at: null })).toBeUndefined();
    expect(pingText({ sender: 'github', pinged_at: null })).toBe('Waiting for GitHub’s ping');
    expect(pingText({ sender: 'github', pinged_at: '2026-11-03T08:55:00' })).toBe(
      'Received GitHub’s ping on Nov 3 at 08:55',
    );
  });
});

describe('the delivery log’s words', () => {
  const base: WebhookDelivery = {
    id: 'd',
    received_at: '',
    outcome: 'accepted',
    reason: '',
    item: null,
    event: '',
    receipt: 'accepted',
    duplicates: 0,
    evaluations: [],
  };

  it('reads a delivery kept before the receiver from its outcome', () => {
    expect(receiptOf({ ...base, receipt: null }).label).toBe('Accepted');
    expect(receiptOf({ ...base, outcome: 'refused', receipt: null }).label).toBe('Refused');
  });

  it('notes why it was refused and how many times it was sent', () => {
    expect(receiptNote({ ...base, outcome: 'refused', receipt: 'refused', reason: 'signature did not match' })).toBe(
      'signature did not match',
    );
    expect(receiptNote({ ...base, receipt: 'accepted', duplicates: 2 })).toBe('Sent 3 times');
  });

  it('says what a factory made of it', () => {
    const e = { factory: 'f', factory_name: 'f', item: null } as const;
    expect(evaluationText({ ...e, state: 'done', outcome: 'skipped', reason: 'not acme/goal' }).text).toBe(
      'Skipped: not acme/goal',
    );
    expect(evaluationText({ ...e, state: 'failed', outcome: 'failed', reason: 'no station' }).text).toBe(
      'Failed: no station',
    );
    expect(evaluationText({ ...e, state: 'pending', outcome: null, reason: '' }).kind).toBe('pending');
  });
});

describe('nameProblem', () => {
  it('accepts a slug and refuses the rest', () => {
    expect(nameProblem('github-acme')).toBeUndefined();
    expect(nameProblem('')).toBeTruthy();
    expect(nameProblem('ab')).toBeTruthy();
    expect(nameProblem('Bad Name')).toBeTruthy();
    expect(nameProblem('sentry')).toMatch(/reserved/);
  });
});
