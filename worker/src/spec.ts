import { z } from 'zod';

/** Where things live in a run's sandbox (worker/README.md, "Sandbox contract"). */
export const paths = {
  /** What goal-agent prepare writes from the run bundle. */
  goal: '/goal',
  spec: '/goal/spec.json',
  agent: '/goal/agent',
  workspace: '/workspace',
  /** Claude Code's config dir for this run, in the sandbox's throwaway home; never the host's. */
  claudeConfig: '/home/node/.claude',
} as const;

/** The task as /goal/spec.json holds it: the bundle's spec plus the gateway's URL. It holds no secrets. */
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

/** A path inside /goal: relative, without "." or ".." segments. */
const bundlePath = z
  .string()
  .regex(/^[A-Za-z0-9_@+-][A-Za-z0-9_.@+-]*(\/[A-Za-z0-9_@+-][A-Za-z0-9_.@+-]*)*$/)
  .refine((p) => !p.split('/').some((seg) => seg === '.' || seg === '..'), 'no . or .. segments')
  .refine((p) => p !== 'spec.json', 'spec.json is the spec');

/** A git login for https://<host>: the user name and the variable that holds the password. */
export const GitCredential = z.object({
  host: z.string().regex(/^[A-Za-z0-9.-]+(:\d{1,5})?$/),
  username: z.string().regex(/^[A-Za-z0-9._@+-]+$/),
  password_env: z.string().regex(/^[A-Za-z_][A-Za-z0-9_]*$/),
});
export type GitCredential = z.infer<typeof GitCredential>;

/** A mise tool spec such as go@1.26.10 or npm:pnpm@12.15.0; never an option. */
const toolSpec = z.string().regex(/^[A-Za-z0-9][A-Za-z0-9@:/._+-]*$/);

/**
 * The run bundle, GET <gateway origin>/run with the run's task token (internal/gateway, ServeRun).
 * It holds no secrets: git credentials name the variables that carry their passwords.
 */
export const Bundle = z.object({
  spec: TaskSpec.omit({ gateway_url: true }),
  /** Files under /goal by path, base64. */
  files: z.record(bundlePath, z.base64()),
  /** Cloned into /workspace with the item's branch checked out; absent leaves it empty. */
  repository: z
    .object({
      /** A clone URL with a scheme (goald's definitions allow https only), never an option. */
      url: z.string().regex(/^[A-Za-z][A-Za-z0-9+.-]*:\/\/\S+$/),
      branch: z.string().min(1),
    })
    .optional(),
  git_credentials: z.array(GitCredential).default([]),
  /** The definition's runtime.tools, installed after the repository's own. */
  tools: z.array(toolSpec).default([]),
});
export type Bundle = z.infer<typeof Bundle>;
