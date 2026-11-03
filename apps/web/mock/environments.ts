// The mock's environments (ADR 0037 decisions 5, 14 and 15): the tool catalog (GET /v1/catalog), the
// environment test (POST …/factories/{factory}/environments/{environment}/test), the seeded
// environments/default.yaml and deploy.yaml of the goal factory, and the plain-key check of the
// definition's validation. The test answers from fixtures by the server's host or name; a secret whose
// name holds "bad" is refused as an expired key would be.
import type { IncomingMessage, ServerResponse } from 'node:http';
import { parse } from 'yaml';
import type { DefinitionProblem, EnvironmentTestResult, ServerTest, ToolTemplate } from '../src/api/types.ts';

type Json = Record<string, unknown>;

const mcp = (url: string, token: string, tools: Record<string, 'allow' | 'ask' | 'off'> = {}) => ({
  url,
  headers: { Authorization: `Bearer {{${token}}}` },
  tools,
});

const slot = (slot: string, title: string, description: string, suggest: string) => ({
  slot,
  title,
  description,
  suggest,
});

const template = (
  name: string,
  title: string,
  description: string,
  secrets: ToolTemplate['secrets'],
  adds: Partial<ToolTemplate['adds']>,
  test: ToolTemplate['test'] = null,
): ToolTemplate => ({
  name,
  title,
  description,
  secrets,
  adds: { tools: [], env: {}, files: {}, mcp: {}, ...adds },
  test,
});

/** The tool templates goald ships, as the picture lists them. */
export const catalog: ToolTemplate[] = [
  template(
    'sentry',
    'Sentry',
    'Errors and releases',
    [slot('token', 'Sentry auth token', 'An auth token', 'sentry-token')],
    {
      mcp: { sentry: mcp('https://mcp.sentry.dev/mcp', 'token', { 'update_*': 'ask', 'delete_*': 'off' }) },
    },
  ),
  template(
    'linear',
    'Linear',
    'Issues and projects',
    [slot('key', 'Linear API key', 'A personal API key', 'linear-key')],
    {
      mcp: { linear: mcp('https://mcp.linear.app/mcp', 'key', { 'create_*': 'ask', 'delete_*': 'off' }) },
    },
  ),
  template(
    'notion',
    'Notion',
    'Pages and databases',
    [slot('token', 'Notion integration token', 'An integration token', 'notion-token')],
    {
      mcp: { notion: mcp('https://mcp.notion.com/mcp', 'token', { 'update_*': 'ask' }) },
    },
  ),
  template('playwright', 'Playwright', 'Drive a browser', [], {
    mcp: { playwright: { command: ['npx', '@playwright/mcp@0.0.41'], tools: {} } },
  }),
  template(
    'figma',
    'Figma',
    'Designs and components',
    [slot('token', 'Figma access token', 'A personal access token', 'figma-token')],
    {
      mcp: { figma: mcp('https://mcp.figma.com/mcp', 'token') },
    },
  ),
  template(
    'github-cli',
    'GitHub CLI',
    'gh with a token',
    [slot('token', 'GitHub token', 'A token with repo scope', 'github-token')],
    {
      tools: ['gh@2'],
      env: { GH_TOKEN: '{{token}}' },
    },
  ),
  template(
    'wrangler',
    'Wrangler',
    'Cloudflare Workers and R2',
    [slot('token', 'Cloudflare API token', 'A scoped API token', 'cloudflare-deploy')],
    {
      tools: ['wrangler@4'],
      env: { CLOUDFLARE_API_TOKEN: '{{token}}' },
    },
  ),
  template(
    'kubectl',
    'kubectl',
    'A cluster’s kubeconfig',
    [slot('kubeconfig', 'Kubeconfig', 'The cluster’s kubeconfig file', 'kube-staging')],
    {
      tools: ['kubectl@1.31'],
      files: { '~/.kube/config': '{{kubeconfig}}' },
    },
  ),
  template(
    'datadog',
    'Datadog',
    'Metrics, logs, monitors',
    [slot('key', 'Datadog API key', 'An API key', 'dd-api-key')],
    { env: { DD_API_KEY: '{{key}}', DD_SITE: 'datadoghq.com' } },
    {
      method: 'GET',
      // biome-ignore lint/suspicious/noTemplateCurlyInString: the template's own ${VAR} syntax
      url: 'https://api.${DD_SITE}/api/v1/validate',
      headers: { 'DD-API-KEY': '{{key}}' },
      expect: 200,
    },
  ),
  template(
    'stripe',
    'Stripe',
    'Payments, a restricted key',
    [slot('key', 'Stripe restricted key', 'A restricted key', 'stripe-key')],
    {
      env: { STRIPE_API_KEY: '{{key}}' },
    },
  ),
];

