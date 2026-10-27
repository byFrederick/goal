import { createHash } from 'node:crypto';
import { readdir, readFile } from 'node:fs/promises';
import { join, posix } from 'node:path';
import type { CanUseTool, Options, query as sdkQuery } from '@anthropic-ai/claude-agent-sdk';
import { z } from 'zod';
import { agentEnv, isolation, realTarget } from './agent.js';
import { EventMapper, type Finished, type WorkerEvent } from './events.js';
import { PromptQueue } from './notes.js';
import { failed } from './run.js';
import { type DraftRequest, paths, type TaskSpec } from './spec.js';

/**
 * Definition mode (ADR 0021): a draft request's run. /workspace holds the factory's draft files and
 * no repository; the agent changes the files the request needs, and the result lists what changed.
 */

/** The only tools of definition mode: reading and editing files, nothing that runs a command. */
export const definitionTools = ['Read', 'Write', 'Edit', 'MultiEdit', 'Glob', 'Grep'];

/** The folders whose files a draft request may change, besides factory.yaml (ADR 0002 decision 7). */
const definitionDirs = ['agents', 'rules', 'skills'];

/**
 * Whether a path relative to the workspace is one a draft request may change: factory.yaml or a
 * file under agents/, rules/ or skills/. Never evals/, contracts/, another top-level file, or a path
 * that is absolute or has an empty, "." or ".." segment.
 */
export function isDefinitionPath(path: string): boolean {
  const segments = path.split('/');
  if (segments.some((s) => s === '' || s === '.' || s === '..')) return false;
  if (path === 'factory.yaml') return true;
  return segments.length >= 2 && definitionDirs.includes(segments[0] ?? '');
}

/** The system prompt appended in definition mode; prompt.md is not read. */
export const definitionSystemPrompt = [
  "## Definition mode\nYou change a Goal factory's definition for one request a person made. The working directory holds the files of the factory's draft: factory.yaml, agents/, rules/, skills/ and maybe evals/ and contracts/. It is not a git repository, and nothing you do here is published: a person reviews the draft first.",
  '## Rules\n' +
    [
      '- Change only the files the request needs, and keep every other file as it is.',
      `- Keep factory.yaml valid against the factory schema (${paths.factorySchema}, when it is there). Goal runs the publish checks on the draft afterwards.`,
      '- Never create, change or delete anything under evals/ or contracts/. A change there, or to any path other than factory.yaml and files under agents/, rules/ and skills/, fails the request and is dropped.',
      '- Leave your work in the working directory; write nowhere else.',
    ].join('\n'),
  '## Finishing\nReply with one paragraph that says what you changed and why, or why you changed nothing.',
].join('\n\n');

/** The output contract of definition mode. */
const summarySchema = {
  type: 'object',
  properties: { summary: { type: 'string' } },
  required: ['summary'],
  additionalProperties: false,
};

/** The user prompt: the request's text and, when it is about one agent, its name. */
export function definitionPrompt(request: DraftRequest): string {
  const parts = [request.text];
  if (request.agent) parts.push(`This request is about the agent \`${request.agent}\` of the definition.`);
  return parts.join('\n\n');
}

export interface DefinitionDeps {
  query: typeof sdkQuery;
  emit: (event: WorkerEvent) => void;
  now: () => Date;
  env: NodeJS.ProcessEnv;
  abortController: AbortController;
  /** The draft's files (/workspace) and what prepare wrote (/goal). */
  dirs?: { workspace: string; goal: string };
}

/** Builds the SDK query of a draft request: the fixed prompt, the file tools, no MCP server and no skill. */
export function definitionQuery(
  spec: TaskSpec,
  request: DraftRequest,
  env: NodeJS.ProcessEnv,
  abortController: AbortController,
  dirs: { workspace: string; goal: string },
): { prompt: string; options: Options } {
  const options: Options = {
    cwd: dirs.workspace,
    systemPrompt: { type: 'preset', preset: 'claude_code', append: definitionSystemPrompt },
    tools: definitionTools,
    // Nothing is pre-approved: every call nothing else allows reaches the path check.
    permissionMode: 'default',
    canUseTool: workspacePermission(dirs),
    skills: [],
    settings: isolation,
    settingSources: ['user', 'project'],
    mcpServers: {},
    strictMcpConfig: true,
    includePartialMessages: true,
    persistSession: false,
    env: agentEnv(env),
    abortController,
    outputFormat: { type: 'json_schema', schema: summarySchema },
    stderr: (data) => process.stderr.write(data),
  };
  if (spec.model) options.model = spec.model;
  if (spec.max_turns) options.maxTurns = spec.max_turns;
  if (spec.max_usd) options.maxBudgetUsd = spec.max_usd;
  return { prompt: definitionPrompt(request), options };
}

/**
 * The permission check of definition mode: the file tools may write only inside the workspace and
 * read only it and /goal/definition (the factory schema), both checked as written and with symlinks
 * resolved. Everything else is denied, so the baseline, the box's home and /proc stay out of reach.
 */
