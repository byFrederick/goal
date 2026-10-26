import { realpath } from 'node:fs/promises';
import { posix } from 'node:path';
import type { CanUseTool, McpServerConfig, Options, Settings } from '@anthropic-ai/claude-agent-sdk';
import { type AgentFiles, type Connection, paths, type TaskSpec } from './spec.js';

/** goald's gateway as an MCP server: Goal's own tools, mcp__goal__<tool>. */
export const gatewayServer = 'goal';

/** Tools an agent gets when its definition allows none: it can read the workspace, nothing more. */
const readOnlyTools = ['Read', 'Grep', 'Glob'];

/** Environment variables the agent's process never sees. */
const hiddenEnv = [
  'GOAL_GATEWAY_TOKEN', // reaches the gateway as a header, not the agent's shell
];

/** Settings that keep everything Claude Code brings or syncs on its own out of the agent's session. */
const isolation = {
  disableBundledSkills: true,
  syncClaudeAiSkills: false,
  syncClaudeAiPlugins: false,
  disableClaudeAiConnectors: true,
} as const satisfies Settings;

export interface AgentQuery {
  prompt: string;
  options: Options;
}

/** Builds the SDK query for one task. */
export function agentQuery(
  spec: TaskSpec,
  files: AgentFiles,
  env: NodeJS.ProcessEnv,
  abortController: AbortController,
): AgentQuery {
  const options: Options = {
    cwd: paths.workspace,
    systemPrompt: { type: 'preset', preset: 'claude_code', append: systemAppend(spec, files) },
    ...toolPolicy(spec, spec.gateway_url !== undefined),
    // Pre-approved rules run; every other call reaches canUseTool, which allows a connection's MCP
    // tools as the agent's rules say and denies the rest, since nobody is there to ask. (dontAsk
    // would deny those calls before canUseTool sees them.)
    permissionMode: 'default',
    canUseTool: connectionPermission(spec),
    // Only the definition's skills are listed to the model and accepted by the Skill tool. Claude
    // Code would otherwise also offer the skills bundled with it, skills and plugins synced from
    // the claude.ai account behind the token, and the checkout's .claude/skills.
    skills: spec.skills,
    settings: isolation,
    // user: the agent's skills, copied to paths.claudeConfig/skills; project: the checkout's
    // CLAUDE.md and .claude/ settings.
    settingSources: ['user', 'project'],
    mcpServers: mcpServers(spec, env.GOAL_GATEWAY_TOKEN),
    strictMcpConfig: true,
    // The stream's message_delta events carry each API call's final token counts.
    includePartialMessages: true,
    persistSession: false,
    env: agentEnv(env),
    abortController,
    stderr: (data) => process.stderr.write(data),
  };
  if (spec.model) options.model = spec.model;
  if (spec.max_turns) options.maxTurns = spec.max_turns;
  if (spec.max_usd) options.maxBudgetUsd = spec.max_usd;
  if (files.outputSchema) options.outputFormat = { type: 'json_schema', schema: files.outputSchema };

  return { prompt: taskPrompt(spec, env.GOAL_FEEDBACK), options };
}

/**
 * The MCP servers of the run: goald's gateway (Goal's tools) and each connection's own, which
 * Claude Code reaches from the box like any API. A remote server's headers name the box's env
 * variables (${NAME}), which Claude Code expands, so no value is in the config; a stdio server is
 * started in the box and inherits the agent's env. A connection with both a URL and a command is
 * reached at its URL. Every server's tools are loaded up front: deferred behind tool search,
 * agents called them by name and failed.
 */
function mcpServers(spec: TaskSpec, gatewayToken: string | undefined): Record<string, McpServerConfig> {
  const servers: Record<string, McpServerConfig> = {};
  if (spec.gateway_url) {
    servers[gatewayServer] = {
      type: 'http',
      url: spec.gateway_url,
      headers: gatewayToken ? { Authorization: `Bearer ${gatewayToken}` } : {},
      alwaysLoad: true,
    };
  }
  for (const c of spec.connections) {
    if (c.name === gatewayServer) throw new Error(`connection ${c.name}: the name is goald's gateway's`);
    const [command, ...args] = c.mcp?.command ?? [];
    if (c.mcp?.url) {
      servers[c.name] = { type: 'http', url: c.mcp.url, headers: c.mcp.headers ?? {}, alwaysLoad: true };
    } else if (command) {
      servers[c.name] = { type: 'stdio', command, args, alwaysLoad: true };
    }
  }
  return servers;
}

/**
 * Maps the agent's tool rules onto the SDK. Allow rules use Claude Code's permission syntax
 * ("Read", "Bash(npm test*)"): their tool names are the only built-in tools available, and the
 * rules themselves are pre-approved. Deny rules always win. Rules for connection tools
 * ("github__get_*": a connection, two underscores, a tool pattern) are left out: the gateway
 * enforces them for its tools and connectionPermission for the connections' own servers.
 */