const reads = (names: string[]) => names;

/** The tools each server answers tools/list with, by host (remote) or command. */
const serverTools: Record<string, string[]> = {
  'mcp.sentry.dev': [
    ...reads(['search_issues', 'get_issue_details', 'list_events', 'get_trace', 'list_releases', 'get_project']),
    'update_issue',
    'update_project',
    'delete_issue',
  ],
  'mcp.linear.app': [
    ...reads([
      'list_issues',
      'get_issue',
      'search_issues',
      'list_projects',
      'get_project',
      'list_teams',
      'get_team',
      'list_users',
      'list_comments',
      'list_cycles',
      'list_labels',
      'get_user',
    ]),
    'create_comment',
    'update_issue_status',
    'create_issue',
    'update_issue',
    'delete_issue',
    'archive_project',
  ],
  'mcp.context7.com': ['resolve-library-id', 'get-library-docs'],
  'mcp.notion.com': ['search_pages', 'get_page', 'list_databases', 'update_page'],
  'mcp.figma.com': ['get_file', 'get_node', 'list_components'],
};

const hostOf = (url: string) => /^https?:\/\/([^/]+)/.exec(url)?.[1] ?? '';

const text = (v: unknown) => (typeof v === 'string' ? v : '');
const obj = (v: unknown): Json => (typeof v === 'object' && v !== null && !Array.isArray(v) ? (v as Json) : {});

/** The environment test of one server: stdio is not started, a remote one answers with its tools. */
function testServer(name: string, server: Json, secretsOk: boolean, now: string): ServerTest {
  if (Array.isArray(server.command)) return { server: name, tested: false, ok: false, tools: [], at: now };
  const tools = serverTools[hostOf(text(server.url))] ?? [];
  if (!secretsOk)
    return {
      server: name,
      tested: true,
      ok: false,
      tools: [],
      error: '401 Unauthorized: the key was refused',
      at: now,
    };
  if (tools.length === 0)
    return { server: name, tested: true, ok: false, tools: [], error: 'No answer from the server', at: now };
  return { server: name, tested: true, ok: true, tools: [...tools].sort(), at: now };
}

export interface EnvironmentsDeps {
  clock: () => number;
  /** The text of a file of the factory's draft (the current version without one), or undefined. */
  fileText: (org: string, factory: string, path: string) => string | undefined;
}

export interface MockEnvironments {
  /** Serves …/factories/{factory}/environments/{environment}/test; false when rest is not that. */
  handle: (
    org: { id: string },
    rest: string,
    method: string,
    res: ServerResponse,
    body: () => Promise<Record<string, unknown>>,
  ) => Promise<boolean>;
}

const reply = (res: ServerResponse, status: number, body: unknown) => {
  res.statusCode = status;
  res.setHeader('Content-Type', status >= 400 ? 'application/problem+json' : 'application/json');
  res.end(JSON.stringify(body));
};

const refuse = (res: ServerResponse, status: number, title: string, code: string, detail?: string) =>
  reply(res, status, { title, status, code, ...(detail ? { detail } : {}) });

export function createEnvironments(deps: EnvironmentsDeps): MockEnvironments {
  return {
    handle: async (org, rest, method, res, body) => {
      const m = /^\/factories\/([^/]+)\/environments\/([^/]+)\/test$/.exec(rest);
      if (!m || method !== 'POST') return false;
      const factory = decodeURIComponent(m[1] ?? '');
      const environment = decodeURIComponent(m[2] ?? '');
      const now = new Date(deps.clock()).toISOString();
      const request = await body();
      const named = obj(request.template);

      if (typeof named.name === 'string') {
        const t = catalog.find((x) => x.name === named.name);
        if (!t) {
          refuse(res, 404, 'Template not found', 'template-not-found');
          return true;
        }
        const picked = obj(named.secrets);
        const missing = t.secrets.find((s) => typeof picked[s.slot] !== 'string' || picked[s.slot] === '');
        if (missing) {
          refuse(res, 400, 'Invalid test', 'environment-test-invalid', `the slot ${missing.slot} has no secret`);
          return true;
        }
        const ok = !Object.values(picked).some((n) => String(n).includes('bad'));
        const answer: EnvironmentTestResult = {
          environment,
          version: 'draft',
          servers: Object.entries(t.adds.mcp)
            .filter(([, s]) => s.url)
            .map(([name, s]) => testServer(name, { url: s.url }, ok, now)),
          ...(t.test
            ? {
                template: {
                  name: t.name,
                  ok,
                  status: ok ? 200 : 401,
                  ...(ok ? {} : { error: '401 Unauthorized' }),
                  at: now,
                },
              }
            : {}),
        };
        reply(res, 200, answer);
        return true;
      }

      const file = deps.fileText(org.id, factory, `environments/${environment}.yaml`);
      if (file === undefined) {
        refuse(res, 404, 'Environment not found', 'environment-not-found');
        return true;
      }
      let servers: Json = {};
      try {
        servers = obj(obj(obj(parse(file)).spec).mcp);
      } catch {}
      const only = typeof request.server === 'string' ? request.server : undefined;
      if (only !== undefined && !(only in servers)) {
        refuse(res, 404, 'Server not found', 'environment-server-not-found');
        return true;
      }
      const answer: EnvironmentTestResult = {
        environment,
        version: 'draft',
        servers: Object.entries(servers)
          .filter(([name]) => only === undefined || name === only)
          .map(([name, s]) => testServer(name, obj(s), !JSON.stringify(s).includes('bad'), now)),
      };
      reply(res, 200, answer);
      return true;
    },
  };
}

