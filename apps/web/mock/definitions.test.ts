import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import type {
  BuilderMeta,
  Draft,
  DraftModel,
  Factory,
  Problem,
  SaveResult,
  SkillFiles,
  SkillPage,
  Validation,
  Version,
} from '@goal/api-client';
import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { createMockApi, type MockApi } from './api.ts';
import { seedDefinitions, seedTemplates } from './definition-seed.ts';
import { formatYaml } from './model.ts';

let api: MockApi;
let server: Server;
let base = '';

beforeAll(async () => {
  api = createMockApi();
  server = createServer((req, res) =>
    api.handle(req, res, () => {
      res.statusCode = 404;
      res.end();
    }),
  );
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  base = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
});

afterAll(async () => {
  api.close();
  await new Promise((resolve) => server.close(resolve));
});

beforeEach(async () => {
  await fetch(`${base}/__mock/reset`, { method: 'POST' });
});

const goal = `/v1/orgs/acme/factories/goal`;

async function call<T>(path: string, init: RequestInit = {}): Promise<{ status: number; body: T; res: Response }> {
  const res = await fetch(`${base}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...(init.headers ?? {}) },
  });
  const text = await res.text();
  let body: unknown = text;
  try {
    body = JSON.parse(text);
  } catch {}
  return { status: res.status, body: body as T, res };
}

const put = (body: unknown, etag?: string) => ({
  method: 'PUT',
  body: JSON.stringify(body),
  headers: etag ? { 'If-Match': `"${etag}"` } : {},
});

type Station = Record<string, unknown>;

async function lineSave(change: (stations: Station[]) => void) {
  const { body: m } = await call<DraftModel>(`${goal}/draft/model`);
  const line = structuredClone(m.model.line) as { stations: Station[] };
  change(line.stations);
  return { model: m, value: line };
}

describe('the draft', () => {
  it('opens goal with its changes, problems, etags and edited fields', async () => {
    const { body: draft } = await call<Draft>(`${goal}/draft`);
    expect(draft).toMatchObject({ revision: 42, next_number: 9, change_count: 3, problem_count: 1 });
    expect(draft.base?.number).toBe(8);
    expect(draft.sections.find((s) => s.section === 'line')).toMatchObject({ changes: 2, problems: 0 });
    expect(draft.sections.find((s) => s.section === 'agents')).toMatchObject({ changes: 1, problems: 1 });

    const { body: m } = await call<DraftModel>(`${goal}/draft/model`);
    expect(Object.keys(m.etags)).toEqual(['basics', 'line', 'agents', 'connections', 'review']);
    expect(m.edited).toEqual({
      '/line/stations/3/checks': ['task check'],
      '/line/stations/4/gate/max_rework': 1,
      '/agents/agents/2/builtins/web': 'off',
    });
    expect(m.ranges['/line/stations/3']?.[0]).toBeGreaterThan(0);
  });

  it('saves a section on its etag and keeps the comments of factory.yaml', async () => {
    const { model, value } = await lineSave((s) => {
      (s[3] as Station).wip = 3;
    });

    const saved = await call<SaveResult>(`${goal}/draft/model/line`, put({ value }, model.etags.line));

    expect(saved.status).toBe(200);
    expect(saved.body.revision).toBe(43);
    const file = await call<string>(`${goal}/draft/files/factory.yaml`);
    expect(file.body).toContain("# the spec's lane picks the engineer");
    expect(file.body).toContain('        wip: 3\n');
    expect(file.body.startsWith('# Goal builds itself')).toBe(true);
    const after = await call<DraftModel>(`${goal}/draft/model`);
    expect(after.body.etags.line).toBe(saved.body.etag);
  });

  it('refuses a stale etag with the section as it is now', async () => {
    const { model, value } = await lineSave((s) => {
      (s[3] as Station).wip = 3;
    });
    await call(`${goal}/draft/model/line`, put({ value }, model.etags.line));

    const stale = await call<Problem>(`${goal}/draft/model/line`, put({ value }, model.etags.line));

    expect(stale.status).toBe(412);
    expect(stale.body).toMatchObject({ code: 'draft-conflict', section: 'line' });
    expect(stale.body.etag).not.toBe(model.etags.line);
    expect((stale.body.value as { stations: Station[] }).stations[3]?.wip).toBe(3);
  });

  it('asks for a precondition on every draft write', async () => {
    const { value } = await lineSave(() => {});
    const res = await call<Problem>(`${goal}/draft/model/line`, put({ value }));
    expect(res.status).toBe(428);
    expect(res.body.code).toBe('precondition-required');
  });

  it('reports a station with an unknown agent, with fixes, and refuses to publish it', async () => {
    const { model, value } = await lineSave((s) => {
      (s[1] as Station).agent = 'ghost';
    });
    await call(`${goal}/draft/model/line`, put({ value }, model.etags.line));

    const { body: v } = await call<Validation>(`${goal}/draft/validate`, { method: 'POST', body: '{}' });
    const problem = v.problems.find((p) => p.code === 'unknown-agent');
    expect(v.ok).toBe(false);
    expect(problem).toMatchObject({ section: 'line', pointer: '/line/stations/1/agent', severity: 'error' });
    expect(problem?.fixes[0]?.patch[0]).toMatchObject({ op: 'replace', path: '/stations/1/agent' });

    const refused = await call<Problem>(`${goal}/draft/publish`, {
      method: 'POST',
      body: JSON.stringify({
        revision: v.revision,
        expected_current_version_id: '019a1c00-0000-7000-8000-0000000000d8',
      }),
    });
    expect(refused.status).toBe(409);
    expect(refused.body.code).toBe('definition-invalid');
    expect(refused.body.problems?.some((p) => p.code === 'unknown-agent')).toBe(true);
  });

  it('asks where the items queued at a removed station go, and publishes with the move', async () => {
    const { model, value } = await lineSave((s) => {
      s.splice(0, 1); // inbox, where #14, #15, #145 and #147 wait
    });
    await call(`${goal}/draft/model/line`, put({ value }, model.etags.line));

    const validate = (body: unknown) =>
      call<Validation>(`${goal}/draft/validate`, { method: 'POST', body: JSON.stringify(body) });
    const { body: v } = await validate({});
    const problem = v.problems.find((p) => p.code === 'items-at-removed-station');
    expect(v.ok).toBe(false);
    expect(problem?.message).toMatch(/removes station inbox, where 4 items are waiting/);
    expect(problem?.fixes[0]).toMatchObject({
      label: 'Move them to spec',
      section: 'publish',
      patch: [{ op: 'add', path: '/station_moves/inbox', value: 'spec' }],
    });
    expect(v.checks.find((c) => c.id === 'items')?.status).toBe('failed');

    const moved = await validate({ station_moves: { inbox: 'spec' } });
    expect(moved.body.problems.some((p) => p.code === 'items-at-removed-station')).toBe(false);

    const res = await call<Version>(`${goal}/draft/publish`, {
      method: 'POST',
      body: JSON.stringify({
        revision: v.revision,
        expected_current_version_id: '019a1c00-0000-7000-8000-0000000000d8',
        station_moves: { inbox: 'spec' },
      }),
    });
    expect(res.status).toBe(201);
  });

  it('publishes the draft as v9, which then runs', async () => {
    const res = await call<Version>(`${goal}/draft/publish`, {
      method: 'POST',
      body: JSON.stringify({
        revision: 42,
        expected_current_version_id: '019a1c00-0000-7000-8000-0000000000d8',
        message: 'Build also runs pnpm test',
      }),
    });

    expect(res.status).toBe(201);
    expect(res.body).toMatchObject({ number: 9, current: true, message: 'Build also runs pnpm test' });
    const { body: factory } = await call<Factory>(goal);
    expect(factory.current?.number).toBe(9);
    expect(factory.draft).toBeNull();
    expect((await call<Problem>(`${goal}/draft`)).body.code).toBe('no-draft');
  });

  it('restores an older version as a pointer move', async () => {
    const res = await call<Factory>(`${goal}/versions/7/restore`, {
      method: 'POST',
      body: JSON.stringify({ expected_current_version_id: '019a1c00-0000-7000-8000-0000000000d8' }),
    });
    expect(res.status).toBe(200);
    expect(res.body.current?.number).toBe(7);
    expect(res.body.stations.find((s) => s.id === 'merge')?.action).toBeUndefined();
  });

  it('creates a factory from a template with its comments and a first draft', async () => {
    const res = await call<Factory>('/v1/orgs/acme/factories', {
      method: 'POST',
      body: JSON.stringify({ name: 'cloud-costs', start: { kind: 'template', template: 'cost-review' } }),
    });
    expect(res.status).toBe(201);
    expect(res.body).toMatchObject({ name: 'cloud-costs', current: null, draft: { revision: 1 } });

    const yaml = await call<string>(`/v1/orgs/acme/factories/${res.body.id}/draft/files/factory.yaml`);
    expect(yaml.body).toContain('# Cost review template v2.');
    expect(yaml.body).toContain('name: cloud-costs');
    expect(yaml.body).toContain(`id: ${res.body.id}`);

    const taken = await call<Problem>('/v1/orgs/acme/factories', {
      method: 'POST',
      body: JSON.stringify({ name: 'goal', start: { kind: 'blank' } }),
    });
    expect(taken.body.code).toBe('name-taken');
  });
});

describe('a blank factory', () => {
  it('starts from goald’s skeleton and publishes once the line, an agent and its model connection are added', async () => {
    const { body: f } = await call<Factory>('/v1/orgs/acme/factories', {
      method: 'POST',
      body: JSON.stringify({ name: 'blank-one', start: { kind: 'blank' } }),
    });
    const path = `/v1/orgs/acme/factories/${f.id}`;
    const { body: draft } = await call<Draft>(`${path}/draft`);
    expect(draft.source).toEqual({ kind: 'blank' });

    const before = await call<Validation>(`${path}/draft/validate`, { method: 'POST', body: '{}' });
    expect(before.body.problems.filter((p) => p.code === 'schema-required').map((p) => p.pointer)).toEqual([
      '/line/stations',
      '/agents/agents',
    ]);

    const { body: m } = await call<DraftModel>(`${path}/draft/model`);
    const agents = { agents: [{ name: 'worker', prompt: { path: './agents/worker.md' } }] };
    const files = { 'agents/worker.md': { text: '# worker\n' } };
    expect((await call(`${path}/draft/model/agents`, put({ value: agents, files }, m.etags.agents))).status).toBe(200);
    const line = {
      stations: [
        { id: 'inbox', kind: 'queue' },
        { id: 'work', kind: 'agent', agent: 'worker', wip: 1 },
        { id: 'done', kind: 'done' },
      ],
    };
    const lined = await call<SaveResult>(`${path}/draft/model/line`, put({ value: line }, m.etags.line));
    expect(lined.status).toBe(200);

    // An agent's runs need a model: publish refuses until Basics names the model connection.
    const refused = await call<Problem>(`${path}/draft/publish`, {
      method: 'POST',
      body: JSON.stringify({ revision: lined.body.revision }),
    });
    expect([refused.status, refused.body.problems?.map((p) => p.code)]).toEqual([409, ['model-connection-missing']]);
    const basics = { ...(m.model.basics as Record<string, unknown>), model_connection: 'claude' };
    const saved = await call<SaveResult>(`${path}/draft/model/basics`, put({ value: basics }, m.etags.basics));
    expect(saved.status).toBe(200);

    const published = await call<Version>(`${path}/draft/publish`, {
      method: 'POST',
      body: JSON.stringify({ revision: saved.body.revision }),
    });
    expect(published.status).toBe(201);
    expect(published.body.number).toBe(1);
  });
});

describe('the seed', () => {
  it('writes every factory.yaml the way a Visual save keeps it', () => {
    const trees = [
      ...Object.values(seedDefinitions()).flatMap((d) =>
        [...d.versions, ...(d.draft ? [d.draft] : [])].map((v) => v.files),
      ),
      ...seedTemplates().map((t) => t.files),
    ];
    for (const t of trees) {
      const text = new TextDecoder().decode(t.get('factory.yaml'));
      expect(formatYaml(text)).toBe(text);
    }
  });
});

describe('fault injection', () => {
  it('answers the next matching request with the status asked for, then serves again', async () => {
    await call('/__mock/fail', {
      method: 'POST',
      body: JSON.stringify({ path: '/draft/model/line', method: 'PUT', status: 409, code: 'definition-invalid' }),
    });
    const { model, value } = await lineSave(() => {});

    const failed = await call<Problem>(`${goal}/draft/model/line`, put({ value }, model.etags.line));
    expect(failed.status).toBe(409);
    expect(failed.body.code).toBe('definition-invalid');
    expect(failed.body.problems?.length).toBeGreaterThan(0);

    const served = await call<SaveResult>(`${goal}/draft/model/line`, put({ value }, model.etags.line));
    expect(served.status).toBe(200);
  });

  it('answers a 412 on a section save with the section as it is now', async () => {
    await call('/__mock/fail', { method: 'POST', body: JSON.stringify({ path: '/draft/model/line', status: 412 }) });
    const { model, value } = await lineSave(() => {});

    const failed = await call<Problem>(`${goal}/draft/model/line`, put({ value }, model.etags.line));

    expect(failed.status).toBe(412);
    expect(failed.body).toMatchObject({ code: 'draft-conflict', section: 'line', etag: model.etags.line });
  });

  it('delays a request', async () => {
    await call('/__mock/fail', { method: 'POST', body: JSON.stringify({ path: '/draft', delay_ms: 60 }) });
    const started = performance.now();
    const res = await call<Draft>(`${goal}/draft`);
    expect(res.status).toBe(200);
    expect(performance.now() - started).toBeGreaterThanOrEqual(55);
  });
});

describe('the builder', () => {
  it('lists the models and what is wired', async () => {
    const { body } = await call<BuilderMeta>('/v1/orgs/acme/builder/meta');
    expect(body.models.filter((m) => m.default)).toHaveLength(1);
    expect(body.capabilities).toEqual({ tool_approvals: false });
  });

  it('searches the catalog, most installed first, a page at a time', async () => {
    const first = await call<SkillPage>('/v1/orgs/acme/builder/skills?limit=2');
    expect(first.body.skills.map((s) => s.skill)).toEqual(['terraform-style-guide', 'aws-cost-explorer']);
    const next = await call<SkillPage>(`/v1/orgs/acme/builder/skills?limit=2&after=${first.body.next_after}`);
    expect(next.body).toMatchObject({ skills: [{ skill: 'paper-notes' }], next_after: null });
    expect((await call<SkillPage>('/v1/orgs/acme/builder/skills?q=COST')).body.skills).toHaveLength(1);
    expect((await call<Problem>('/v1/orgs/acme/builder/skills?limit=51')).status).toBe(400);
  });

  it('serves a skill folder, or skill-not-found', async () => {
    const { body } = await call<SkillFiles>(
      '/v1/orgs/acme/builder/skills/acme/agent-skills/terraform-style-guide/files',
    );
    expect(Object.keys(body.files)).toEqual(['SKILL.md', 'references/naming.md']);
    const missing = await call<Problem>('/v1/orgs/acme/builder/skills/acme/agent-skills/nope/files');
    expect(missing.body.code).toBe('skill-not-found');
  });
});

describe('the stream', () => {
  it('sends a definition event for every draft save', async () => {
    const ctrl = new AbortController();
    const stream = await fetch(`${base}/v1/orgs/acme/stream`, { signal: ctrl.signal });
    const reader = (stream.body as ReadableStream<Uint8Array>).getReader();
    await reader.read(); // retry line

    const { model, value } = await lineSave((s) => {
      (s[3] as Station).wip = 3;
    });
    await call(`${goal}/draft/model/line`, put({ value }, model.etags.line));

    const chunk = new TextDecoder().decode((await reader.read()).value);
    ctrl.abort();
    expect(chunk).toContain('event: definition');
    expect(chunk).toContain('"kind":"draft"');
    expect(chunk).toContain('"revision":43');
  });
});
