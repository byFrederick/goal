// A factory rule's file (ADR 0014): a short markdown file under rules/ whose frontmatter says what
// the rule is for (`description`, one line), whom it applies to (`appliesTo`: agents, stations,
// lanes, workTypes; an empty list is left out, and no appliesTo means every agent) and which
// files it is scoped to (`paths`, Claude Code's globs). The keys are the ones internal/config reads.
import { parse, stringify } from 'yaml';

export interface RuleScope {
  agents?: string[];
  stations?: string[];
  lanes?: string[];
  workTypes?: string[];
}

export interface RuleText {
  description: string;
  appliesTo?: RuleScope;
  paths?: string[];
  body: string;
}

const scopeKeys = ['agents', 'stations', 'lanes', 'workTypes'] as const satisfies readonly (keyof RuleScope)[];

/** The scope with its non-empty lists only; undefined when it targets every agent. */
function scopeOf(scope: { [K in keyof RuleScope]?: string[] | undefined } | undefined): RuleScope | undefined {
  const kept: RuleScope = {};
  for (const key of scopeKeys) {
    const list = scope?.[key];
    if (list && list.length > 0) kept[key] = list;
  }
  return Object.keys(kept).length > 0 ? kept : undefined;
}

/** The file's text: frontmatter (quoted where YAML needs it), a blank line, the body. */
export function serializeRule(rule: RuleText): string {
  const front: Record<string, unknown> = { description: rule.description };
  const appliesTo = scopeOf(rule.appliesTo);
  if (appliesTo) front.appliesTo = appliesTo;
  if (rule.paths && rule.paths.length > 0) front.paths = rule.paths;
  return `---\n${stringify(front)}---\n\n${rule.body}`;
}

const frontMatter = /^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/;

/** A YAML mapping, or undefined when the header is not one. */
function mappingOf(header: string): Record<string, unknown> | undefined {
  try {
    const value: unknown = parse(header);
    return value && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, unknown>) : undefined;
  } catch {
    return undefined;
  }
}

/** The strings of a YAML list; undefined when there are none. */
function stringsOf(value: unknown): string[] | undefined {
  if (!Array.isArray(value)) return undefined;
  const list = value.filter((v): v is string => typeof v === 'string');
  return list.length > 0 ? list : undefined;
}

/**
 * A rule file read back. A file without frontmatter (or with one that is not a mapping) is all
 * body, with no description: the editor shows it as it is and saving writes the frontmatter.
 */
export function parseRule(text: string): RuleText {
  const match = frontMatter.exec(text);
  const front = match ? mappingOf(match[1] ?? '') : undefined;
  if (!match || !front) return { description: '', body: text };

  const appliesRaw = front.appliesTo && typeof front.appliesTo === 'object' ? front.appliesTo : undefined;
  const scope = appliesRaw as Record<string, unknown> | undefined;
  const appliesTo = scopeOf({
    agents: stringsOf(scope?.agents),
    stations: stringsOf(scope?.stations),
    lanes: stringsOf(scope?.lanes),
    workTypes: stringsOf(scope?.workTypes),
  });
  const paths = stringsOf(front.paths);
  // The blank line serializeRule writes after the frontmatter is not part of the body.
  const body = text.slice(match[0].length).replace(/^\r?\n/, '');
  return {
    description: typeof front.description === 'string' ? front.description : '',
    ...(appliesTo ? { appliesTo } : {}),
    ...(paths ? { paths } : {}),
    body,
  };
}
