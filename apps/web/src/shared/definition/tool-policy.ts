// What an agent may call, in Claude Code's permission syntax, as goald hands it to the worker
// (internal/config: Builtins.rules): an allowed built-in group's tools are allowed, every other
// group's are denied, and Ask me is served as Off until tool approvals arrive. Run commands with a
// command list allows Bash(<command>:*) for each. A legacy `tools` block is shown as written.
import type { BuiltinKey, Builtins, ModelAgent } from './model';

/**
 * The worker's tools for an agent whose definition allows none: it can read the workspace. The
 * editor's first built-in choice starts from these.
 */
export const defaultBuiltins: Builtins = { read: 'allowed' };

/** Each built-in group's name in the editor and on the agent page. */
export const builtinLabels: Record<BuiltinKey, string> = {
  read: 'Read files',
  edit: 'Edit files',
  run: 'Run commands',
  web: 'Fetch web pages',
  subagents: 'Start subagents',
};

const groups: { key: Exclude<BuiltinKey, 'run'>; tools: string[] }[] = [
  { key: 'read', tools: ['Read', 'Grep', 'Glob'] },
  { key: 'edit', tools: ['Edit', 'Write'] },
  { key: 'web', tools: ['WebFetch', 'WebSearch'] },
  { key: 'subagents', tools: ['Task'] },
];

export interface LegacyRules {
  allow: string[];
  deny: string[];
}

export interface ToolPolicy {
  allowed: string[];
  denied: string[];
  /** The built-in groups set to Ask me, which run as Off until tool approvals arrive. */
  asked: BuiltinKey[];
}

export function toolPolicy(agent: ModelAgent, legacy?: LegacyRules): ToolPolicy {
  if (agent.custom_tools) return { allowed: legacy?.allow ?? [], denied: legacy?.deny ?? [], asked: [] };
  const b = agent.builtins ?? defaultBuiltins;
  const policy: ToolPolicy = { allowed: [], denied: [], asked: [] };
  for (const g of groups) {
    if (b[g.key] === 'allowed') policy.allowed.push(...g.tools);
    else policy.denied.push(...g.tools);
    if (b[g.key] === 'ask') policy.asked.push(g.key);
  }
  const run = b.run;
  if (run?.mode !== 'allowed') policy.denied.push('Bash');
  else if (!run.commands?.length) policy.allowed.push('Bash');
  else policy.allowed.push(...run.commands.map((c) => `Bash(${c}:*)`));
  if (run?.mode === 'ask') policy.asked.push('run');
  return policy;
}

const strings = (v: unknown): string[] => (Array.isArray(v) ? v.filter((s): s is string => typeof s === 'string') : []);

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null;

/**
 * An agent's legacy `tools` block, from factory.yaml's text (the model only says it has one).
 * The YAML parser loads on first use: only agents with such a block need it.
 */
export async function legacyRules(factoryYaml: string, agent: string): Promise<LegacyRules | undefined> {
  const { parse } = await import('yaml');
  let doc: unknown;
  try {
    doc = parse(factoryYaml);
  } catch {
    return undefined;
  }
  const spec = isObject(doc) ? doc.spec : undefined;
  const agents = isObject(spec) && Array.isArray(spec.agents) ? spec.agents : [];
  const entry: unknown = agents.find((a: unknown) => isObject(a) && a.name === agent);
  const tools = isObject(entry) ? entry.tools : undefined;
  return isObject(tools) ? { allow: strings(tools.allow), deny: strings(tools.deny) } : undefined;
}
