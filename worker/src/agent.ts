import type { Options, Settings } from '@anthropic-ai/claude-agent-sdk';
import { type AgentFiles, paths, type TaskSpec } from './spec.js';

/** The only MCP server an agent sees: goald's gateway. Its tools are mcp__goal__<tool>. */
export const gatewayServer = 'goal';

/** Tools an agent gets when its definition allows none: it can read the workspace, nothing more. */
const readOnlyTools = ['Read', 'Grep', 'Glob'];

/** Environment variables the agent's process never sees. */
const hiddenEnv = [
  'ANTHROPIC_API_KEY', // model auth is the subscription token; an API key must never take over
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
  const gatewayToken = env.GOAL_GATEWAY_TOKEN;
  const options: Options = {
    cwd: paths.workspace,
    systemPrompt: { type: 'preset', preset: 'claude_code', append: systemAppend(spec, files) },
    ...toolPolicy(spec, spec.gateway_url !== undefined),
    // Pre-approved rules run; anything else is denied, since nobody is there to ask.
    permissionMode: 'dontAsk',
    // Only the definition's skills are listed to the model and accepted by the Skill tool. Claude
    // Code would otherwise also offer the skills bundled with it, skills and plugins synced from
    // the claude.ai account behind the token, and the checkout's .claude/skills.
    skills: spec.skills,
    settings: isolation,
    // user: the agent's skills, copied to paths.claudeConfig/skills; project: the checkout's
    // CLAUDE.md and .claude/ settings.
    settingSources: ['user', 'project'],
    mcpServers: spec.gateway_url
      ? {
          [gatewayServer]: {
            type: 'http',
            url: spec.gateway_url,
            headers: gatewayToken ? { Authorization: `Bearer ${gatewayToken}` } : {},
            // The gateway already serves only the agent's allowed tools; loading them up front
            // (instead of deferring them behind tool search) is what lets agents call them
            // reliably, and it waits for the connection before the first turn.
            alwaysLoad: true,
          },
        }
      : {},
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
 * Maps the agent's tool rules onto the SDK. Allow rules use Claude Code's permission syntax
 * ("Read", "Bash(npm test*)"): their tool names are the only built-in tools available, and the
 * rules themselves are pre-approved. Deny rules always win. Rules for gateway tools
 * ("github__get_*": a connection, two underscores, a tool pattern) are enforced by goald's
 * gateway, which serves only the allowed ones, so they are left out here.
 */
export function toolPolicy(
  spec: Pick<TaskSpec, 'tools_allow' | 'tools_deny' | 'skills'>,
  gateway: boolean,
): Pick<Options, 'tools' | 'allowedTools' | 'disallowedTools'> {
  const builtin = spec.tools_allow.filter((rule) => !isGatewayRule(rule));
  const allow = builtin.length > 0 ? builtin : readOnlyTools;
  const tools = new Set(allow.map((rule) => rule.replace(/\(.*$/, '')).filter((name) => !name.startsWith('mcp__')));
  if (spec.skills.length > 0) tools.add('Skill');

  return {
    tools: [...tools],
    allowedTools: gateway ? [...allow, `mcp__${gatewayServer}`] : [...allow],
    disallowedTools: spec.tools_deny.filter((rule) => !isGatewayRule(rule)),
  };
}

/** Gateway tool rules name "<connection>__<tool pattern>"; built-in tool names have no "__". */
function isGatewayRule(rule: string): boolean {
  const name = rule.replace(/\(.*$/, '');
  return name.includes('__') && !name.startsWith('mcp__');
}

/**
 * The agent's environment: the container's, without hiddenEnv, with Claude Code's config in the
 * throwaway home and its nonessential traffic (telemetry, error reports, the updater) off. The
 * container reaches the outside only through goald's egress proxy (HTTPS_PROXY), which lets an
 * agent reach the model API and the gateway and refuses everything else.
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

/** What goes after Claude Code's system prompt: the factory's purpose, when it has one, then the agent's prompt. */
export function systemAppend(spec: Pick<TaskSpec, 'purpose'>, files: Pick<AgentFiles, 'prompt'>): string {
  return spec.purpose ? `## This factory\n${spec.purpose}\n\n${files.prompt}` : files.prompt;
}

/**
 * The user prompt: the brief, then what goald adds for this attempt (GOAL_FEEDBACK: people's
 * notes, why a previous attempt was rejected), earlier stations' outputs and how to finish.
 */
export function taskPrompt(spec: TaskSpec, feedback?: string): string {
  const parts = feedback ? [spec.brief, feedback] : [spec.brief];
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