/** GET /v1/catalog. */
export function serveCatalog(req: IncomingMessage, res: ServerResponse): boolean {
  if (req.method !== 'GET') return false;
  reply(res, 200, { templates: catalog });
  return true;
}

const header = (name: string, description: string) =>
  `apiVersion: goal/v1\nkind: Environment\nmetadata:\n  name: ${name}\n  description: ${description}\n`;

/** environments/default.yaml and deploy.yaml of the goal factory, with the picture's content. */
export const environmentFiles: Record<string, string> = {
  'environments/default.yaml': `${header('default', 'What every agent without an environment gets.')}spec:
  size: medium
  # The repository's own mise.toml comes first, then these.
  tools: [go@1.26, node@22, pnpm@9, gh@2, task@3]
  setup:
    - pnpm install --frozen-lockfile
  env:
    GH_TOKEN: "{{github-token}}"
    DD_API_KEY: "{{dd-api-key}}"
    SENTRY_ORG: byfrederick
    LINEAR_API_KEY: lin_api_8fK2q9Zx0m3Wd
  mcp:
    linear:
      url: https://mcp.linear.app/mcp
      headers: { Authorization: "Bearer {{linear-key}}" }
      tools: { "create_*": ask, "delete_*": off, "archive_*": off }
    context7:
      url: https://mcp.context7.com/mcp
    playwright:
      command: [npx, "@playwright/mcp@0.0.41"]
`,
  'environments/deploy.yaml': `${header('deploy', 'Infrastructure changes: the cluster and Cloudflare.')}spec:
  size: large
  tools: [wrangler@4, gh@2, kubectl@1.31]
  files:
    ~/.kube/config: "{{kube-staging}}"
  env:
    CLOUDFLARE_API_TOKEN: "{{cloudflare-deploy}}"
    KUBE_CONTEXT: staging
`,
};

const keyPrefixes = ['sk-', 'ghp_', 'github_pat_', 'glpat-', 'xoxb-', 'xoxp-'];
const keyName = /(KEY|TOKEN|SECRET|PASSWORD)$/;

/** plain-key (ADR 0037 decision 7), as the definition's check reports it for each environment file of the tree. */
export function environmentProblems(files: ReadonlyMap<string, Uint8Array>): DefinitionProblem[] {
  const out: DefinitionProblem[] = [];
  for (const [path, bytes] of files) {
    const m = /^environments\/([^/]+)\.yaml$/.exec(path);
    if (!m) continue;
    const name = m[1] ?? '';
    const source = new TextDecoder().decode(bytes);
    let env: Json = {};
    try {
      env = obj(obj(obj(parse(source)).spec).env);
    } catch {
      continue;
    }
    for (const [key, value] of Object.entries(env)) {
      const v = text(value);
      if (v === '' || v.includes('{{')) continue;
      const looksLikeKey = keyPrefixes.some((p) => v.startsWith(p));
      if (!looksLikeKey && !keyName.test(key)) continue;
      const line = source.split('\n').findIndex((l) => l.trim().startsWith(`${key}:`)) + 1;
      const secret = key.toLowerCase().replaceAll('_', '-');
      out.push({
        code: 'plain-key',
        severity: looksLikeKey ? 'error' : 'warning',
        check: 'secrets',
        section: 'environments',
        pointer: `/environments/${name}/env/${key}`,
        file: path,
        line,
        column: 1,
        end_line: line,
        end_column: 0,
        message: looksLikeKey
          ? `${key} holds a key in plain text.`
          : `${key} looks like a key. Keep keys in Secrets so they never sit in a file.`,
        fixes: [
          {
            label: 'Move to Secrets',
            section: 'environments',
            patch: [{ op: 'replace', path: `/${name}/env/${key}`, value: `{{${secret}}}` }],
          },
        ],
      });
    }
  }
  return out;
}
