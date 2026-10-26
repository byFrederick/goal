import { describe, expect, it } from 'vitest';
import type { LineSection } from './model';
import { lineSlots, slotCap, slotsSentence } from './slots';

describe('slots at full WIP', () => {
  it('counts the agents each agent station can run at once', () => {
    const line: LineSection = {
      stations: [
        { id: 'inbox', kind: 'queue', wip: 9 },
        { id: 'spec', kind: 'agent', agent: 'a', wip: 2 }, // one agent, whatever the WIP
        { id: 'build', kind: 'agent', agent: 'b', agents: 3, wip: 2 }, // WIP caps the agents
        { id: 'review', kind: 'agent', agent: 'c', agents: 2 }, // no WIP: every agent
        { id: 'approve', kind: 'human', wip: 4 },
        { id: 'done', kind: 'done' },
      ],
    };
    expect(lineSlots(line)).toBe(1 + 2 + 2);
    expect(lineSlots({})).toBe(0);
  });

  it("takes the factory's cap, else the organization's, else none", () => {
    const slots = {
      max: 6,
      factories: [
        { factory_id: 'f1', name: 'goal', max: 3 },
        { factory_id: 'f2', name: 'sre', max: null },
      ],
    };
    expect(slotCap(slots, 'f1')).toEqual({ cap: 3, scope: 'factory' });
    expect(slotCap(slots, 'f2')).toEqual({ cap: 6, scope: 'org' });
    expect(slotCap({ max: null, factories: [] }, 'f2')).toBeUndefined();
    expect(slotCap(undefined, 'f1')).toBeUndefined();
  });

  it('says how much of the cap the line uses', () => {
    expect(slotsSentence(5, 6)).toBe('5 of your 6 Claude slots at full WIP');
    expect(slotsSentence(1, 1)).toBe('1 of your 1 Claude slot at full WIP');
    expect(slotsSentence(8, 6)).toBe('8 agents at full WIP, more than your 6 Claude slots');
  });
});
