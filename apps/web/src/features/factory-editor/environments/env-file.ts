// An environment file, environments/<name>.yaml (ADR 0037 decision 5), read and edited as text. Edits
// go through the `yaml` package's Document, so the file's comments and key order stay as they were.
// Every function takes the file's text and returns the new text; the section writes it through the
// draft session (session.editFile), which saves it on the file's sha256.
import { type Document, isMap, parseDocument, YAMLMap, YAMLSeq } from 'yaml';
import type { PatchOp, TemplateAdds } from '../../../api/types';

export const modes = ['allow', 'ask', 'off'] as const;
export type ToolMode = (typeof modes)[number];

export const sizes = ['small', 'medium', 'large'] as const;
export type BoxSize = (typeof sizes)[number];

export const environmentsDir = 'environments/';
export const defaultEnvironment = 'default';
export const envPath = (name: string) => `${environmentsDir}${name}.yaml`;

/** The environment a path names (environments/deploy.yaml → deploy), or undefined for any other path. */
export function envName(path: string): string | undefined {
  const m = /^environments\/([^/]+)\.yaml$/.exec(path);
  return m?.[1];
}

export interface McpServer {
  url?: string;
  headers: Record<string, string>;
  command?: string[];
  env: Record<string, string>;
  tools: Record<string, ToolMode>;
}

/** The file's spec, read leniently: what is missing or malformed is left out. */
export interface EnvSpec {
  size?: BoxSize;
  image?: string;
  tools: string[];
  setup: string[];
  env: Record<string, string>;
  files: Record<string, string>;
  mcp: Record<string, McpServer>;
}

const obj = (v: unknown): Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v) ? (v as Record<string, unknown>) : {};
const strings = (v: unknown): string[] => (Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []);
const strMap = (v: unknown): Record<string, string> =>
  Object.fromEntries(Object.entries(obj(v)).filter((e): e is [string, string] => typeof e[1] === 'string'));

export function readEnv(text: string): EnvSpec {
  const doc = parseDocument(text);
  const spec = obj(obj(doc.toJS({ maxAliasCount: 0 }) ?? {}).spec);
  const mcp: Record<string, McpServer> = {};
  for (const [name, raw] of Object.entries(obj(spec.mcp))) {
    const s = obj(raw);
    const tools = Object.fromEntries(
      Object.entries(strMap(s.tools)).filter((e): e is [string, ToolMode] =>
        (modes as readonly string[]).includes(e[1]),
      ),
    );
    mcp[name] = {
      ...(typeof s.url === 'string' ? { url: s.url } : {}),
      headers: strMap(s.headers),
      ...(Array.isArray(s.command) ? { command: strings(s.command) } : {}),
      env: strMap(s.env),
      tools,
    };
  }
  return {
    ...((sizes as readonly unknown[]).includes(spec.size) ? { size: spec.size as BoxSize } : {}),
    ...(typeof spec.image === 'string' ? { image: spec.image } : {}),
    tools: strings(spec.tools),
    setup: strings(spec.setup),
    env: strMap(spec.env),
    files: strMap(spec.files),
    mcp,
  };
}

/** A new file: the header and an empty spec. */
export function newEnvText(name: string): string {
  return `apiVersion: goal/v1\nkind: Environment\nmetadata:\n  name: ${name}\nspec: {}\n`;
}

/** The name of the secret a value is, as a whole `{{name}}` reference. */
export function secretRef(value: string): string | undefined {
  return /^\{\{\s*([^{}\s]+)\s*\}\}$/.exec(value)?.[1];
}

/** The secret names a value mentions, `{{a}}` anywhere in it. */
export function secretsIn(value: string): string[] {
  return [...value.matchAll(/\{\{\s*([^{}\s]+)\s*\}\}/g)].map((m) => m[1] ?? '');
}

type Path = (string | number)[];

/** Deletes the node at `path` and then every parent left empty, up to `spec`. */
function prune(doc: Document, path: Path): void {
  doc.deleteIn(path);
  for (let n = path.length - 1; n >= 2; n--) {
    const parent = doc.getIn(path.slice(0, n), true);
    const empty = (isMap(parent) || parent instanceof YAMLSeq) && parent.items.length === 0;
    if (!empty) break;
    doc.deleteIn(path.slice(0, n));
  }
}

