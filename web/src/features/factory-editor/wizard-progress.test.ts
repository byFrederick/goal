import { describe, expect, it } from 'vitest';
import type { Model } from '../../shared/definition/model';
import { doneSteps, nextStep, previousStep, readProgress, recordDone, resumeStep } from './wizard-progress';

const empty: Model = {
  basics: { name: 'new-factory' },
  line: { stations: [] },
  agents: { agents: [] },
  connections: [],
  review: {},
};

const fromTemplate: Model = {
  basics: { name: 'cloud-costs', purpose: 'Finds cloud spend waste.' },
  line: {
    stations: [
      { id: 'inbox', kind: 'queue' },
      { id: 'done', kind: 'done' },
    ],
  },
  agents: { agents: [{ name: 'cost-analyst' }] },
  connections: [],
  review: {},
};

function memory(): Storage {
  const m = new Map<string, string>();
  return {
    getItem: (k) => m.get(k) ?? null,
    setItem: (k, v) => void m.set(k, v),
    removeItem: (k) => void m.delete(k),
    clear: () => m.clear(),
    key: () => null,
    length: 0,
  };
}

describe('the wizard’s progress', () => {
  it('counts the steps with content as done when nothing is recorded', () => {
    expect([...doneSteps(null, empty)]).toEqual(['start']);
    expect([...doneSteps(null, fromTemplate)]).toEqual(['start', 'basics', 'line', 'agents']);
  });

  it('prefers the record: a step is done once it was left with Continue', () => {
    const storage = memory();
    expect(readProgress(storage, 'f1')).toBeNull();
    recordDone(storage, 'f1', 'basics');
    expect(readProgress(storage, 'f1')).toEqual(['start', 'basics']);
    expect([...doneSteps(readProgress(storage, 'f1'), fromTemplate)]).toEqual(['start', 'basics']);
    expect(readProgress(storage, 'f2')).toBeNull();
  });

  it('resumes at the first step not done, Review when all others are', () => {
    expect(resumeStep(doneSteps(null, empty))).toBe('basics');
    expect(resumeStep(doneSteps(null, fromTemplate))).toBe('connections');
    expect(resumeStep(new Set(['start', 'basics', 'line', 'agents', 'connections']))).toBe('review');
  });

  it('reads a record it did not write as nothing recorded', () => {
    const storage = memory();
    storage.setItem('goal.wizard.f1', '{not json');
    expect(readProgress(storage, 'f1')).toBeNull();
    storage.setItem('goal.wizard.f1', '["line","nope"]');
    expect(readProgress(storage, 'f1')).toEqual(['line']);
    const refusing = {
      getItem: () => null,
      setItem: () => {
        throw new Error('quota');
      },
    };
    expect(recordDone(refusing, 'f1', 'line')).toEqual(['start', 'line']);
  });

  it('steps forward and back', () => {
    expect(nextStep('start')).toBe('basics');
    expect(nextStep('review')).toBeNull();
    expect(previousStep('basics')).toBe('start');
    expect(previousStep('start')).toBeNull();
  });
});
