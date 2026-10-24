import { z } from 'zod';

/** Where goald's Docker runner mounts things (internal/adapters/docker). */
export const paths = {
  spec: '/goal/spec.json',
  agent: '/goal/agent',
  workspace: '/workspace',
  /** Claude Code's config dir for this run, in the container's throwaway home; never the host's. */
  claudeConfig: '/home/node/.claude',
} as const;

/** The task as goald writes it to /goal/spec.json. It holds no secrets. */
export const TaskSpec = z.object({
  factory_id: z.string().min(1),
  item_id: z.string().min(1),
  run_id: z.string().min(1),
  station: z.string(),
  agent: z.string(),
  model: z.string().optional(),
  skills: z.array(z.string()),
  tools_allow: z.array(z.string()),
  tools_deny: z.array(z.string()),
  gateway_url: z.url().optional(),
  purpose: z.string().optional(),
  brief: z.string(),
  inputs: z.record(z.string(), z.unknown()).optional(),
  output: z.string().optional(),
  max_turns: z.number().int().positive().optional(),
  max_usd: z.number().positive().optional(),
});
export type TaskSpec = z.infer<typeof TaskSpec>;

/** The agent's files from /goal/agent. */
export interface AgentFiles {
  /** prompt.md: the agent's instructions. */
  prompt: string;
  /** output.schema.json: the station's output contract, when it has one. */
  outputSchema?: Record<string, unknown>;
}

export function parseSpec(raw: string): TaskSpec {
  return TaskSpec.parse(JSON.parse(raw));
}
