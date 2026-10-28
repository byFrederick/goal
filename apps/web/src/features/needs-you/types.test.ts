import { describe, expect, it } from 'vitest';
import type { InboxEntry } from '../../api/types';
import { inTab, typeOf } from './types';

const kind = (k: string) => ({ kind: k }) as Pick<InboxEntry, 'kind'>;

describe('the type of an entry', () => {
  it('shows a decision as a Decision', () => {
    expect(typeOf(kind('decision'))).toBe('decision');
  });

  it('shows a question as a Question', () => {
    expect(typeOf(kind('question'))).toBe('question');
  });

  it('shows a tool approval as an Approval', () => {
    expect(typeOf(kind('tool_approval'))).toBe('approval');
  });

  it.each(['andon', 'rework_warning', 'blocked', 'expedite'])('shows %s as an Alert', (k) => {
    expect(typeOf(kind(k))).toBe('alert');
  });

  it('shows a kind this build does not know as a generic row', () => {
    expect(typeOf(kind('sync_failed'))).toBe('generic');
  });
});

describe('which tab an entry shows under', () => {
  it('shows every entry, generic ones included, under All', () => {
    for (const k of ['decision', 'andon', 'tool_approval', 'sync_failed']) expect(inTab(kind(k), 'all')).toBe(true);
  });

  it('shows a generic row under no type tab', () => {
    for (const tab of ['decision', 'alert', 'approval', 'question'] as const) {
      expect(inTab(kind('sync_failed'), tab)).toBe(false);
    }
  });

  it('shows Decisions and Alerts under their own tab only', () => {
    expect(inTab(kind('decision'), 'decision')).toBe(true);
    expect(inTab(kind('decision'), 'alert')).toBe(false);
    expect(inTab(kind('blocked'), 'alert')).toBe(true);
    expect(inTab(kind('blocked'), 'decision')).toBe(false);
  });

  it('shows a question under Questions only', () => {
    expect(inTab(kind('question'), 'question')).toBe(true);
    expect(inTab(kind('question'), 'decision')).toBe(false);
    expect(inTab(kind('question'), 'alert')).toBe(false);
    expect(inTab(kind('question'), 'all')).toBe(true);
  });

  it('shows a tool approval under Approvals and All only', () => {
    expect(inTab(kind('tool_approval'), 'approval')).toBe(true);
    expect(inTab(kind('tool_approval'), 'decision')).toBe(false);
    expect(inTab(kind('tool_approval'), 'alert')).toBe(false);
    expect(inTab(kind('tool_approval'), 'all')).toBe(true);
  });
});