function workspacePermission(dirs: { workspace: string; goal: string }): CanUseTool {
  const writable = [dirs.workspace];
  const readable = [dirs.workspace, join(dirs.goal, 'definition')];
  return async (toolName, input) => {
    const deny = { behavior: 'deny' as const, message: `${toolName} may reach only the draft's files.` };
    if (!definitionTools.includes(toolName)) return deny;
    const search = toolName === 'Glob' || toolName === 'Grep';
    const target = search ? (input.path ?? dirs.workspace) : input.file_path;
    if (typeof target !== 'string' || target === '') return deny;
    const roots = toolName === 'Read' || search ? readable : writable;
    if (await within(roots, posix.resolve(dirs.workspace, target))) return { behavior: 'allow', updatedInput: input };
    return deny;
  };
}

async function within(roots: string[], path: string): Promise<boolean> {
  const inside = (root: string, p: string) => {
    const rel = posix.relative(root, p);
    return rel === '' || (!rel.startsWith('..') && !posix.isAbsolute(rel));
  };
  const real = await realTarget(path);
  for (const root of roots) {
    if (inside(root, path) && inside(await realTarget(root), real)) return true;
  }
  return false;
}

/** A file a draft request wrote: its path, the sha256 of its new content and the content, base64. */
export interface ChangedFile {
  path: string;
  sha256: string;
  content: string;
}

/** What a draft request changed, sorted by path; rejected changes are in neither changed nor deleted. */
export interface DefinitionChanges {
  changed: ChangedFile[];
  deleted: string[];
  rejected: string[];
}

/** definition.json: each draft file's starting sha256 by its path. */
const Baseline = z.record(z.string(), z.string().regex(/^[0-9a-f]{64}$/));

/**
 * Compares the workspace with the baseline prepare wrote. A new or modified regular file is changed
 * and a missing one deleted; one outside the allowlist, and any symlink or other non-regular file
 * (never followed), is rejected instead.
 */
export async function definitionChanges(
  workspace: string,
  baseline: Record<string, string>,
): Promise<DefinitionChanges> {
  const files: string[] = [];
  const others: string[] = [];
  const walk = async (dir: string, rel: string): Promise<void> => {
    for (const entry of await readdir(dir, { withFileTypes: true })) {
      const path = rel === '' ? entry.name : `${rel}/${entry.name}`;
      if (entry.isDirectory()) await walk(join(dir, entry.name), path);
      else if (entry.isFile()) files.push(path);
      else others.push(path);
    }
  };
  await walk(workspace, '');

  const out: DefinitionChanges = { changed: [], deleted: [], rejected: [...others] };
  for (const path of files) {
    const content = await readFile(join(workspace, path));
    const sha256 = createHash('sha256').update(content).digest('hex');
    if (baseline[path] === sha256) continue;
    if (isDefinitionPath(path)) out.changed.push({ path, sha256, content: content.toString('base64') });
    else out.rejected.push(path);
  }
  const present = new Set([...files, ...others]);
  for (const path of Object.keys(baseline)) {
    if (present.has(path)) continue;
    if (isDefinitionPath(path)) out.deleted.push(path);
    else out.rejected.push(path);
  }
  out.changed.sort((a, b) => (a.path < b.path ? -1 : 1));
  out.deleted.sort();
  out.rejected.sort();
  return out;
}

/**
 * Runs a draft request and emits its events, ending with one finished event whose output is
 * { summary, changed, deleted, rejected }. A rejected change fails the run. It returns 0 when the
 * agent produced a result and 1 when it crashed, as runTask does.
 */
export async function runDefinition(spec: TaskSpec, deps: DefinitionDeps): Promise<number> {
  const dirs = deps.dirs ?? { workspace: paths.workspace, goal: paths.goal };
  const request = spec.draft_request;
  if (!request) {
    deps.emit(failed(deps.now, 'definition mode needs a draft request'));
    return 1;
  }
  const mapper = new EventMapper(deps.now, []);
  const inbox = new PromptQueue();
  let result: Finished | undefined;
  try {
    const baseline = Baseline.parse(JSON.parse(await readFile(join(dirs.goal, 'definition.json'), 'utf8')));
    const { prompt, options } = definitionQuery(spec, request, deps.env, deps.abortController, dirs);
    inbox.push({ type: 'user', message: { role: 'user', content: prompt }, parent_tool_use_id: null });
    for await (const msg of deps.query({ prompt: inbox, options })) {
      for (const event of mapper.map(msg)) {
        if (event.type === 'finished') {
          result = event;
          inbox.close();
        } else deps.emit(event);
      }
    }
    if (result) result = withChanges(result, await definitionChanges(dirs.workspace, baseline));
  } catch (err) {
    deps.emit(failed(deps.now, err instanceof Error ? err.message : String(err)));
    return 1;
  } finally {
    inbox.close();
  }
  deps.emit(result ?? failed(deps.now, 'the agent ended without a result'));
  return result ? 0 : 1;
}

/** The finished event with definition mode's output; a rejected change fails it, naming the paths. */
function withChanges(result: Finished, changes: DefinitionChanges): Finished {
  const reply = result.output as { summary?: unknown } | undefined;
  const summary = typeof reply?.summary === 'string' ? reply.summary : '';
  const out: Finished = { ...result, output: { summary, ...changes } };
  if (changes.rejected.length > 0) {
    const why = `changes outside factory.yaml, agents/, rules/ and skills/ were dropped: ${changes.rejected.join(', ')}`;
    out.status = 'failed';
    out.error = result.error ? `${result.error}; ${why}` : why;
  }
  return out;
}
