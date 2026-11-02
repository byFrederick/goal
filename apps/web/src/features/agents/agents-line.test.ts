import { describe, expect, it } from 'vitest';
import type { ModelAgent, ModelStation } from '../../shared/definition/model';
import { laneLabel, staffingOf } from './agents-line';

const agents = ['a', 'b', 'c'].map((name) => ({ name })) as ModelAgent[];

describe('staffingOf', () => {
  it('gives one place per agent at a routed station, with every lane it takes', () => {
    const station = {
      id: 'build',
      kind: 'agent',
      route: { backend: 'a', frontend: 'b', infra: 'c', '': 'b' },
    } as unknown as ModelStation;

    const places = staffingOf(station, agents);

    expect(places.map((p) => p.agent.name)).toEqual(['a', 'b', 'c']);
    expect(places.map((p) => p.key)).toEqual(['build/a', 'build/b', 'build/c']);
    expect(places[1]?.lanes).toEqual(['frontend', '']);
  });
});

describe('laneLabel', () => {
  it('names the empty lane "no lane"', () => {
    expect(laneLabel(['frontend', ''])).toBe('frontend, no lane');
  });
});
