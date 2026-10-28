// The sentences of a run's timeline rows. Consecutive calls of one kind become one step ("Read 18 files
// in apps/web/src"), and each step says what happened in plain words, with paths and commands set as code.
// Pure, so the grouping is tested without rendering. A row's right label is a status or a duration, never
// a call's output: that output is kept for a disclosure under a failed step.
import { formatDuration } from '../../shared/format';
import type { CheckEntry, ToolCall } from './timeline';

/** A piece of a step's sentence: plain words, or a path, command or tool name set as code. */
export interface Segment {
  text: string;
  code?: boolean;
}

/** One timeline row of tool calls: its sentence, the tool its icon comes from, and what failed in it. */
export interface StepRow {
  key: string;
  at: string;
  tool: string;
  failed: boolean;
  segments: Segment[];
  /** A short status for the right column: "failed", or "3 of 6 failed". */
  right?: string | undefined;
  /** The raw output of the failed calls, shown behind a disclosure and never as the right label. */
  output: string[];
}

const editTools = new Set(['Edit', 'Write', 'MultiEdit']);
const searchTools = new Set(['Grep', 'Glob']);

/** The kind of step a tool's calls group into; shell commands and other tools are a step each. */
function kindOf(tool: string): 'read' | 'edit' | 'search' | undefined {
  if (tool === 'Read') return 'read';
  if (editTools.has(tool)) return 'edit';
  if (searchTools.has(tool)) return 'search';
  return undefined;
}

const code = (text: string): Segment => ({ text, code: true });

/** The directory every path shares, segment by segment ("apps/web/src" for files under it); "" when none. */
export function commonDirectory(paths: string[]): string {
  const [first, ...rest] = paths.map((p) => p.split('/').slice(0, -1));
  if (!first) return '';
  let common = first;
  for (const dir of rest) {
    let i = 0;
    while (i < common.length && i < dir.length && common[i] === dir[i]) i++;
    common = common.slice(0, i);
  }
  return common.join('/');
}

/** A shell command's first word: "git" for "git diff --stat". */
function firstWord(command: string): string {
  return command.trim().split(/\s+/)[0] ?? '';
}

/** The sentence of one step: the calls it groups, in the words a person reads. */
function sentenceOf(calls: ToolCall[]): Segment[] {
  const first = calls[0];
  if (!first) return [];
  const n = calls.length;
  const args = calls.map((c) => c.argument).filter(Boolean);

  if (first.tool === 'Bash') {
    const word = firstWord(first.argument);
    return word ? [{ text: 'Ran ' }, code(`${word}…`)] : [{ text: 'Ran a shell command' }];
  }

  switch (kindOf(first.tool)) {
    case 'read': {
      if (n === 1) return first.argument ? [{ text: 'Read ' }, code(first.argument)] : [{ text: 'Read a file' }];
      const dir = commonDirectory(args);
      return dir ? [{ text: `Read ${n} files in ` }, code(dir)] : [{ text: `Read ${n} files` }];
    }
    case 'edit': {
      const files = new Set(args).size || n;
      const only = args[0];
      if (files === 1 && only) return [{ text: 'Edited ' }, code(only)];
      return [{ text: `Edited ${files} ${files === 1 ? 'file' : 'files'}` }];
    }
    case 'search':
      if (n === 1) {
        return first.argument ? [{ text: 'Searched for ' }, code(first.argument)] : [{ text: 'Ran a search' }];
      }
      return [{ text: `Ran ${n} searches` }];
    default:
      return first.argument ? [{ text: `${first.tool} ` }, code(first.argument)] : [{ text: first.tool }];
  }
}

/** Consecutive calls of one kind, as groups; each other call is a group of its own. */
export function groupCalls(calls: ToolCall[]): ToolCall[][] {
  const groups: ToolCall[][] = [];
  for (const call of calls) {
    const kind = kindOf(call.tool);
    const last = groups.at(-1);
    const lastKind = last?.[0] ? kindOf(last[0].tool) : undefined;
    if (kind && last && lastKind === kind) last.push(call);
    else groups.push([call]);
  }
  return groups;
}

/** A run's tool calls as timeline rows: one row per group of consecutive calls of one kind. */
export function toolSteps(calls: ToolCall[]): StepRow[] {
  return groupCalls(calls).flatMap((group): StepRow[] => {
    const first = group[0];
    if (!first) return [];
    const failed = group.filter((c) => c.isError);
    const right =
      failed.length === 0 ? undefined : group.length === 1 ? 'failed' : `${failed.length} of ${group.length} failed`;
    return [
      {
        key: `tool-${first.at}-${first.id}`,
        at: first.at,
        tool: first.tool,
        failed: failed.length > 0,
        segments: sentenceOf(group),
        right,
        output: failed.flatMap((c) => (c.summary ? [c.summary] : [])),
      },
    ];
  });
}

/** A check as a step: its command in code, passed or failed with its exit code; the right label is how long it took. */
export function checkStep(check: CheckEntry): { segments: Segment[]; right?: string | undefined } {
  const exit = !check.passed && check.exitCode !== undefined ? `, exit ${check.exitCode}` : '';
  return {
    segments: [code(check.command), { text: `${check.passed ? ' passed' : ' failed'}${exit}` }],
    right: check.durationMs !== undefined ? formatDuration(check.durationMs) : undefined,
  };
}
