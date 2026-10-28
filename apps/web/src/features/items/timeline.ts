// Turns an item's raw event history into the trace the Item screen shows: one block per agent
// run (its tool calls paired with their results, cost and turns), checks, and decisions. goald's
// record of each run (GET …/items/{item}/runs), when at hand, is the authority on its status,
// turns, cost and the definition version it ran on.
import type { ItemEvent, Run } from '../../api/types';
import { humanize } from '../../shared/format';
import type { Tone } from '../../shared/item-state';

export interface ToolCall {
  id: string;
  tool: string;
  /** The call's main argument (a path, a command, a pattern), shortened. */
  argument: string;
  summary?: string;
  isError: boolean;
  at: string;
}

export interface RunEntry {
  kind: 'run';
  key: string;
  /** The run's id. */
  id: string;
  at: string;
  endedAt?: string;
  station: string;
  agent: string;
  /** The station slot of the claim the run worked in, from 0: a machine field, never shown to people. */
  slot?: number;
  model?: string;
  /** The number of the definition version the run ran on. */
  version?: number;
  status: 'running' | 'succeeded' | 'failed' | 'cancelled';
  costUsd?: number;
  turns?: number;
  error?: string;
  tools: ToolCall[];
  /** The agent's last reply: prose, or the output it returned (its final reply is JSON). */
  reply?: Reply;
  /** The factory rules the run ran with, from its rules_applied event; absent when none applied. */
  rules?: AppliedRule[];
}

export type Reply = { kind: 'text'; text: string } | { kind: 'output'; value: unknown };

/** A factory rule a run ran with (ADR 0014): its name, and the hash of its file in the run's version. */
export interface AppliedRule {
  name: string;
  sha256: string;
}

/**
 * What a reply says: an agent finishes by replying with its output, JSON (whole, or in a fenced
 * block, as goald reads it), so that is the output, not words to quote; a JSON string output is
 * text, and anything else is prose.
 */
