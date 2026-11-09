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
  /** Each of the item's repositories after the first, at /repos/<owner>/<name> (ADR 0041 decision 5). */
  repos: '/repos',
  /** The run's knowledge: each pinned snapshot unpacked, read-only, into /knowledge/<source>/ (ADR 0038). */
  knowledge: '/knowledge',
  /** Claude Code's config dir for this run, in the sandbox's throwaway home; never the host's. */
  claudeConfig: '/home/node/.claude',
  /** The sandbox's throwaway home, where git's global config and credential store go. */
  home: '/home/node',
  /** The logins the credential helper answers with (0600), written by prepare. */
  repositoryLogins: '/home/node/.goal/repository-logins.json',
  /** The repositories the credential helper answered for, one URL per line. */
  repositoryRecord: '/home/node/.goal/repositories',
} as const;

/** An https URL or URL prefix without spaces or quotes: a clone's URL or a login's prefix. */
const httpsUrl = z.string().regex(/^https:\/\/[^\s'"\\]+$/);

/** Where prepare cloned a repository: /workspace, or /repos/ and a relative path. */
const clonePath = z
  .string()
  .refine(
    (p) => p === paths.workspace || (p.startsWith(`${paths.repos}/`) && relativePath.safeParse(p.slice(7)).success),
    'a clone path is /workspace or /repos/<owner>/<name>',
  );

/**
 * One of the item's repositories as prepare cloned it, in spec.json: where, the item's branch,
 * whether the run may push to it, and the commits prepare saw (the base's and the branch's, before
 * any rebase or merge), which goal-agent run reports at finish.
 */
export const ClonedRepository = z.object({
  url: httpsUrl,
  path: clonePath,
  branch: z.string().min(1),
  write: z.boolean(),
  base_commit: z.string().optional(),
  branch_head: z.string().optional(),
});
export type ClonedRepository = z.infer<typeof ClonedRepository>;

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

/** One entry of an environment server's tool modes: a tool name or glob (as Go's path.Match) and its mode. */
export const ToolMode = z.object({ tool: z.string().min(1), mode: z.enum(['allow', 'ask', 'off']) });
export type ToolMode = z.infer<typeof ToolMode>;

/**
 * One MCP server of the agent's environment file (ADR 0037): remote (url and headers) or stdio
 * (command and env), each secret in them as ${GOAL_SECRET_<NAME>} for Claude Code to expand, and
 * its tool modes, most specific first.
 */
export const EnvironmentServer = z.object({
  /** As environment.schema.json names servers: an MCP server name and the mcp__<name>__ tool prefix. */
  name: z.string().regex(/^[a-z][a-z0-9_-]{0,39}$/),
  url: z.url().optional(),
  headers: z.record(z.string(), z.string()).optional(),
  command: z.array(z.string()).min(1).optional(),
  env: z.record(envName, z.string()).optional(),
  tool_modes: z.array(ToolMode).default([]),
});
export type EnvironmentServer = z.infer<typeof EnvironmentServer>;

/**
 * The agent's environment file on a version on secrets (ADR 0037 decisions 5, 8 and 11), without a
 * value: goal-agent run's env holds the variables, each file's content in the variable its env
 * names (GOAL_FILE_<n>) and the servers' secrets (GOAL_SECRET_<NAME>).
 */
export const Environment = z.object({
  name: z.string().min(1),
  mcp_servers: z.array(EnvironmentServer).default([]),
  /** Shell commands run in /workspace, in order, before the agent starts. */
  setup: z.array(z.string().min(1)).default([]),
  /** Files written before setup with mode 0600: an absolute path or one that starts with ~/. */
  files: z.array(z.object({ path: z.string().regex(/^(\/|~\/).+$/), env: envName })).default([]),
  /** The names of the file's variables. */
  env: z.array(envName).default([]),
  /** The names of the file's tools, without their versions. */
  clis: z.array(z.string()).default([]),
  /**
   * A try of a past item's (ADR 0039 decision 9): env is for setup alone, and goal-agent run
   * removes its variables before the agent starts.
   */
  setup_only: z.boolean().optional(),
});
export type Environment = z.infer<typeof Environment>;

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

/**
 * The run's knowledge (ADR 0038 decisions 7 and 8): its prompt's Knowledge section, and each pinned
 * snapshot prepare downloads (a URL resolved against the gateway's, which the task token authorizes)
 * and unpacks into /knowledge/<source>/. A draft request's has the section and no snapshot.
 */
export const Knowledge = z.object({
  section: z.string(),
  snapshots: z.array(
    z.object({
      /** A source's name, a slug: its folder under /knowledge, so it never holds a separator. */
      source: z.string().regex(/^[a-z0-9][a-z0-9-]*$/),
      url: z.string().min(1),
    }),
  ),
});
export type Knowledge = z.infer<typeof Knowledge>;

/** A draft request (ADR 0021): what a person asked to change in the factory's draft, and the agent it is about. */
export const DraftRequest = z.object({
  id: z.string().min(1),
  text: z.string().min(1).max(2000),
  agent: z.string().min(1).optional(),
});
export type DraftRequest = z.infer<typeof DraftRequest>;

/**
 * Jonah mode (ADR 0034, ADR 0039 decision 4): the review's policy. Everything it reads is a file
 * of the bundle under /goal (definition/, evidence/, proposals/, files.md, events.md, observations.md).
 */
export const Jonah = z.object({
  /** The policy's proposeAfter: a daily review proposes an observation only when its count reaches it. */
  propose_after: z.number().int().min(1),
  /** Whether a person asked for this review, which may propose below the threshold. */
  manual: z.boolean(),
});
export type Jonah = z.infer<typeof Jonah>;

/**
 * Grade mode (ADR 0039 decision 10): the reason an item went back and the result to grade, a try's
 * output and diff or the item's original or final result. The run answers fixed, not fixed or unknown.
 */
export const Grade = z.object({
  /** Why the item went back to an earlier station. */
  reason: z.string().min(1),
  result: z.object({
    kind: z.enum(['try', 'original', 'final']),
    output: z.string(),
    diff: z.string(),
  }),
});
export type Grade = z.infer<typeof Grade>;

/**
 * Check mode (ADR 0040 decision 19): the trigger whose check this is, and what goald gives it
 * besides its brief: the open items its trigger filed, the keys whose items closed in the last
 * 7 days, the last check's decisions and the state it saved, as goald read them.
 */
export const Check = z.object({
  trigger: z.string().min(1),
  input: z.object({
    open: z.array(
      z.object({ key: z.string(), ref: z.string(), title: z.string(), state: z.string(), last_note: z.string() }),
    ),
    closed: z.array(z.object({ key: z.string(), ref: z.string(), how: z.string(), why: z.string(), at: z.string() })),
    last_decisions: z.unknown(),
    state: z.unknown(),
  }),
});
export type Check = z.infer<typeof Check>;

const taskFields = z.object({
  factory_id: z.string().min(1),
  /** Empty only in definition, Jonah, grade and check mode, which need no item. */
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
  /** The agent's environment file on a version on secrets (ADR 0037); absent on a version on connections. */
  environment: Environment.optional(),
  /** The run's Knowledge section and snapshots; absent when it has none, or from a goald before them. */
  knowledge: Knowledge.optional(),
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
  /** The item's branch prepare checked out in /workspace; absent without a repository. */
  branch: z.string().optional(),
  /**
   * The item's repositories prepare cloned (ADR 0041 decision 5), the first at /workspace; absent
   * for a run whose bundle had no repository, which reports no heads.
   */
  repositories: z.array(ClonedRepository).optional(),
  /** Definition mode (ADR 0021): the run changes the factory's draft files in /workspace for this request. */
  draft_request: DraftRequest.optional(),
  /** Jonah mode (ADR 0039 decision 4): the run reads its box with Read, Grep and Glob and returns observations and proposals. */
  jonah: Jonah.optional(),
  /** Grade mode (ADR 0039 decision 10): the run grades this result against the reason, in one model call without tools. */
  grade: Grade.optional(),
  /** Check mode (ADR 0040 decision 19): the run looks at a source with read-only tools and answers what to file. */
  check: Check.optional(),
});

type ModeFields = {
  item_id: string;
  draft_request?: DraftRequest | undefined;
  jonah?: Jonah | undefined;
  grade?: Grade | undefined;
  check?: Check | undefined;
};

/** An item's run names its item; only definition, Jonah, grade and check mode may leave it empty. */
const itemNamed = (s: ModeFields) =>
  s.draft_request !== undefined ||
  s.jonah !== undefined ||
  s.grade !== undefined ||
  s.check !== undefined ||
  s.item_id.length > 0;
const itemMessage = {
  message: 'item_id is required outside definition, Jonah, grade and check mode',
  path: ['item_id'],
};

/** A run is in one mode at most: a draft request's, a Jonah review's, a grade's or a check's. */
const oneMode = (s: ModeFields) =>
  [s.draft_request, s.jonah, s.grade, s.check].filter((m) => m !== undefined).length <= 1;
const modeMessage = {
  message: 'a spec has at most one mode: a draft_request, a jonah, a grade or a check (not both)',
  path: ['jonah'],
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

/**
 * A git login for https://<host>: the user name and the variable that holds the password. With
 * store (the repository token's login, ADR 0037 decision 7), prepare keeps it in git's credential
 * store, so git commands after prepare sign in without the variable.
 */
export const GitCredential = z.object({
  host: z.string().regex(/^[A-Za-z0-9.-]+(:\d{1,5})?$/),
  username: z.string().regex(/^[A-Za-z0-9._@+-]+$/),
  password_env: z.string().regex(/^[A-Za-z_][A-Za-z0-9_]*$/),
  store: z.boolean().optional(),
});
export type GitCredential = z.infer<typeof GitCredential>;

/**
 * The git login of one repository entry: the credential helper answers with it for every URL under
 * prefix (one repository, or an owner's every repository ending in /), with username and the
 * value of password_env, which prepare reads. write is whether the run may push under prefix.
 */
export const RepositoryLogin = z.object({
  prefix: httpsUrl,
  username: z.string().regex(/^[A-Za-z0-9._@+-]+$/),
  password_env: z.string().regex(/^[A-Za-z_][A-Za-z0-9_]*$/),
  write: z.boolean(),
});
export type RepositoryLogin = z.infer<typeof RepositoryLogin>;

/** A mise tool spec such as go@1.26.10 or npm:pnpm@12.15.0; never an option. */
const toolSpec = z.string().regex(/^[A-Za-z0-9][A-Za-z0-9@:/._+-]*$/);

/**
 * The run bundle, GET <gateway origin>/run with the run's task token (internal/gateway, ServeRun).
 * It holds no secrets: git credentials name the variables that carry their passwords.
 */
export const Bundle = z
  .object({
    spec: taskFields
      .omit({ gateway_url: true, base_conflicts: true, base_branch: true, branch: true, repositories: true })
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
        /**
         * A try of a past item (ADR 0039 decision 9): the commit its original run started from, and
         * its base's commit then. Prepare makes a full clone, fetches them by SHA, checks branch out
         * at commit, brings it up to date with base_commit and drops every git login.
         */
        commit: z
          .string()
          .regex(/^(?:[0-9a-f]{40}|[0-9a-f]{64})$/)
          .optional(),
        base_commit: z
          .string()
          .regex(/^(?:[0-9a-f]{40}|[0-9a-f]{64})$/)
          .optional(),
      })
      .optional(),
    git_credentials: z.array(GitCredential).default([]),
    /**
     * A station run's code (ADR 0041 decisions 3 and 5), null or absent for every other run: the
     * item's repositories to clone (the first at /workspace), a login per entry with a token for
     * the credential helper, and the URL prefixes the run may not push to.
     */
    repositories: z
      .array(z.object({ url: httpsUrl, path: clonePath, branch: z.string().min(1), write: z.boolean() }))
      .nullish(),
    repository_logins: z.array(RepositoryLogin).nullish(),
    read_only: z.array(httpsUrl).nullish(),
    /** The definition's runtime.tools, installed after the repository's own. */
    tools: z.array(toolSpec).default([]),
    /**
     * Definition mode (ADR 0021): the factory's draft files by path, base64, written into /workspace
     * instead of a repository. It comes with the spec's draft_request, and never with a repository.
     */
    definition: z.object({ files: z.record(relativePath, z.base64()) }).optional(),
    /**
     * Whether the run restores and saves its workspace repository's build cache (ADR 0045, src/cache.ts);
     * false when the definition turns it off or the run has no repository. Absent is off.
     */
    build_cache: z.object({ enabled: z.boolean() }).optional(),
  })
  .refine((b) => !(b.definition && b.repository), {
    message: 'a bundle has a definition or a repository, not both',
    path: ['definition'],
  })
  .refine((b) => (b.definition === undefined) === (b.spec.draft_request === undefined), {
    message: "a bundle's definition and its spec's draft_request come together",
    path: ['definition'],
  })
  .refine((b) => !(b.spec.jonah && (b.repository || b.definition)), {
    message: "a jonah's bundle has no repository and no definition",
    path: ['spec', 'jonah'],
  })
  .refine((b) => !(b.spec.grade && (b.repository || b.definition)), {
    message: "a grade's bundle has no repository and no definition",
    path: ['spec', 'grade'],
  })
  .refine((b) => !(b.spec.check && (b.repository || b.definition)), {
    message: "a check's bundle has no repository and no definition",
    path: ['spec', 'check'],
  });
export type Bundle = z.infer<typeof Bundle>;