function edit(text: string, change: (doc: Document) => void): string {
  const doc = parseDocument(text);
  if (!isMap(doc.contents))
    doc.contents = doc.createNode({ apiVersion: 'goal/v1', kind: 'Environment', spec: {} }) as never;
  if (!isMap(doc.get('spec', true))) doc.set('spec', new YAMLMap());
  change(doc);
  const out = doc.toString();
  // A change that leaves the spec as it was leaves the text alone (no reformatting of what nobody edited).
  return JSON.stringify(readEnv(out)) === JSON.stringify(readEnv(text)) ? text : out;
}

/** Sets a value under spec, or removes the key when `value` is undefined (and parents left empty). */
function put(text: string, path: Path, value: unknown): string {
  return edit(text, (doc) => {
    const full: Path = ['spec', ...path];
    if (value === undefined) prune(doc, full);
    else doc.setIn(full, value);
  });
}

export const setSize = (text: string, size: BoxSize | undefined) => put(text, ['size'], size);
export const setImage = (text: string, image: string | undefined) =>
  put(text, ['image'], image === undefined || image.trim() === '' ? undefined : image.trim());
export const setList = (text: string, key: 'tools' | 'setup', list: readonly string[]) =>
  put(text, [key], list.length === 0 ? undefined : [...list]);

/** Sets a variable or a file; `rename` changes the key and keeps its place. */
export function setEntry(text: string, key: 'env' | 'files', name: string, value: string, rename?: string): string {
  return edit(text, (doc) => {
    const map = doc.getIn(['spec', key], true);
    if (rename === undefined || rename === name || !isMap(map)) {
      doc.setIn(['spec', key, name], value);
      return;
    }
    const pair = map.items.find((p) => String((p.key as { value?: unknown })?.value ?? p.key) === rename);
    if (pair) {
      (pair.key as { value: unknown }).value = name;
      pair.value = doc.createNode(value);
    } else doc.setIn(['spec', key, name], value);
  });
}

export const removeEntry = (text: string, key: 'env' | 'files', name: string) => put(text, [key, name], undefined);

export const setToolMode = (text: string, server: string, tool: string, mode: ToolMode | undefined) =>
  put(text, ['mcp', server, 'tools', tool], mode);

export const removeServer = (text: string, server: string) => put(text, ['mcp', server], undefined);

/** A mode for every tool of a group at once (all the tools of Read). */
export function setToolModes(text: string, server: string, tools: readonly string[], mode: ToolMode): string {
  return tools.reduce((t, tool) => setToolMode(t, server, tool, mode), text);
}

/** The mode a tool gets: the most specific match of the server's `tools` (an exact name, else the longest glob), allow when none. */
export function modeOf(server: McpServer, tool: string): ToolMode {
  const exact = server.tools[tool];
  if (exact) return exact;
  let best: { len: number; mode: ToolMode } | undefined;
  for (const [pattern, mode] of Object.entries(server.tools)) {
    if (!pattern.includes('*') && !pattern.includes('?')) continue;
    const re = new RegExp(
      `^${pattern
        .replace(/[.+^${}()|[\]\\]/g, '\\$&')
        .replaceAll('*', '.*')
        .replaceAll('?', '.')}$`,
    );
    if (re.test(tool) && (!best || pattern.length > best.len)) best = { len: pattern.length, mode };
  }
  return best?.mode ?? 'allow';
}

const unescapePointer = (token: string) => token.replaceAll('~1', '/').replaceAll('~0', '~');

/**
 * Applies a problem's quick fix: its patch paths are `/<environment>/<keys below spec>` (a patch on the
 * Environments section), so the first token is dropped and the rest are keys under the file's `spec`.
 */
export function applyPatch(text: string, patch: readonly PatchOp[]): string {
  return edit(text, (doc) => {
    for (const op of patch) {
      const tokens = op.path.split('/').slice(1).map(unescapePointer);
      const path: Path = ['spec', ...tokens.slice(1)];
      if (op.op === 'remove') prune(doc, path);
      else doc.setIn(path, op.value);
    }
  });
}

