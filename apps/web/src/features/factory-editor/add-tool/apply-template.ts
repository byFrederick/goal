// What Add a tool decides without a screen: which group a catalog template is in, whether an environment
// already has it, the secret picked for each of its slots, and the lines a custom tool adds. The writing
// into the environment file is env-file.ts's (addToText).
import type { TemplateAdds, ToolTemplate } from '../../../api/types';
import { type EnvSpec, secretRef } from '../environments/env-file';

export type ToolKind = 'mcp' | 'cli' | 'api';

export const kindTitles: Record<ToolKind, string> = {
  mcp: 'MCP servers',
  cli: 'Command-line tools',
  api: 'APIs',
};

/** The group of a template, by what it adds: an MCP server, else installed tools, else just variables and files. */
export function kindOf(template: ToolTemplate): ToolKind {
  if (Object.keys(template.adds.mcp).length > 0) return 'mcp';
  if (template.adds.tools.length > 0) return 'cli';
  return 'api';
}

/** The templates by group, in the order MCP servers, command-line tools, APIs; each in the catalog's order. */
export function groupTemplates(
  templates: readonly ToolTemplate[],
  query = '',
): { kind: ToolKind; templates: ToolTemplate[] }[] {
  const q = query.trim().toLowerCase();
  const shown = templates.filter(
    (t) =>
      q === '' || t.title.toLowerCase().includes(q) || t.name.includes(q) || t.description.toLowerCase().includes(q),
  );
  return (['mcp', 'cli', 'api'] as const)
    .map((kind) => ({ kind, templates: shown.filter((t) => kindOf(t) === kind) }))
    .filter((g) => g.templates.length > 0);
}

/** Whether the environment already has what the template adds: its servers, else its tools, else its variables. */
export function inEnvironment(template: ToolTemplate, spec: EnvSpec): boolean {
  const { mcp, tools, env, files } = template.adds;
  const servers = Object.keys(mcp);
  if (servers.length > 0) return servers.every((s) => s in spec.mcp);
  if (tools.length > 0) return tools.every((t) => spec.tools.includes(t));
  const keys = [...Object.keys(env).map((k) => spec.env[k]), ...Object.keys(files).map((k) => spec.files[k])];
  return keys.length > 0 && keys.every((v) => v !== undefined);
}

/** A secret slot's pick: one of the organization's secrets, or a new one with the value pasted here. */
export type SlotPick = { kind: 'existing'; name: string } | { kind: 'new'; name: string; value: string };

/** The secret name for every slot, or undefined while one is missing or a new one lacks a name or value. */
export function pickedNames(
  template: ToolTemplate,
  picks: Readonly<Record<string, SlotPick | undefined>>,
): Record<string, string> | undefined {
  const out: Record<string, string> = {};
  for (const slot of template.secrets) {
    const pick = picks[slot.slot];
    if (!pick || pick.name.trim() === '') return undefined;
    if (pick.kind === 'new' && pick.value === '') return undefined;
    out[slot.slot] = pick.name.trim();
  }
  return out;
}

/** The names a slot's pick would show before anything is chosen: the template's suggestion. */
export const suggested = (template: ToolTemplate): Record<string, SlotPick> =>
  Object.fromEntries(template.secrets.map((s) => [s.slot, { kind: 'new', name: s.suggest, value: '' } as SlotPick]));

export interface CustomRow {
  key: string;
  /** A plain value, used when `secret` is ''. */
  value: string;
  /** The name of a secret: the row's value is `{{name}}`. */
  secret: string;
}

export interface CustomTool {
  type: ToolKind;
  /** An MCP server's name. */
  name: string;
  url: string;
  /** The secret an MCP server's Authorization header carries ('' for none). */
  token: string;
  /** A command-line tool's mise spec. */
  install: string;
  variables: CustomRow[];
  files: CustomRow[];
}

export const blankCustom = (type: ToolKind = 'mcp'): CustomTool => ({
  type,
  name: '',
  url: '',
  token: '',
  install: '',
  variables: [],
  files: [],
});

// A secret of ' ' is a row switched to a secret and not picked yet: it writes its plain value (none).
const rowValue = (r: CustomRow) => (r.secret.trim() !== '' ? `{{${r.secret.trim()}}}` : r.value);
const rows = (list: readonly CustomRow[]) =>
  Object.fromEntries(list.filter((r) => r.key.trim() !== '').map((r) => [r.key.trim(), rowValue(r)]));

/** What a custom tool adds, by its type: a server, an installed tool with its variables and files, or variables. */
export function customAdds(tool: CustomTool): TemplateAdds {
  const none: TemplateAdds = { tools: [], env: {}, files: {}, mcp: {} };
  if (tool.type === 'mcp') {
    const name = tool.name.trim();
    if (name === '' || tool.url.trim() === '') return none;
    return {
      ...none,
      mcp: {
        [name]: {
          url: tool.url.trim(),
          ...(tool.token !== '' ? { headers: { Authorization: `Bearer {{${tool.token}}}` } } : {}),
          tools: {},
        },
      },
    };
  }
  return {
    ...none,
    tools: tool.type === 'cli' && tool.install.trim() !== '' ? [tool.install.trim()] : [],
    env: rows(tool.variables),
    files: tool.type === 'cli' ? rows(tool.files) : {},
  };
}

const slug = /^[a-z][a-z0-9_-]{0,39}$/;

/** Why a custom tool cannot be added yet, in a sentence; undefined when it can. */
export function customProblem(tool: CustomTool): string | undefined {
  if (tool.type === 'mcp') {
    if (!slug.test(tool.name.trim())) return 'Name the server with lowercase letters, digits, dashes and underscores.';
    if (!/^https?:\/\/\S+$/.test(tool.url.trim())) return 'Give the server’s address, starting with https://.';
    return undefined;
  }
  if (tool.type === 'cli' && !/^\S+@\S+$/.test(tool.install.trim())) return 'Give a tool spec such as kubectl@1.31.';
  const unpicked = [...tool.variables, ...tool.files].find(
    (r) => r.key.trim() !== '' && r.secret !== '' && r.secret.trim() === '',
  );
  if (unpicked) return `Pick a secret for ${unpicked.key}.`;
  const bad = tool.variables.find((r) => r.key.trim() !== '' && !/^[A-Za-z_][A-Za-z0-9_]*$/.test(r.key.trim()));
  if (bad) return `${bad.key} is not a variable name.`;
  const badFile = tool.files.find((r) => r.key.trim() !== '' && !/^(\/|~\/).+/.test(r.key.trim()));
  if (badFile) return `${badFile.key} is not an absolute path or one that starts with ~/.`;
  if (tool.type === 'api' && Object.keys(rows(tool.variables)).length === 0)
    return 'Add the variables the API call reads.';
  return undefined;
}

/** The secret a slot's `{{slot}}` value names, when a header holds exactly that. */
export const slotOf = (value: string) => secretRef(value);
