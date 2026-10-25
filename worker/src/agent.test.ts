import { describe, expect, it } from 'vitest';
import { agentQuery } from './agent.js';
import { parseSpec } from './spec.js';

const base = {
  factory_id: 'f1',
  item_id: 'i1',
  run_id: 'r1',
  station: 'build',
  agent: 'engineer',
  skills: [],
  tools_allow: [],
  tools_deny: [],
  brief: 'Add a health endpoint',
};

function systemPromptOf(purpose?: string): string {
  const spec = parseSpec(JSON.stringify(purpose === undefined ? base : { ...base, purpose }));
  const { options } = agentQuery(spec, { prompt: 'You are the engineer.' }, {}, new AbortController());
  const system = options.systemPrompt;
  if (typeof system !== 'object' || system === null || !('append' in system)) throw new Error('no appended prompt');
  return system.append ?? '';
}

describe('agentQuery', () => {
  it("puts the factory's purpose before the agent's prompt", () => {
    expect(systemPromptOf('Builds and reviews changes to the acme shop.')).toBe(
      '## This factory\nBuilds and reviews changes to the acme shop.\n\nYou are the engineer.',
    );
  });

  it("puts goald's feedback for this attempt (GOAL_FEEDBACK) right after the brief", () => {
    const spec = parseSpec(JSON.stringify(base));
    const feedback = 'Your previous attempt was rejected: `task check` exited 1.';
    const { prompt } = agentQuery(spec, { prompt: 'p' }, { GOAL_FEEDBACK: feedback }, new AbortController());
    expect(prompt.startsWith(`Add a health endpoint\n\n${feedback}\n\n## Finishing`)).toBe(true);
    expect(agentQuery(spec, { prompt: 'p' }, {}, new AbortController()).prompt).not.toContain('rejected');
  });

  it('appends only the prompt when the factory has no purpose', () => {
    expect(systemPromptOf()).toBe('You are the engineer.');
    expect(systemPromptOf('')).toBe('You are the engineer.');
  });
});
