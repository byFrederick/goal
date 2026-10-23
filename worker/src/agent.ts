import type { Options } from '@anthropic-ai/claude-agent-sdk';
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
    systemPrompt: { type: 'preset', preset: 'claude_code', append: files.prompt },
    ...toolPolicy(spec, spec.gateway_url !== undefined),
    // Pre-approved rules run; anything else is denied, since nobody is there to ask.
    permissionMode: 'dontAsk',
    skills: spec.skills,
    // user: the agent's skills, copied to ~/.claude/skills; project: the checkout's CLAUDE.md
    // and .claude/ settings.
    settingSources: ['user', 'project'],
    mcpServers: spec.gateway_url
      ? {
          [gatewayServer]: {
            type: 'http',
            url: spec.gateway_url,
            headers: gatewayToken ? { Authorization: `Bearer ${gatewayToken}` } : {},
          },
        }
      : {},
    strictMcpConfig: true,
    persistSession: false,
    env: agentEnv(env),
    abortController,
    stderr: (data) => process.stderr.write(data),
  };
  if (spec.model) options.model = spec.model;
  if (spec.max_turns) options.maxTurns = spec.max_turns;
  if (spec.max_usd) options.maxBudgetUsd = spec.max_usd;
  if (files.outputSchema) options.outputFormat = { type: 'json_schema', schema: files.outputSchema };

  return { prompt: taskPrompt(spec), options };
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

export function agentEnv(env: NodeJS.ProcessEnv): Record<string, string | undefined> {
  const out: Record<string, string | undefined> = { ...env, CLAUDE_AGENT_SDK_CLIENT_APP: 'goal-worker/0.1.0' };
  for (const name of hiddenEnv) delete out[name];
  return out;
}

/** The user prompt: the brief, earlier stations' outputs and how to finish. */
export function taskPrompt(spec: TaskSpec): string {
  const parts = [spec.brief];
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
