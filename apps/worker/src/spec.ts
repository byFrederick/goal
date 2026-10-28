import { z } from 'zod';

/** Where things live in a run's sandbox (apps/worker/README.md, "Sandbox contract"). */
export const paths = {
  /** What goal-agent prepare writes from the run bundle. */
  goal: '/goal',
  spec: '/goal/spec.json',
  /** Definition mode: each draft file's starting sha256, by its path in /workspace. */
  definition: '/goal/definition.json',
  /** Definition mode: the factory schema, when the bundle ships it. */
  factorySchema: '/goal/definition/factory.schema.json',
  agent: '/goal/agent',
  workspace: '/workspace',
  /** Claude Code's config dir for this run, in the sandbox's throwaway home; never the host's. */
  claudeConfig: '/home/node/.claude',
} as const;

/** A variable name; never an option or a shell word. */
const envName = z.string().regex(/^[A-Za-z_][A-Za-z0-9_]*$/);

/**
 * One of the agent's organization connections, as the run bundle describes it without a value: the
 * variables it sets in the agent's env, its MCP server (remote, whose headers name variables as
 * ${NAME} for Claude Code to expand, or a stdio command started in the box) and its kind's docs.
 */
export const Connection = z.object({
  /** A slug (lowercase letters, digits, inner dashes): an MCP server name and a tool rule prefix. */
  name: z.string().regex(/^[a-z][a-z0-9-]{1,38}[a-z0-9]$/),
  kind: z.string(),
  title: z.string(),
  env: z.array(envName),
  mcp: z
    .object({
      url: z.url().optional(),
      headers: z.record(z.string(), z.string()).optional(),
      command: z.array(z.string()).min(1).optional(),
    })
    .optional(),
  docs: z.string().optional(),
  /** The tools that ask first or are off for this run's agent (ADR 0031); a tool not listed is allowed. Absent from a goald before tool modes. */
  tool_modes: z.record(z.string(), z.enum(['ask', 'off'])).optional(),
  /** The asking tools, sorted, a person may answer with Always allow. */
  always_allow_offered: z.array(z.string()).optional(),
});
export type Connection = z.infer<typeof Connection>;

/**
 * One of the factory rules that apply to the run (ADR 0014): its name, the sha256 of its rule
 * file in the run's definition version, its paths and the markdown file to write (frontmatter
 * with description and paths, then the body).
 */
export const Rule = z.object({
  /** A slug, as the factory schema names rules: prepare writes it as <name>.md, so it never holds a separator. */
  name: z.string().regex(/^[a-z][a-z0-9-]{1,38}[a-z0-9]$/),
  sha256: z.string().regex(/^[0-9a-f]{64}$/),
  paths: z.array(z.string()).optional(),
  content: z.string(),
});
export type Rule = z.infer<typeof Rule>;

/** A draft request (ADR 0021): what a person asked to change in the factory's draft, and the agent it is about. */
export const DraftRequest = z.object({
  id: z.string().min(1),
  text: z.string().min(1).max(2000),
  agent: z.string().min(1).optional(),
});
export type DraftRequest = z.infer<typeof DraftRequest>;

/** A judge's outcome (ADR 0025): its score from 0 to 1, its label and what it means. */
export const JudgeOutcome = z.object({
  score: z.number().min(0).max(1),
  label: z.string().min(1).max(40),
  meaning: z.string().min(1).max(300),
});
export type JudgeOutcome = z.infer<typeof JudgeOutcome>;

/**
 * What a judge reads: a finished run (its events, its spec and its pull request diff) or an eval
 * try (the case's brief, the agent's output and the git diff), each as text.
 */
export const JudgeMaterial = z.discriminatedUnion('kind', [
  z.object({ kind: z.literal('run'), events: z.string(), spec: z.string(), diff: z.string() }),
  z.object({ kind: z.literal('eval_try'), brief: z.string(), output: z.string(), diff: z.string() }),
]);
export type JudgeMaterial = z.infer<typeof JudgeMaterial>;

