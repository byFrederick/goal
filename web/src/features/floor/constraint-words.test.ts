import { describe, expect, it } from 'vitest';
import type { ConstraintCandidate } from '../../api/types';
import { addLabel, busyOver, estimateSentence, headline, waitingSentence } from './constraint-words';

const investigate: ConstraintCandidate = {
  station: 'investigate',
  slots: 3,
  agents: ['investigator'],
  busy_ratio: 0.94,
  avg_waiting: 2.2,
  mean_wait_minutes: 95,
  mean_service_minutes: 50,
  leases: 120,
  estimate: { extra_slot_saves_hours: 2.1 },
};

describe('constraint card words', () => {
  it('heads the constraint as holding the line back, the other candidates by their place', () => {
    expect(headline(investigate, 0, 'investigate')).toBe('Investigate is holding the line back');
    expect(headline({ ...investigate, station: 'triage' }, 1, 'investigate')).toBe('Triage is the next busiest');
    expect(headline({ ...investigate, station: 'review' }, 2, 'investigate')).toBe('Review is the third busiest');
  });

  it('says how many items wait for the station’s agent on average', () => {
    expect(waitingSentence(investigate)).toBe('On average two items wait for an investigator.');
    expect(waitingSentence({ ...investigate, avg_waiting: 0.8 })).toBe(
      'On average one item waits for an investigator.',
    );
    expect(waitingSentence({ ...investigate, avg_waiting: 0.2 })).toBe('Almost nothing waits in front of it.');
    expect(waitingSentence({ ...investigate, agents: ['backend', 'frontend'], avg_waiting: 3 })).toBe(
      'On average three items wait for an agent.',
    );
  });

  it('says what one more slot would save only when there is an estimate', () => {
    expect(estimateSentence(investigate)).toBe('A fourth investigator would cut lead time by about 2 hours.');
    expect(estimateSentence({ ...investigate, slots: 1, estimate: { extra_slot_saves_hours: 0.6 } })).toBe(
      'A second investigator would cut lead time by about 36 minutes.',
    );
    expect(estimateSentence({ ...investigate, estimate: null })).toBeNull();
  });

  it('names the agent to add and the stretch measured', () => {
    expect(addLabel(investigate)).toBe('Add an investigator');
    expect(addLabel({ ...investigate, agents: ['reviewer'] })).toBe('Add a reviewer');
    expect(addLabel({ ...investigate, agents: [] })).toBe('Add an agent');
    expect(busyOver('2026-10-17', '2026-10-23')).toBe('7 days');
    expect(busyOver('2026-10-23', '2026-10-23')).toBe('1 day');
  });
});
