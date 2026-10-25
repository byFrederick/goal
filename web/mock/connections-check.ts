// The mock's checks of phase 4's definition forms (spec 4 §4.4), as goald's config and authoring
// report them: `tools` (tool-spec-invalid), `removed` (field-removed, env-reference-removed and
// connection-inline-removed, warnings until the cutover) and `connections` (connection-unknown,
// connection-not-model, repository-host-mismatch, connection-env-conflict and
// knowledge-source-unknown, errors), each problem at its model pointer with goald's fixes.
import type { DefinitionProblem, ProblemFix } from '@goal/api-client';
import type { ConnectionOutline } from './connections.ts';
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
  ['auth', 'runtime.auth', 'set runtime.modelConnection to an organization connection with model access'],
  ['secrets', 'spec.secrets', 'a run gets its secrets from the connections it uses'],
  [
    'knowledge_base',
    'spec.knowledgeBase',
    "list the organization's knowledge sources the factory may search in knowledge.sources",
  ],
];

export function checkConnections(model: Model, conns: ConnectionOutline[], add: Add): void {
  const basics = isObject(model.basics) ? model.basics : {};
  const agents = list(isObject(model.agents) ? model.agents.agents : undefined);
  const connections = list(model.connections);
  const byName = new Map(conns.map((c) => [c.name, c]));
  const names = [...byName.keys()].sort();

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
    add('field-removed', 'warning', 'removed', `/basics/${key}`, `${path} is removed in phase 4: ${why}`, [
      remove(`Remove ${path}`, 'basics', `/${key}`),
    ]);
  }
  const repository = isObject(basics.repository) ? basics.repository : undefined;
  if (typeof repository?.token === 'string') {
    add(
      'env-reference-removed',
      'warning',
      'removed',
      '/basics/repository/token',
      'runtime.repository.token is removed in phase 4: name a connection that signs git in to the host',
      [remove('Remove the token', 'basics', '/repository/token')],
    );
  }
  connections.forEach((c, i) => {
    if (isOrgConnection(c)) return;
    add(
      'connection-inline-removed',
      'warning',
      'removed',
      `/connections/${i}`,
      `connection ${c.name}: inline connections are removed in phase 4; create an organization connection named ${c.name}`,
      [replace(`Use the organization’s ${c.name}`, 'connections', `/${i}`, { name: c.name })],
    );
  });

  // connections: every organization connection it names exists
  connections.forEach((c, i) => {
    if (!isOrgConnection(c) || byName.has(String(c.name))) return;
    const named = agents.some((a) => strings(a.connections).includes(String(c.name)));
    add(
      'connection-unknown',
      'error',
      'connections',
      `/connections/${i}`,
      `connection ${c.name}: the organization has no connection of that name; create it on the Connections page or remove it`,
      named ? [] : [remove(`Remove ${c.name}`, 'connections', `/${i}`)],
    );
  });

  // its model connection gives model access
  const modelName = typeof basics.model_connection === 'string' ? basics.model_connection : '';
  if (modelName) {
    const fixes = names
      .filter((n) => byName.get(n)?.model)
      .map((n) => replace(`Use ${n}`, 'basics', '/model_connection', n));
    const c = byName.get(modelName);
    if (!c) {
      add(
        'connection-unknown',
        'error',
        'connections',
        '/basics/model_connection',
        `runtime.modelConnection: the organization has no connection named ${modelName}`,
        fixes,
      );
    } else if (!c.model) {
      const titles = [...new Set(conns.filter((x) => x.model).map((x) => x.title))].sort();
      add(
        'connection-not-model',
        'error',
        'connections',
        '/basics/model_connection',
        `runtime.modelConnection: ${modelName} is a ${c.title} connection, which gives no model access; choose one of a kind that does (${titles.join(', ')})`,
        fixes,
      );
    }
  }

  // its repository's connection signs git in to the repository's host
  const repoName = typeof repository?.connection === 'string' ? repository.connection : '';
  if (repoName) {
    let host = '';
    try {
      host = new URL(String(repository?.url ?? '')).host;
    } catch {}
    const fixes = [
      ...names
        .filter((n) => n !== repoName && host && byName.get(n)?.gitHost.toLowerCase() === host.toLowerCase())
        .map((n) => replace(`Use ${n}`, 'basics', '/repository/connection', n)),
      remove('Clone without a connection', 'basics', '/repository/connection'),
    ];
    const got = byName.get(repoName);
    const at = '/basics/repository/connection';
    if (!got) {
      add(
        'connection-unknown',
        'error',
        'connections',
        at,
        `runtime.repository: the organization has no connection named ${repoName}`,
        fixes,
      );
    } else if (!got.gitHost) {
      add(
        'repository-host-mismatch',
        'error',
        'connections',
        at,
        `runtime.repository: ${repoName} signs git in to no host, so it cannot clone the repository on ${host}`,
        fixes,
      );
    } else if (got.gitHost.toLowerCase() !== host.toLowerCase()) {
      add(
        'repository-host-mismatch',
        'error',
        'connections',
        at,
        `runtime.repository: ${repoName} signs git in to ${got.gitHost}, not to ${host}, the repository's host`,
        fixes,
      );
    }
  }

  // no two connections an agent's runs get set the same variable
  const listed = new Set(connections.filter(isOrgConnection).map((c) => String(c.name)));
  const shared = (a: string, b: string) => {
    const theirs = byName.get(b)?.env ?? [];
    return (byName.get(a)?.env ?? []).filter((v) => theirs.includes(v));
  };
  const runtime = [...new Set([modelName, repoName].filter((n) => n && byName.has(n)))];
  agents.forEach((a, i) => {
    const runs: { name: string; index: number }[] = runtime.map((name) => ({ name, index: -1 }));
    strings(a.connections).forEach((n, j) => {
      if (listed.has(n) && byName.has(n) && !runs.some((r) => r.name === n)) runs.push({ name: n, index: j });
    });
    runs.forEach((b, y) => {
      for (const x of runs.slice(0, y)) {
        if (b.index < 0) continue;
        const vars = shared(x.name, b.name);
        if (vars.length === 0) continue;
        const entry = (c: { index: number }) => `/agents/${i}/connections/${c.index}`;
        add(
          'connection-env-conflict',
          'error',
          'connections',
          `/agents${entry(b)}`,
          `agent ${a.name}: ${x.name} and ${b.name} both set ${vars.join(', ')}; a run gets one value per variable, so keep one`,
          [
            remove(`Keep ${x.name}`, 'agents', entry(b)),
            ...(x.index >= 0 ? [remove(`Keep ${b.name}`, 'agents', entry(x))] : []),
          ],
        );
      }
    });
  });

  // every knowledge source it may search exists: none do before the knowledge base (4-16)
  strings(basics.knowledge_sources).forEach((source, i) => {
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

/** The connection names a factory model uses: its organization connections, model and repository's. */
export function connectionNames(model: Model): string[] {
  const basics = isObject(model.basics) ? model.basics : {};
  const repository = isObject(basics.repository) ? basics.repository : {};
  const out = list(model.connections)
    .filter(isOrgConnection)
    .map((c) => String(c.name));
  for (const n of [basics.model_connection, repository.connection]) {
    if (typeof n === 'string' && n && !out.includes(n)) out.push(n);
  }
  return out;
}
