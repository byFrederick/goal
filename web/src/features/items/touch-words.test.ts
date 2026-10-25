import { describe, expect, it } from 'vitest';
import { touchMeta, touchSentence } from './touch-words';

describe('touchSentence', () => {
  it('says what the person did, with where to and why when the event says', () => {
    expect(touchSentence({ kind: 'moved' }, { to: 'expedite-review' })).toBe('moved it to Expedite review');
    expect(touchSentence({ kind: 'rejected' }, { reason: '  No   evidence ' })).toBe('sent it back: “No evidence”');
    expect(touchSentence({ kind: 'note' }, { text: 'x'.repeat(200) })).toBe(`left a note: “${'x'.repeat(119)}…”`);
    expect(touchSentence({ kind: 'merged' })).toBe('merged the pull request');
    expect(touchSentence({ kind: 'moved' })).toBe('moved it');
  });

  it('names kinds it has no sentence for by their words', () => {
    expect(touchSentence({ kind: 'cancelled' })).toBe('cancelled');
  });
});

describe('touchMeta', () => {
  it('says the station, the kind and whether the line required it', () => {
    expect(touchMeta({ kind: 'gate_approved', station: 'review', planned: true })).toEqual([
      'Review',
      'passed a gate',
      'planned',
    ]);
    expect(touchMeta({ kind: 'note', station: null, planned: false })).toEqual(['No station', 'note', 'unplanned']);
  });
});