export function toolPolicy(
  spec: Pick<TaskSpec, 'tools_allow' | 'tools_deny' | 'skills'>,
  gateway: boolean,
): Pick<Options, 'tools' | 'allowedTools' | 'disallowedTools'> {
  const builtin = spec.tools_allow.filter((rule) => !isConnectionRule(rule));
  const allow = builtin.length > 0 ? builtin : readOnlyTools;
  const tools = new Set(allow.map((rule) => rule.replace(/\(.*$/, '')).filter((name) => !name.startsWith('mcp__')));
  if (spec.skills.length > 0) tools.add('Skill');

  return {
    tools: [...tools],
    allowedTools: gateway ? [...allow, `mcp__${gatewayServer}`] : [...allow],
    disallowedTools: spec.tools_deny.filter((rule) => !isConnectionRule(rule)),
  };
}

/** Connection tool rules name "<connection>__<tool pattern>"; built-in tool names have no "__". */
function isConnectionRule(rule: string): boolean {
  const name = rule.replace(/\(.*$/, '');
  return name.includes('__') && !name.startsWith('mcp__');
}

/**
 * The permission check of calls nothing pre-approved. A tool of a connection's MCP server
 * (mcp__<connection>__<tool>) is allowed by the agent's rules for it, as the gateway applies them
 * to its tools: deny wins, a malformed deny pattern denies the connection's every tool, and a
 * connection with allow rules allows only the tools they match. A built-in write Claude Code
 * escalates is decided by guidanceWrite. Every other call is denied, as Claude Code's dontAsk
 * mode would.
 */
export function connectionPermission(
  spec: Pick<TaskSpec, 'tools_allow' | 'tools_deny' | 'connections'>,
  workspace: string = paths.workspace,
): CanUseTool {
  const servers = new Set(spec.connections.filter((c) => c.mcp).map((c) => c.name));
  const rules = (list: string[], conn: string) =>
    list
      .filter(isConnectionRule)
      .flatMap((rule) => (rule.startsWith(`${conn}__`) ? [rule.slice(conn.length + 2)] : []));

  return async (toolName, input) => {
    if (writeTools.has(toolName) || toolName === 'Bash') {
      if (await guidanceWrite(spec, workspace, toolName, input)) return { behavior: 'allow', updatedInput: input };
      return { behavior: 'deny', message: `${toolName} may write only the workspace's guidance files: ${guidance}.` };
    }
    const [, conn = '', tool = ''] = /^mcp__([a-z0-9-]+)__(.+)$/.exec(toolName) ?? [];
    if (!servers.has(conn))
      return { behavior: 'deny', message: `${toolName} is not allowed by the agent's tool rules.` };

    const allow = rules(spec.tools_allow, conn);
    const denied = rules(spec.tools_deny, conn).some((p) => matches(p, tool) ?? true);
    if (denied || (allow.length > 0 && !allow.some((p) => matches(p, tool) ?? false))) {
      return { behavior: 'deny', message: `The agent's tool rules do not let it call ${conn}__${tool}.` };
    }
    return { behavior: 'allow', updatedInput: input };
  };
}

/** Built-in tools that write the file named by their file_path. */
const writeTools = new Set(['Write', 'Edit', 'MultiEdit']);

/** The workspace's guidance files: the only paths an escalated built-in write may reach. */
const guidanceDirs = ['.claude/rules/', '.claude/skills/', '.claude/agents/', '.claude/commands/'];
const guidanceFiles = ['CLAUDE.md', 'AGENTS.md'];
const guidance = [...guidanceDirs, ...guidanceFiles].join(', ');

/**
 * Whether an escalated built-in write may run. Claude Code treats .claude/ as protected and asks
 * about every write there even when an allow rule covers it; nobody is there to answer, so this
 * check does. The call must be granted by the agent's tools_allow and matched by no tools_deny
 * rule (as Claude Code matches them), and it must write only guidance files of the workspace:
 * .claude/{rules,skills,agents,commands}/ and the root CLAUDE.md and AGENTS.md. Everything else
 * stays denied, above all .claude/settings*.json, .claude/hooks and .git/, which would change the
 * agent's own permissions, hooks or history for later runs, and anything outside the workspace.
 */
async function guidanceWrite(
  spec: Pick<TaskSpec, 'tools_allow' | 'tools_deny'>,
  workspace: string,
  tool: string,
  input: Record<string, unknown>,
): Promise<boolean> {
  const target = tool === 'Bash' ? input.command : input.file_path;
  if (typeof target !== 'string' || target === '') return false;
  const builtin = (list: string[]) => list.filter((rule) => !isConnectionRule(rule));
  const ruleMatches = (rule: string) => builtinRuleMatches(rule, tool, target, workspace);
  if (builtin(spec.tools_deny).some(ruleMatches) || !builtin(spec.tools_allow).some(ruleMatches)) return false;

  // Bash: a command's targets cannot be parsed reliably (variables, cd, eval, redirections,
  // subshells), so this is a mention-based decision, not a path check. The command must mention a
  // guidance path and nothing protected: settings*.json, .git, any other .claude/ path, a parent
  // (..), the home directory or Claude Code's config dir. Indirection can still hide a target;
  // tools_deny and the box's isolation remain the backstop.
  if (tool === 'Bash') return guidanceMention.test(target) && !protectedMention.test(target);

  const lexical = posix.resolve(workspace, target);
  if (!isGuidance(posix.relative(workspace, lexical))) return false;
  // A committed symlink (.claude/rules -> ../.git) would pass the lexical check: the real path of
  // the target's nearest existing ancestor must be guidance of the workspace's real path too.
  const real = await realTarget(lexical);
  return isGuidance(posix.relative(await realpath(workspace), real));
}

const guidanceMention = /(^|[\s'"=/])(\.claude\/(rules|skills|agents|commands)\/|CLAUDE\.md|AGENTS\.md)/;
const protectedMention =
  /settings[\w.-]*\.json|\.git(?![\w-])|\.claude(?!\/(rules|skills|agents|commands)(\/|(?![\w.-])))|\.\.|~|\$\{?HOME|CLAUDE_CONFIG_DIR|\/home\//;

/** Whether rel, a path relative to the workspace, is one of its guidance files. */
function isGuidance(rel: string): boolean {
  if (rel.startsWith('..') || posix.isAbsolute(rel)) return false;
  return guidanceFiles.includes(rel) || guidanceDirs.some((dir) => rel.startsWith(dir) && rel.length > dir.length);
}

/** The absolute path with its nearest existing ancestor's symlinks resolved. */
async function realTarget(path: string): Promise<string> {
  const rest: string[] = [];
  for (let dir = path; ; dir = posix.dirname(dir)) {
    try {
      return posix.join(await realpath(dir), ...rest.reverse());
    } catch {
      if (dir === '/') return path;
      rest.push(posix.basename(dir));
    }
  }
}

/**
 * Whether a built-in tool rule in Claude Code's syntax covers the call: a bare tool name covers
 * every call of it; Bash(spec) covers commands matching spec ("*" any run, "prefix:*" a prefix);
 * Write(spec), Edit(spec) and MultiEdit(spec) cover file paths matching spec as a gitignore-style
 * path ("//abs" absolute, "~/" the home directory, which holds no guidance, otherwise relative to
 * the workspace; "**" any run of directories, "*" any run within one).
 */
function builtinRuleMatches(rule: string, tool: string, target: string, workspace: string): boolean {
  const [, name = rule, specifier] = /^([^(]+)\((.*)\)$/s.exec(rule) ?? [];
  if (name !== tool) return false;
  if (specifier === undefined || specifier === '' || specifier === '*') return true;
  if (tool === 'Bash') {
    if (specifier.endsWith(':*')) return target.startsWith(specifier.slice(0, -2));
    return wildcard(specifier, '.*').test(target);
  }
  let pattern = specifier;
  if (pattern.startsWith('//')) pattern = pattern.slice(1);
  else if (pattern.startsWith('~/'))
    return false; // the home directory: never the workspace
  else pattern = posix.join(workspace, pattern);
  return wildcard(pattern, '[^/]*').test(posix.resolve(workspace, target));
}

/** A regular expression of a pattern whose only metacharacters are "**" (any run) and "*" (star). */
function wildcard(pattern: string, star: string): RegExp {
  const re = pattern
    .split('**')
    .map((part) =>
      part
        .split('*')
        .map((s) => s.replace(/[.*+?^${}()|[\]\\/-]/g, '\\$&'))
        .join(star),
    )
    .join('.*');
  return new RegExp(`^${re}$`, 's');
}

/**
 * Whether name matches pattern as Go's path.Match does on a tool name (no "/"): "*" any run of
 * characters, "?" one, "[…]" a class ("[^…]" or "[!…]" negated), "\\" escaping the next one.
 * A malformed pattern matches nothing and returns undefined.
 */
export function matches(pattern: string, name: string): boolean | undefined {
  let re = '';
  for (let i = 0; i < pattern.length; i++) {
    const ch = pattern.charAt(i);
    if (ch === '*') re += '.*';
    else if (ch === '?') re += '.';
    else if (ch === '\\') {
      if (i + 1 >= pattern.length) return undefined;
      re += literal(pattern.charAt(++i));
    } else if (ch === '[') {
      const end = pattern.indexOf(']', i + 2);
      if (end < 0) return undefined;
      let body = pattern.slice(i + 1, end);
      const negated = body.startsWith('^') || body.startsWith('!');
      if (negated) body = body.slice(1);
      if (body === '') return undefined;
      re += `[${negated ? '^' : ''}${body.replace(/[\\\]^]/g, '\\$&')}]`;
      i = end;
    } else re += literal(ch);
  }
  return new RegExp(`^${re}$`, 's').test(name);
}

function literal(ch: string): string {
  return ch.replace(/[.*+?^${}()|[\]\\/-]/g, '\\$&');
}

/**
 * The agent's environment: the container's, without hiddenEnv, with Claude Code's config in the
 * throwaway home and its nonessential traffic (telemetry, error reports, the updater) off. goald
 * gives the agent's exec exactly the model's access and its connections' env, so whatever model
 * credential is here is the model connection's.
 */
export function agentEnv(env: NodeJS.ProcessEnv): Record<string, string | undefined> {
  const out: Record<string, string | undefined> = {
    ...env,
    CLAUDE_AGENT_SDK_CLIENT_APP: 'goal-worker/0.1.0',
    CLAUDE_CONFIG_DIR: paths.claudeConfig,
    CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: '1',
  };
  for (const name of hiddenEnv) delete out[name];
  return out;
}

/**
 * What goes after Claude Code's system prompt: the factory's purpose, when it has one, the agent's
 * prompt, then the agent's connections and the knowledge sources the gateway's knowledge tools
 * search, when it serves them.
 */
export function systemAppend(
  spec: Pick<TaskSpec, 'purpose' | 'connections' | 'gateway_url' | 'knowledge_sources'>,
  files: Pick<AgentFiles, 'prompt'>,
): string {
  const parts = spec.purpose ? [`## This factory\n${spec.purpose}`, files.prompt] : [files.prompt];
  if (spec.connections.length > 0) parts.push(connectionsSection(spec.connections));
  const sources = spec.knowledge_sources ?? [];
  if (spec.gateway_url && sources.length > 0) {
    const tool = (name: string) => `mcp__${gatewayServer}__knowledge__${name}`;
    parts.push(
      `## Knowledge\nThe organization's knowledge sources ${sources.join(', ')} are searchable with ${tool('search')}; ` +
        `${tool('read')} returns a whole document by the id a search gave.`,
    );
  }
  return parts.join('\n\n');
}

/** The "Connections" section: each connection's name, kind, env names and MCP server, and its kind's docs; never a value. */
function connectionsSection(connections: Connection[]): string {
  const parts = [
    '## Connections',
    'Your environment holds these connections. Their variables carry credentials: use them, never print or share their values.',
  ];
  for (const c of connections) {
    const facts = [];
    if (c.env.length > 0) facts.push(`Environment: ${c.env.map((n) => `\`${n}\``).join(', ')}.`);
    if (c.mcp) facts.push(`MCP server \`${c.name}\`: its tools are \`mcp__${c.name}__*\`.`);
    parts.push([`### ${c.name} (${c.title})`, facts.join(' '), c.docs].filter(Boolean).join('\n\n'));
  }
  return parts.join('\n\n');
}

/**
 * The user prompt: a note to resolve conflicts with the default branch when prepare found some,
 * the brief, then what goald adds for this attempt (GOAL_FEEDBACK: people's
 * notes, why a previous attempt was rejected), earlier stations' outputs and how to finish.
 */
export function taskPrompt(spec: TaskSpec, feedback?: string): string {
  const parts = feedback ? [spec.brief, feedback] : [spec.brief];
  const conflicts = spec.base_conflicts ?? [];
  if (conflicts.length > 0) {
    const list = conflicts.map((f) => `- ${f}`).join('\n');
    parts.unshift(
      `## First: conflicts with the default branch\n\nYour branch could not be brought up to date with the repository's default branch (origin/HEAD): these files conflict. It is checked out as it was pushed. Before the task below, rebase or merge the default branch, resolve the conflicts in these files, and push:\n\n${list}`,
    );
  }
  const inputs = Object.entries(spec.inputs ?? {});
  if (inputs.length > 0) {
    parts.push('## Outputs of earlier stations');
    for (const [station, output] of inputs) {
      parts.push(`### ${station}\n\n\`\`\`json\n${JSON.stringify(output, null, 2)}\n\`\`\``);
    }
  }
  const contract = spec.output ? ` that satisfies the station's output contract "${spec.output}"` : '';
  parts.push(
    `## Finishing\n\nWhen the work is done, reply with only your final output: one JSON object${contract}. It is validated before the item moves on.`,
  );
  return parts.join('\n\n');
}
