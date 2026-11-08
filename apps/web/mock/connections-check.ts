// The mock's checks of phase 4's definition forms (spec 4 §4.4), as goald's config and authoring
// report them: `tools` (tool-spec-invalid), `removed` (field-removed, env-reference-removed and
// connection-inline-removed, errors since the cutover) and `connections` (knowledge-source-unknown,
// an error), each problem at its model pointer with goald's fixes. goald no longer checks a
// definition against organization connections (ADR 0037), so the mock does not either.
import type { DefinitionProblem, ProblemFix } from '@goal/api-client';
import type { Model } from './model.ts';

type Json = Record<string, unknown>;

type Add = (
  code: string,
  severity: DefinitionProblem['severity'],
  check: string,
  pointer: string,
  message: string,
  fixes?: ProblemFix[],
) => void;

const isObject = (v: unknown): v is Json => typeof v === 'object' && v !== null && !Array.isArray(v);
const list = (v: unknown): Json[] => (Array.isArray(v) ? v.filter(isObject) : []);
const strings = (v: unknown): string[] => (Array.isArray(v) ? v.filter((x) => typeof x === 'string') : []);

/** config's toolSpec: an optional backend, a tool name and an optional @version. */
export const toolSpec = /^(?:[a-z][a-z0-9-]*:)?@?[A-Za-z0-9][A-Za-z0-9._/+-]*(?:@[A-Za-z0-9][A-Za-z0-9._+:-]*)?$/;

const remove = (label: string, section: string, path: string): ProblemFix => ({
  label,
  section,
  patch: [{ op: 'remove', path }],
});
const replace = (label: string, section: string, path: string, value: unknown): ProblemFix => ({
  label,
  section,
  patch: [{ op: 'replace', path, value }],
});

/** An organization connection in the model: {name} alone, not an inline or connector form. */
export const isOrgConnection = (c: Json) => typeof c.name === 'string' && !c.type && !c.connector;

/** The keys phase 4 removed from Basics, with what replaces each. */
const removedKeys: [string, string, string][] = [
  ['runner', 'runtime.runner', 'the platform chooses where runs start'],
  [
    'image',
    'runtime.image',
    "every run starts from the platform's agent image and installs the repository's tools and runtime.tools",
  ],
  ['auth', 'runtime.auth', 'set runtime.modelKey to the {{secret}} that holds the model key'],
  ['secrets', 'spec.secrets', 'a run gets its secrets from the connections it uses'],
  [
    'knowledge_base',
    'spec.knowledgeBase',
    "list the organization's knowledge sources the factory may search in knowledge.sources",
  ],
];

export function checkConnections(model: Model, sources: string[], add: Add): void {
  const basics = isObject(model.basics) ? model.basics : {};
  const connections = list(model.connections);

  // tools
  strings(basics.tools).forEach((t, i) => {
    if (toolSpec.test(t)) return;
    add(
      'tool-spec-invalid',
      'error',
      'tools',
      `/basics/tools/${i}`,
      `runtime.tools: "${t}" is not a mise tool spec, such as go, go@1.26.10 or npm:@acme/cli@2`,
      [remove(`Remove ${t}`, 'basics', `/tools/${i}`)],
    );
  });

  // removed
  for (const [key, path, why] of removedKeys) {
    if (basics[key] === undefined) continue;
    add('field-removed', 'error', 'removed', `/basics/${key}`, `${path} is removed: ${why}`, [
      remove(`Remove ${path}`, 'basics', `/${key}`),
    ]);
  }
  connections.forEach((c, i) => {
    if (isOrgConnection(c)) return;
    add(
      'connection-inline-removed',
      'error',
      'removed',
      `/connections/${i}`,
      `connection ${c.name}: inline connections are removed; create an organization connection named ${c.name}`,
      [replace(`Use the organization’s ${c.name}`, 'connections', `/${i}`, { name: c.name })],
    );
  });

  // every knowledge source it may search is one of the organization's
  strings(basics.knowledge_sources).forEach((source, i) => {
    if (sources.includes(source)) return;
    add(
      'knowledge-source-unknown',
      'error',
      'connections',
      `/basics/knowledge_sources/${i}`,
      `knowledge.sources: the organization has no knowledge source named ${source}`,
      [remove(`Remove ${source}`, 'basics', `/knowledge_sources/${i}`)],
    );
  });
}

/** The knowledge sources a factory model may search (config.Factory.Knowledge.Sources). */
export function knowledgeSourceNames(model: Model): string[] {
  const basics = isObject(model.basics) ? model.basics : {};
  return strings(basics.knowledge_sources);
}
