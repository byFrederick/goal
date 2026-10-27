import { describe, expect, it } from 'vitest';
import type { ActivityEvent } from '../../api/types';
import { activityWords, actorRole, avatarOf } from './activity-words';

const event = (over: Partial<ActivityEvent>): ActivityEvent => ({
  id: 1,
  item_id: 'item-146',
  at: '2026-10-26T14:32:00Z',
  kind: 'merged',
  station: 'review',
  actor: 'reviewer',
  data: {},
  ref: 'goal#146',
  number: 146,
  title: 'Ship the retry fix',
  ...over,
});

describe('actorRole', () => {
  it('drops the copy number so an agent is named by its role', () => {
    expect(actorRole('frontend-engineer-2')).toBe('frontend-engineer');
    expect(actorRole('reviewer')).toBe('reviewer');
  });

  it('names nobody for the engine or a missing actor', () => {
    expect(actorRole('engine')).toBeNull();
    expect(actorRole('')).toBeNull();
  });
});

describe('activityWords', () => {
  it('says who merged an item and where', () => {
    expect(activityWords(event({}))).toEqual({ head: 'goal#146 merged', rest: ' by reviewer at Review' });
  });

  it('never shows a copy number', () => {
    const { head, rest } = activityWords(event({ actor: 'frontend-engineer-3', kind: 'created', station: 'build' }));
    expect(`${head}${rest}`).toBe('goal#146 created by frontend-engineer at Build');
  });

  it('names the station an item moved to', () => {
    const words = activityWords(event({ kind: 'moved', actor: 'engine', data: { from: 'spec', to: 'build' } }));
    expect(words).toEqual({ head: 'goal#146 moved', rest: ' to Build' });
  });

  it('reads a sent-back event with no actor as plain words', () => {
    expect(activityWords(event({ kind: 'gate_rejected', actor: 'engine', station: 'review' }))).toEqual({
      head: 'goal#146 sent back',
      rest: ' at Review',
    });
  });

  it('falls back to the kind for a kind this build does not know', () => {
    expect(activityWords(event({ kind: 'future_thing', actor: 'engine', station: '' }))).toEqual({
      head: 'goal#146 future thing',
      rest: '',
    });
  });
});

describe('avatarOf', () => {
  it('uses the initials of a two-word role and the first two letters of one word', () => {
    expect(avatarOf('frontend-engineer-2')).toBe('FE');
    expect(avatarOf('reviewer')).toBe('RE');
    expect(avatarOf('engine')).toBe('G');
  });
});
