import { describe, expect, it } from 'vitest';
import type { AgentsSection } from '../../../shared/definition/model';
import { presetEdit, presetOf, pressedPreset } from './model-presets';

// Two agents, one with its own model: Quality first keeps it, Cost first removes it.
const draft: AgentsSection = {
  default_model: 'claude-haiku-5-5',
  agents: [{ name: 'architect', model: 'claude-haiku-5-5' }, { name: 'reviewer' }],
};

describe('model presets', () => {
  it('Quality first sets the default model and keeps each agent override', () => {
    const next = presetEdit(draft, 'quality');
    expect(next.section).toBe('agents');
    expect(next.value.default_model).toBe('claude-opus-5-5');
    expect(next.value.agents).toEqual([{ name: 'architect', model: 'claude-haiku-5-5' }, { name: 'reviewer' }]);
  });

  it('Cost first sets the cheaper default and removes every agent model', () => {
    const next = presetEdit(draft, 'cost');
    expect(next.value.default_model).toBe('claude-sonnet-5-5');
    expect(next.value.agents).toEqual([{ name: 'architect' }, { name: 'reviewer' }]);
    expect(next.value.agents?.every((agent) => !('model' in agent))).toBe(true);
  });

  it('does not change the draft it is given', () => {
    presetEdit(draft, 'cost');
    expect(draft.default_model).toBe('claude-haiku-5-5');
    expect(draft.agents?.[0]?.model).toBe('claude-haiku-5-5');
  });

  it('presetOf recognises both results', () => {
    expect(presetOf(presetEdit(draft, 'quality').value)).toBe('quality');
    expect(presetOf(presetEdit(draft, 'cost').value)).toBe('cost');
  });

  it('presetOf returns null for a hand-set default and for Cost first with an override left', () => {
    expect(presetOf(draft)).toBeNull();
    expect(presetOf({ ...draft, default_model: 'claude-sonnet-5-5' })).toBeNull();
  });

  it('presents Quality first while the draft names no default model, and nothing for a hand-set one', () => {
    expect(pressedPreset({ agents: [] })).toBe('quality');
    expect(pressedPreset(draft)).toBeNull();
  });
});