/**
 * Judge mode (ADR 0025): the judge evals/judges/<slug>.md, its rubric (the file's Markdown body),
 * its outcomes and pass mark, and the material it scores.
 */
export const Judge = z.object({
  slug: z.string().regex(/^[a-z][a-z0-9-]{1,38}[a-z0-9]$/),
  rubric: z.string().min(1),
  outcomes: z
    .array(JudgeOutcome)
    .min(1)
    .refine((os) => new Set(os.map((o) => o.label)).size === os.length, 'outcome labels are unique')
    .refine((os) => new Set(os.map((o) => o.score)).size === os.length, 'outcome scores are unique'),
  pass_mark: z.number().min(0).max(1),
  material: JudgeMaterial,
});
export type Judge = z.infer<typeof Judge>;

/** A factory definition file as a Jonah review reads it: its path in the definition, its sha256 and its text. */
export const JonahFile = z.object({
  path: z.string().min(1),
  sha256: z.string().regex(/^[0-9a-f]{64}$/),
  content: z.string(),
});
export type JonahFile = z.infer<typeof JonahFile>;

/** A sent-back event in the review's window: an item's touch that a station or a gate sent back, with its reason. */
export const JonahEvent = z.object({
  /** Names the event in the review's evidence; a proposal's evidence points at this id. */
  id: z.string().min(1),
  item_ref: z.string(),
  title: z.string(),
  station: z.string(),
  agent: z.string(),
  kind: z.string(),
  reason: z.string(),
});
export type JonahEvent = z.infer<typeof JonahEvent>;

/** A proposal the factory rejected in the last 90 days, with the reason a person gave: a later review does not repeat it. */
export const JonahRejected = z.object({
  title: z.string(),
  summary: z.string(),
  paths: z.array(z.string()),
  reason: z.string(),
});
export type JonahRejected = z.infer<typeof JonahRejected>;

/** Jonah mode (ADR 0034): the factory's definition, its sent-back events and its rejected proposals, read in one model call. */
export const Jonah = z.object({
  files: z.array(JonahFile),
  events: z.array(JonahEvent),
  rejected: z.array(JonahRejected),
  /** The policy's proposeAfter: a threshold review proposes an observation only when its count reaches it. */
  propose_after: z.number().int().min(1),
  /** Whether a person asked for this review, which may propose below the threshold. */
  manual: z.boolean(),
});
export type Jonah = z.infer<typeof Jonah>;

const taskFields = z.object({
  factory_id: z.string().min(1),
  /** Empty only in definition mode, judge mode and Jonah mode, which need no item. */
  item_id: z.string(),
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
  /** The agent's organization connections its token grants; absent from a goald before connections. */
  connections: z.array(Connection).default([]),
  /** The knowledge sources the gateway's knowledge tools search for this run. */
  knowledge_sources: z.array(z.string()).optional(),
  /** The factory rules that apply to the run; absent from a goald before factory rules. */
  rules: z.array(Rule).optional(),
  /**
   * The Ask-first calls a person approved for this run (ADR 0031 decision 5), each allowed once
   * when its connection, tool and input as canonical JSON match; absent from a goald before them.
   */
  granted_calls: z.array(z.object({ connection: z.string(), tool: z.string(), input: z.unknown() })).optional(),
  /**
   * Files that conflicted when prepare brought the item's branch up to date with the default
   * branch; it aborted, so the branch is as pushed and the agent resolves them first.
   */
  base_conflicts: z.array(z.string()).optional(),
  /** The branch those files conflict with when the bundle named a base; absent means the default branch. */
  base_branch: z.string().optional(),
  /** Definition mode (ADR 0021): the run changes the factory's draft files in /workspace for this request. */
  draft_request: DraftRequest.optional(),
  /** Judge mode (ADR 0025): the run scores this material with this judge, in one model call without tools. */
  judge: Judge.optional(),
  /** Jonah mode (ADR 0034): the run reviews the factory and returns observations and proposals, in one model call without tools. */
  jonah: Jonah.optional(),
});

