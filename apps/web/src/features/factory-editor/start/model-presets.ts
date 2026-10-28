// The two model presets of the new-factory wizard's Line step (ADR 0022). A preset is a one-time edit of the
// draft, never a definition field: the two model ids below are the only place they are written. Changing an id is an
// amendment to ADR 0022.
import type { AgentsSection } from '../../../shared/definition/model';
import { type Edit, setAgentField, setDefaultModel } from '../draft/ops';

export const presetModels = {
  quality: 'claude-opus-5-5',
  cost: 'claude-sonnet-5-5',
} as const;

export type PresetId = keyof typeof presetModels;

export const presets: readonly { id: PresetId; label: string }[] = [
  { id: 'quality', label: 'Quality first' },
  { id: 'cost', label: 'Cost first' },
];

/**
 * The agents section after a preset is chosen. Quality first sets the default model and keeps each agent's model;
 * Cost first sets the cheaper default and removes every agent's model, so each agent runs on the default.
 */
export function presetEdit(agents: AgentsSection, preset: PresetId): Edit<'agents'> {
  let next = setDefaultModel(agents, presetModels[preset]).value;
  if (preset === 'cost') {
    for (const agent of agents.agents ?? []) {
      if (agent.model !== undefined) next = setAgentField(next, agent.name, 'model', undefined).value;
    }
  }
  return { section: 'agents', value: next };
}

/** The preset the draft is exactly, or null: a hand-set default model matches neither. */
export function presetOf(agents: AgentsSection): PresetId | null {
  if (agents.default_model === presetModels.quality) return 'quality';
  const overridden = (agents.agents ?? []).some((agent) => agent.model !== undefined);
  if (agents.default_model === presetModels.cost && !overridden) return 'cost';
  return null;
}

/** The choice to press: presetOf, and Quality first while the draft names no default model (goald's default applies). */
export function pressedPreset(agents: AgentsSection): PresetId | null {
  return presetOf(agents) ?? (agents.default_model === undefined ? 'quality' : null);
}