/** The fix's target: the keys below spec, as `env GH_TOKEN`. */
export const patchTarget = (op: PatchOp): string[] => op.path.split('/').slice(2).map(unescapePointer);

/** Replaces `{{slot}}` with the secret picked for it, in a template's value. */
export function fillSlots(value: string, secrets: Readonly<Record<string, string>>): string {
  return value.replace(/\{\{\s*([^{}\s]+)\s*\}\}/g, (whole, slot: string) => {
    const name = secrets[slot];
    return name ? `{{${name}}}` : whole;
  });
}

/** What Add to draft writes: the template's lines with the picked secrets, the endpoint and the tool modes. */
export function fillAdds(
  adds: TemplateAdds,
  secrets: Readonly<Record<string, string>>,
  overrides: { url?: string; modes?: Readonly<Record<string, Record<string, ToolMode>>> } = {},
): TemplateAdds {
  const map = (m: Record<string, string>) =>
    Object.fromEntries(Object.entries(m).map(([k, v]) => [k, fillSlots(v, secrets)]));
  return {
    tools: [...adds.tools],
    env: map(adds.env),
    files: map(adds.files),
    mcp: Object.fromEntries(
      Object.entries(adds.mcp).map(([name, s]) => [
        name,
        {
          ...(s.url !== undefined ? { url: overrides.url ?? s.url } : {}),
          ...(s.headers ? { headers: map(s.headers) } : {}),
          ...(s.command ? { command: [...s.command] } : {}),
          ...(s.env ? { env: map(s.env) } : {}),
          tools: overrides.modes?.[name] ?? s.tools,
        },
      ]),
    ),
  };
}

const isEmpty = (a: TemplateAdds) =>
  a.tools.length === 0 &&
  Object.keys(a.env).length === 0 &&
  Object.keys(a.files).length === 0 &&
  Object.keys(a.mcp).length === 0;

/** Writes what a template adds into the file: tools not yet installed, then its variables, files and servers. */
export function addToText(text: string, adds: TemplateAdds): string {
  if (isEmpty(adds)) return text;
  return edit(text, (doc) => {
    const have = strings(obj(obj(doc.toJS()).spec).tools);
    const tools = [...have, ...adds.tools.filter((t) => !have.includes(t))];
    if (tools.length > 0) doc.setIn(['spec', 'tools'], tools);
    for (const [k, v] of Object.entries(adds.env)) doc.setIn(['spec', 'env', k], v);
    for (const [k, v] of Object.entries(adds.files)) doc.setIn(['spec', 'files', k], v);
    for (const [name, server] of Object.entries(adds.mcp)) {
      const node = doc.createNode(server);
      const tools = (node as YAMLMap).get('tools', true);
      if (isMap(tools)) tools.flow = true;
      doc.setIn(['spec', 'mcp', name], node);
    }
  });
}

/** The lines Add a tool shows under "Adds to environments/<name>.yaml": what the template writes, each with a plus. */
export function addsPreview(adds: TemplateAdds): string[] {
  const out: Record<string, unknown> = {};
  if (adds.tools.length > 0) out.tools = adds.tools;
  if (Object.keys(adds.env).length > 0) out.env = adds.env;
  if (Object.keys(adds.files).length > 0) out.files = adds.files;
  if (Object.keys(adds.mcp).length > 0) out.mcp = adds.mcp;
  if (Object.keys(out).length === 0) return [];
  const doc = parseDocument('{}');
  doc.contents = doc.createNode(out) as never;
  const mcp = doc.get('mcp', true);
  if (isMap(mcp)) {
    for (const server of mcp.items) {
      const tools = isMap(server.value) ? server.value.get('tools', true) : undefined;
      if (isMap(tools)) tools.flow = true;
    }
  }
  return doc.toString({ lineWidth: 0 }).trimEnd().split('\n');
}

/** The names of the secrets the file mentions, once each. */
export function secretsUsed(spec: EnvSpec): string[] {
  const values = [
    ...Object.values(spec.env),
    ...Object.values(spec.files),
    ...Object.values(spec.mcp).flatMap((s) => [...Object.values(s.headers), ...Object.values(s.env)]),
  ];
  return [...new Set(values.flatMap(secretsIn))];
}