type ModeFields = {
  item_id: string;
  draft_request?: DraftRequest | undefined;
  judge?: Judge | undefined;
  jonah?: Jonah | undefined;
};

/** An item's run names its item; only definition, judge and Jonah mode may leave it empty. */
const itemNamed = (s: ModeFields) =>
  s.draft_request !== undefined || s.judge !== undefined || s.jonah !== undefined || s.item_id.length > 0;
const itemMessage = {
  message: 'item_id is required outside definition mode, judge mode and Jonah mode',
  path: ['item_id'],
};

/** A run is in one mode at most: a draft request's, a judge's or a Jonah review's. */
const oneMode = (s: ModeFields) => [s.draft_request, s.judge, s.jonah].filter((m) => m !== undefined).length <= 1;
const modeMessage = {
  message: 'a spec has at most one mode: a draft_request, a judge or a jonah (not both)',
  path: ['judge'],
};

/** The task as /goal/spec.json holds it: the bundle's spec plus the gateway's URL. It holds no secrets. */
export const TaskSpec = taskFields.refine(itemNamed, itemMessage).refine(oneMode, modeMessage);
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

/** A relative path without "." or ".." segments. */
export const relativePath = z
  .string()
  .regex(/^[A-Za-z0-9_@+-][A-Za-z0-9_.@+-]*(\/[A-Za-z0-9_@+-][A-Za-z0-9_.@+-]*)*$/)
  .refine((p) => !p.split('/').some((seg) => seg === '.' || seg === '..'), 'no . or .. segments');

/** A path inside /goal; never one of the files prepare writes itself. */
const bundlePath = relativePath
  .refine((p) => p !== 'spec.json', 'spec.json is the spec')
  .refine((p) => p !== 'definition.json', 'definition.json is the definition baseline');

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
export const Bundle = z
  .object({
    spec: taskFields
      .omit({ gateway_url: true, base_conflicts: true, base_branch: true })
      .refine(itemNamed, itemMessage)
      .refine(oneMode, modeMessage),
    /** Files under /goal by path, base64. */
    files: z.record(bundlePath, z.base64()),
    /** Cloned into /workspace with the item's branch checked out; absent leaves it empty. */
    repository: z
      .object({
        /** A clone URL with a scheme (goald's definitions allow https only), never an option. */
        url: z.string().regex(/^[A-Za-z][A-Za-z0-9+.-]*:\/\/\S+$/),
        branch: z.string().min(1),
        /** How an existing branch is brought up to date with the default branch; rebase when absent. */
        fresh_base: z.enum(['rebase', 'merge']).optional(),
        /**
         * The branch the item's branch starts from and is brought up to date with; empty or absent
         * means the default branch. Prepare creates it from the default branch when the remote has none.
         */
        base: z.string().optional(),
      })
      .optional(),
    git_credentials: z.array(GitCredential).default([]),
    /** The definition's runtime.tools, installed after the repository's own. */
    tools: z.array(toolSpec).default([]),
    /**
     * Definition mode (ADR 0021): the factory's draft files by path, base64, written into /workspace
     * instead of a repository. It comes with the spec's draft_request, and never with a repository.
     */
    definition: z.object({ files: z.record(relativePath, z.base64()) }).optional(),
  })
  .refine((b) => !(b.definition && b.repository), {
    message: 'a bundle has a definition or a repository, not both',
    path: ['definition'],
  })
  .refine((b) => (b.definition === undefined) === (b.spec.draft_request === undefined), {
    message: "a bundle's definition and its spec's draft_request come together",
    path: ['definition'],
  })
  .refine((b) => !(b.spec.judge && (b.repository || b.definition)), {
    message: "a judge's bundle has no repository and no definition",
    path: ['spec', 'judge'],
  })
  .refine((b) => !(b.spec.jonah && (b.repository || b.definition)), {
    message: "a jonah's bundle has no repository and no definition",
    path: ['spec', 'jonah'],
  });
export type Bundle = z.infer<typeof Bundle>;
