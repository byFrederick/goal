import type { RunDeps } from './run.js';
import { runTask } from './run.js';
import type { AgentFiles, TaskSpec } from './spec.js';

/**
 * Check mode (ADR 0040 decision 19): a trigger's check looks at a source with its agent's
 * environment and answers what to file. It runs as a task with no item, but it only reads: the
 * built-ins that edit or run are never allowed, no tool asks a person (ask is off), it reaches no
 * gateway tool (no question__ask, no knowledge) and has no workspace to change.
 */

/** The built-in tools a check never has, whatever its agent allows. */
export const checkDeniedTools = ['Write', 'Edit', 'MultiEdit', 'NotebookEdit', 'Bash', 'Task'];

/** The longest a check's input block may be, in characters. */
export const maxCheckInput = 60_000;

/** Whether an allow rule ("Bash(npm test*)", "Edit") is for a tool a check never has. */
function denied(rule: string): boolean {
  return checkDeniedTools.includes(rule.replace(/\(.*$/, ''));
}

/** A tool mode as a check takes it: ask is off. */
function noAsk<M extends string>(mode: M): M | 'off' {
  return mode === 'ask' ? 'off' : mode;
}

/**
 * The spec a check runs: the edit and run built-ins out of tools_allow and in tools_deny, every
 * ask tool off, no gateway (so no question__ask, knowledge tools or notes), no granted call, no
 * knowledge, and the brief with the check's input and the answer instructions (checkBrief).
 */
export function checkSpec(spec: TaskSpec): TaskSpec {
  if (!spec.check) throw new Error('check mode needs a spec with check');
  const out: TaskSpec = {
    ...spec,
    tools_allow: spec.tools_allow.filter((rule) => !denied(rule)),
    tools_deny: [...new Set([...spec.tools_deny, ...checkDeniedTools])],
    connections: spec.connections.map((c) => ({
      ...c,
      tool_modes: Object.fromEntries(Object.entries(c.tool_modes ?? {}).map(([tool, mode]) => [tool, noAsk(mode)])),
    })),
    granted_calls: [],
    brief: checkBrief(spec),
  };
  delete out.gateway_url;
  delete out.knowledge;
  delete out.output;
  if (spec.environment) {
    out.environment = {
      ...spec.environment,
      mcp_servers: spec.environment.mcp_servers.map((s) => ({
        ...s,
        tool_modes: s.tool_modes.map((m) => ({ ...m, mode: noAsk(m.mode) })),
      })),
    };
  }
  return out;
}

/**
 * The check's prompt: its brief, then what goald gives it between untrusted-data delimiters (the
 * items' titles and notes and the saved state come from outside), cut to maxCheckInput, then the
 * answer's shape.
 */
export function checkBrief(spec: TaskSpec): string {
  const input = JSON.stringify(spec.check?.input ?? {}, null, 2)
    .replaceAll('</untrusted-event-data>', '<\\/untrusted-event-data>')
    .slice(0, maxCheckInput);
  return [
    spec.brief,
    '## What goald gives this check',
    "The open items this trigger filed (at most 50, with their key, ref, title, state and last note), the keys whose items closed in the last 7 days with how and why, the last check's decisions, and the state the last check saved:",
    `<untrusted-event-data>\n${input}\n</untrusted-event-data>`,
    '## Your answer',
    'Look at the source with your tools; change nothing. Answer with one JSON object: "file", the items to file, each with a "key" (the source\'s own id), a "title" and a "brief", and optionally "type", "lane", "priority", "repositories", "start" (a time or a wait such as 36h) and "reopen" (why to file a key again whose item closed); "note", {"key", "text"} for an open item of the list; "skip", {"key", "reason"} for what you looked at and left; and "state", an object of at most 4 KiB the next check gets, such as {"since": "2026-11-03T14:05:00Z"}.',
  ].join('\n\n');
}

/** Runs a check: the task of checkSpec(spec), with the answer contract goald served as the output schema. */
export async function runCheck(spec: TaskSpec, files: AgentFiles, deps: RunDeps): Promise<number> {
  return runTask(checkSpec(spec), files, deps);
}
