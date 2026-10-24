import { describe, expect, test } from 'vitest';
import { factorySignal, noAttention } from './attention';

describe('factorySignal', () => {
  test('names a quiet factory by its mark and name, and shows its title', () => {
    expect(factorySignal({ name: 'goal', title: 'Goal' }, 'G', noAttention)).toEqual({
      label: 'G, goal factory',
      detail: 'Goal',
    });
  });

  test('says what waits on you', () => {
    expect(factorySignal({ name: 'sre' }, 'S', { ...noAttention, waiting: 3 })).toEqual({
      label: 'S, sre factory, 3 waiting on you',
      detail: '3 waiting on you',
    });
  });

  test('says when the line is stopped, or items are blocked', () => {
    expect(factorySignal({ name: 'support' }, 'Su', { ...noAttention, andon: 'Bad answers' }).label).toBe(
      'Su, support factory, blocked: andon pulled',
    );
    expect(factorySignal({ name: 'goal' }, 'G', { ...noAttention, blocked: 1 }).label).toBe(
      'G, goal factory, 1 item blocked',
    );
    expect(factorySignal({ name: 'goal' }, 'G', { ...noAttention, blocked: 2 }).detail).toBe('2 items blocked');
  });

  test('says both when work waits and is blocked', () => {
    expect(factorySignal({ name: 'goal' }, 'G', { waiting: 2, blocked: 1, andon: null })).toEqual({
      label: 'G, goal factory, 2 waiting on you, 1 item blocked',
      detail: '2 waiting on you, 1 item blocked',
    });
  });
});
