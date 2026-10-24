import { describe, expect, it } from 'vitest';
import type { LineSection } from './model';
import { stationsUsing, whereAgentWorks, whereLabel } from './where';

const line: LineSection = {
  stations: [
    { id: 'inbox', kind: 'queue' },
    { id: 'investigate', name: 'Investigate', kind: 'agent', agent: 'investigator', agents: 2, wip: 2 },
    { id: 'build', name: 'Build', kind: 'agent', route: { backend: 'engineer', frontend: 'web-engineer' } },
    { id: 'review', name: 'Review', kind: 'agent', agent: 'reviewer', gate: { on_reject: 'build', max_rework: 2 } },
    { id: 'done', name: 'Done', kind: 'done' },
  ],
  librarian: 'librarian',
};

describe('where an agent works', () => {
  it('names the station, its parallel count, its lane or its gate', () => {
    expect(whereLabel(line, 'investigator')).toBe('Investigate, × 2');
    expect(whereLabel(line, 'web-engineer')).toBe('Build, frontend lane');
    expect(whereLabel(line, 'reviewer')).toBe('Review, gate');
    expect(whereLabel(line, 'librarian')).toBe('Done');
  });

  it('lists every place, and says so when the agent works nowhere', () => {
    const both: LineSection = {
      stations: [
        { id: 'a', name: 'A', kind: 'agent', agent: 'x' },
        { id: 'b', name: 'B', kind: 'agent', route: { docs: 'x', web: 'x' } },
        { id: 'done', kind: 'done' },
      ],
    };
    expect(whereAgentWorks(both, 'x').map((w) => w.station)).toEqual(['a', 'b']);
    expect(whereLabel(both, 'x')).toBe('A · B, docs and web lanes');
    expect(whereLabel(both, 'nobody')).toBe('Not on the line');
  });

  it('finds the stations that keep an agent from being removed', () => {
    expect(stationsUsing(line, 'engineer').map((s) => s.id)).toEqual(['build']);
    expect(stationsUsing(line, 'librarian').map((s) => s.id)).toEqual(['done']);
    expect(stationsUsing(line, 'unused')).toEqual([]);
  });
});