export function replyOf(text: string): Reply {
  const fenced = /```(?:json)?\s*\n([\s\S]*?)\n?```/.exec(text)?.[1];
  for (const raw of [text.trim(), fenced?.trim()]) {
    if (!raw || !/^[[{"]/.test(raw)) continue;
    try {
      const value: unknown = JSON.parse(raw);
      return typeof value === 'string' ? { kind: 'text', text: value } : { kind: 'output', value };
    } catch {
      // not JSON: prose
    }
  }
  return { kind: 'text', text };
}

export interface CheckEntry {
  kind: 'check';
  key: string;
  at: string;
  station: string;
  command: string;
  passed: boolean;
  exitCode?: number;
  durationMs?: number;
}

export interface NoteEntry {
  kind: 'note';
  key: string;
  at: string;
  station: string;
  actor: string;
  text: string;
  detail?: string;
  tone: Tone;
  /** An agent's question or a person's answer: the row gets the question mark. */
  question?: boolean;
  /** The question is still open: its row says Now. */
  open?: boolean;
}

export type Entry = RunEntry | CheckEntry | NoteEntry;

export interface Trace {
  entries: Entry[];
  costUsd: number;
  turns: number;
  model?: string;
}

const str = (v: unknown): string | undefined => (typeof v === 'string' && v !== '' ? v : undefined);
const num = (v: unknown): number | undefined => (typeof v === 'number' && Number.isFinite(v) ? v : undefined);

const argumentKeys = ['command', 'file_path', 'pattern', 'url', 'query', 'path', 'arg'];

/** The most telling argument of a tool call's (possibly truncated) JSON input. */
export function toolArgument(input: unknown): string {
  const raw = typeof input === 'string' ? input : JSON.stringify(input ?? '');
  let value: unknown = raw;
  try {
    value = JSON.parse(raw);
  } catch {
    // Cut to 500 characters by the worker; show it as it is.
  }
  if (value && typeof value === 'object' && !Array.isArray(value)) {
    const obj = value as Record<string, unknown>;
    const key =
      argumentKeys.find((k) => typeof obj[k] === 'string') ?? Object.keys(obj).find((k) => typeof obj[k] === 'string');
    if (key) value = obj[key];
  }
  const text = typeof value === 'string' ? value : JSON.stringify(value);
  return text.length > 140 ? `${text.slice(0, 139)}…` : text;
}

/** The rules of a rules_applied event's data: entries with a name and a sha256, others left out. */
function appliedRules(value: unknown): AppliedRule[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((v) => {
    const rule = typeof v === 'object' && v !== null ? (v as Record<string, unknown>) : {};
    const name = str(rule.name);
    const sha256 = str(rule.sha256);
    return name && sha256 ? [{ name, sha256 }] : [];
  });
}

function note(e: ItemEvent): Omit<NoteEntry, 'kind' | 'key' | 'at' | 'station' | 'actor'> | undefined {
  const reason = str(e.data.reason);
  const withReason = (text: string, tone: Tone) => ({ text, tone, ...(reason ? { detail: reason } : {}) });
  const actor = e.actor;

  switch (e.kind) {
    case 'created':
      return { text: `Created in ${humanize(e.station ?? 'the first station')}`, tone: 'muted' };
    case 'moved':
      return { text: `Moved to ${humanize(str(e.data.to) ?? e.station ?? '')}`, tone: 'muted' };
    case 'claimed':
    case 'run_started':
    case 'agent_started':
    case 'usage':
      return undefined;
    case 'gate_approved':
      return { text: `${actor} passed it`, tone: 'ok' };
    case 'gate_rejected':
      return withReason(`${actor} sent it back`, 'rework');
    case 'approved':
      return { text: `${actor} approved`, tone: 'ok' };
    case 'rejected':
      return withReason(`${actor} sent it back`, 'rework');
    case 'note':
      return {
        text: `${actor} left a note for the next run`,
        tone: 'you',
        ...(str(e.data.text) ? { detail: str(e.data.text) as string } : {}),
      };
    case 'question_asked':
      return { text: 'Asked you a question', tone: 'you', question: true };
    case 'question_answered':
      return {
        text: `${actor} answered`,
        tone: 'you',
        question: true,
        ...(str(e.data.answer) ? { detail: str(e.data.answer) as string } : {}),
      };
    case 'merged':
      return { text: `${actor} merged the pull request`, tone: 'ok' };
    case 'retried':
      return { text: `${actor} retried it`, tone: 'run' };
    case 'waiting_human':
      return withReason('Waiting on you', 'you');
    case 'blocked':
      return withReason('Blocked', 'rework');
    case 'contract_failed':
      return {
        text: 'Output did not match the station contract',
        tone: 'rework',
        ...(str(e.data.error) ? { detail: str(e.data.error) as string } : {}),
      };
    case 'lease_expired':
      return { text: 'Lease expired, back in the queue', tone: 'muted' };
    case 'released':
      return withReason('Released back to the queue', 'muted');
    case 'done':
      return { text: 'Done', tone: 'ok' };
    case 'rule_violated':
      return withReason(
        `${humanize(str(e.data.agent) ?? actor)} found the rule ${str(e.data.rule) ?? ''} broken`,
        'rework',
      );
    default:
      return { text: humanize(e.kind), tone: 'muted' };
  }
}

/** A run's role as people read it: "Investigator" for the investigator agent. Its slot is a machine field, never shown. */
export function runRole(run: Pick<RunEntry, 'agent'>): string {
  return humanize(run.agent);
}

/** goald's record of a run over what its events said; turns and cost count once it ended. */
function applyRecord(run: RunEntry, record: Run) {
  run.status = record.status;
  run.model = record.model;
  if (record.status !== 'running') {
    run.turns = record.turns;
    run.costUsd = record.cost_usd;
  }
  if (record.ended_at) run.endedAt = record.ended_at;
  if (record.error) run.error = record.error;
  if (record.definition_version !== null) run.version = record.definition_version;
}

/**
 * The item's history as people read it. `questionOpen` is whether the item still holds an open
 * question (Item.question): only then is its latest question's row marked Now.
 */
export function buildTrace(
  events: readonly ItemEvent[],
  records: readonly Run[] = [],
  questionOpen = false,
): Trace {
  const entries: Entry[] = [];
  const runs = new Map<string, RunEntry>();
  // The slot of the latest claim: every run until the next claim works in it.
  let slot: number | undefined;
  const lastAsked = events.findLast((e) => e.kind === 'question_asked');

  for (const e of events) {
    const station = e.station ?? '';

    if (e.kind === 'claimed') slot = num(e.data.slot);

    if (e.run_id) {
      let run = runs.get(e.run_id);
      if (!run) {
        run = {
          kind: 'run',
          key: `run-${e.run_id}`,
          id: e.run_id,
          at: e.at,
          station,
          agent: e.actor,
          ...(slot !== undefined ? { slot } : {}),
          status: 'running',
          tools: [],
        };
        runs.set(e.run_id, run);
        entries.push(run);
      }

      switch (e.kind) {
        case 'agent_started': {
          const m = str(e.data.model);
          if (m) run.model = m;
          break;
        }
        case 'tool_call':
          run.tools.push({
            id: str(e.data.id) ?? String(e.id),
            tool: str(e.data.tool) ?? 'tool',
            argument: toolArgument(e.data.input),
            isError: false,
            at: e.at,
          });
          break;
        case 'tool_result': {
          const id = str(e.data.id);
          const call = [...run.tools].reverse().find((t) => t.id === id && t.summary === undefined);
          if (call) {
            const summary = str(e.data.summary);
            if (summary) call.summary = summary;
            call.isError = e.data.is_error === true;
          }
          break;
        }
        case 'rules_applied': {
          const rules = appliedRules(e.data.rules);
          if (rules.length > 0) run.rules = rules;
          break;
        }
        case 'agent_message': {
          const text = str(e.data.text);
          if (text) run.reply = replyOf(text);
          break;
        }
        case 'run_finished': {
          const status = str(e.data.status);
          run.status = status === 'failed' || status === 'cancelled' ? status : 'succeeded';
          run.endedAt = e.at;
          const cost = num(e.data.cost_usd);
          const t = num(e.data.turns);
          const error = str(e.data.error);
          if (cost !== undefined) run.costUsd = cost;
          if (t !== undefined) run.turns = t;
          if (error) run.error = error;
          break;
        }
      }
      continue;
    }

    if (e.kind === 'check_passed' || e.kind === 'check_failed') {
      const exitCode = num(e.data.exit_code);
      const durationMs = num(e.data.duration_ms);
      entries.push({
        kind: 'check',
        key: `ev-${e.id}`,
        at: e.at,
        station,
        command: str(e.data.command) ?? 'check',
        passed: e.kind === 'check_passed',
        ...(exitCode !== undefined ? { exitCode } : {}),
        ...(durationMs !== undefined ? { durationMs } : {}),
      });
      continue;
    }

    const n = note(e);
    // Only the latest question can be open, and only while the item still holds it (the caller's word).
    const open = questionOpen && e === lastAsked;
    if (n)
      entries.push({ kind: 'note', key: `ev-${e.id}`, at: e.at, station, actor: e.actor, ...n, ...(open ? { open } : {}) });
  }

  for (const record of records) {
    const run = runs.get(record.id);
    if (run) applyRecord(run, record);
  }

  let costUsd = 0;
  let turns = 0;
  let model: string | undefined;
  for (const run of runs.values()) {
    costUsd += run.costUsd ?? 0;
    turns += run.turns ?? 0;
    model = run.model ?? model;
  }

  return { entries, costUsd, turns, ...(model ? { model } : {}) };
}

/**
 * The acceptance criteria an output names: a string list under `acceptance` (this factory's spec
 * contract) or `acceptance_criteria`. Anything else is no criteria.
 */
export function criteriaOf(value: unknown): string[] {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return [];
  const obj = value as Record<string, unknown>;
  const list = obj.acceptance ?? obj.acceptance_criteria;
  return Array.isArray(list) && list.every((c) => typeof c === 'string') ? list : [];
}

/** The criteria a run's final reply lists, if the run replied with an output. */
export function runCriteria(run: RunEntry): string[] {
  return run.reply?.kind === 'output' ? criteriaOf(run.reply.value) : [];
}

/** The tools that change a file, by the name the agents call them. */
const editTools = new Set(['Edit', 'Write', 'MultiEdit']);

/** The distinct files the trace's edits touched: the Changes tab's count. */
export function editedFiles(trace: Trace): string[] {
  const files = new Set<string>();
  for (const e of trace.entries) {
    if (e.kind !== 'run') continue;
    for (const t of e.tools) if (editTools.has(t.tool) && t.argument) files.add(t.argument);
  }
  return [...files];
}

/** The last `n` tool calls across runs, newest last: the mobile "latest steps". */
export function latestToolCalls(trace: Trace, n: number): ToolCall[] {
  return trace.entries.flatMap((e) => (e.kind === 'run' ? e.tools : [])).slice(-n);
}

/** The run still working, if any. */
export function activeRun(trace: Trace): RunEntry | undefined {
  return trace.entries.findLast((e): e is RunEntry => e.kind === 'run' && e.status === 'running');
}
