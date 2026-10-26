import { describe, expect, it } from 'vitest';
import {
  applySection,
  edited,
  formatYaml,
  ModelInvalidError,
  ParseError,
  readModel,
  revert,
  sectionEtag,
} from './model.ts';

// A small factory.yaml in the style goald keeps: comments everywhere, flow and block stations,
// a route whose comment must survive Visual saves.
const doc = `# acme's line, edited by people and the builder alike
apiVersion: goal/v1
kind: Factory
metadata:
  id: 0b6f1c9e-5d0a-4c39-9a51-6f1f3f0e2a10
  name: acme-line
  description: Builds acme's app. # the legacy alias of purpose
spec:
  runtime:
    model: claude-sonnet-5-5
    repository: { url: "https://git.example.com/acme/line.git", token: env:ACME_GIT_TOKEN }
  agents:
    - name: architect
      prompt: ./agents/architect.md
      maxTurns: 40 # long specs
    - name: engineer
      prompt: ./agents/engineer.md
      builtins: { read: allowed, run: { mode: allowed, commands: [make] } }
  board:
    workTypes: [feature, fix]
    stations:
      - { id: inbox, kind: queue }
      # the architect writes the spec
      - { id: spec, kind: agent, agent: architect, wip: 2 }
      - id: build
        kind: agent
        # the spec's lane picks the engineer
        route: { backend: engineer, frontend: engineer }
        wip: 2 # two at a time
        checks: [make test]
      - { id: done, kind: done }
`;

const files = new Map([
  ['agents/architect.md', 'Write the spec for the item.\n'],
  ['agents/engineer.md', 'Build it.\n'],
]);

type Json = Record<string, unknown>;

function model() {
  const read = readModel(doc, files);
  if (read instanceof ParseError) throw read;
  return read;
}

const clone = <T>(v: T): T => structuredClone(v);

function stations(line: unknown): Json[] {
  return (line as { stations: Json[] }).stations;
}

describe('readModel', () => {
  it('maps camelCase keys to snake_case and prompts to their file', () => {
    const { model: m } = model();
    expect(m.basics).toMatchObject({
      name: 'acme-line',
      purpose: "Builds acme's app.",
      work_types: ['feature', 'fix'],
    });
    expect(m.agents).toMatchObject({ default_model: 'claude-sonnet-5-5' });
    const [architect] = (m.agents as { agents: Json[] }).agents;
    expect(architect).toMatchObject({ name: 'architect', max_turns: 40 });
    expect(architect?.prompt).toMatchObject({ path: './agents/architect.md', words: 6 });
    expect(stations(m.line)[2]).toMatchObject({ id: 'build', route: { backend: 'engineer' }, checks: ['make test'] });
  });

  it('places every model pointer on the lines that hold it', () => {
    const { ranges } = model();
    const lines = doc.split('\n');
    const [first, last] = ranges['/line/stations/2'] ?? [0, 0];
    expect(lines[first - 1]).toContain('- id: build');
    expect(lines[last - 1]).toContain('checks: [make test]');
    expect(lines[(ranges['/line/stations/2/wip'] ?? [0])[0] - 1]).toContain('wip: 2 # two at a time');
    expect(lines[(ranges['/basics/purpose'] ?? [0])[0] - 1]).toContain('description:');
  });

  it('reports where a document stops parsing', () => {
    const broken = readModel('spec:\n  board: [\n', files);
    expect(broken).toBeInstanceOf(ParseError);
    expect((broken as ParseError).line).toBeGreaterThan(0);
  });
});

describe('applySection', () => {
  it('keeps every comment and the key order when one field changes', () => {
    const line = clone(model().model.line);
    (stations(line)[2] as Json).wip = 4;

    const out = applySection(doc, 'line', line);

    expect(out).toBe(doc.replace('wip: 2 # two at a time', 'wip: 4 # two at a time'));
  });

  it('leaves the file byte for byte as it was when nothing changes', () => {
    for (const section of ['basics', 'line', 'agents', 'connections', 'review'] as const) {
      expect(applySection(doc, section, clone(model().model[section]))).toBe(doc);
    }
  });

  it('inserts a new station in its siblings style without touching the others', () => {
    const line = clone(model().model.line);
    stations(line).splice(3, 0, { id: 'review', kind: 'human' });

    const out = applySection(doc, 'line', line);

    expect(out).toBe(
      doc.replace(
        '      - { id: done, kind: done }',
        '      - { id: review, kind: human }\n      - { id: done, kind: done }',
      ),
    );
  });

  it('moves a reordered station with its comment, matched by id', () => {
    const line = clone(model().model.line);
    const list = stations(line);
    const [spec] = list.splice(1, 1);
    list.splice(2, 0, spec as Json);

    const out = applySection(doc, 'line', line);

    const order = [...out.matchAll(/id: ([a-z]+)/g)].map((m) => m[1]);
    expect(order).toEqual(['inbox', 'build', 'spec', 'done']);
    expect(out).toContain(
      '      # the architect writes the spec\n      - { id: spec, kind: agent, agent: architect, wip: 2 }',
    );
    expect(out).toContain("# the spec's lane picks the engineer");
  });

  it('inserts a new key in the canonical order and removes a key with its comment', () => {
    const agents = clone(model().model.agents) as { agents: Json[] };
    const [architect] = agents.agents;
    if (!architect) throw new Error('no architect');
    delete architect.max_turns;
    architect.model = 'claude-opus-5-5';

    const out = applySection(doc, 'agents', agents);

    expect(out).toContain('      prompt: ./agents/architect.md\n      model: claude-opus-5-5\n    - name: engineer');
    expect(out).not.toContain('long specs');
  });

  it('writes the purpose to the legacy description when only that key is there', () => {
    const basics = { ...(clone(model().model.basics) as Json), purpose: 'Builds and ships acme.' };

    const out = applySection(doc, 'basics', basics);

    expect(out).toContain('  description: Builds and ships acme. # the legacy alias of purpose');
    expect(out).not.toContain('purpose:');
  });

  it('adds an agent with its prompt reference and keeps the others', () => {
    const agents = clone(model().model.agents) as { agents: Json[] };
    agents.agents.push({ name: 'reviewer', prompt: { path: './agents/reviewer.md' } });

    const out = applySection(doc, 'agents', agents);

    expect(out).toContain('    - name: reviewer\n      prompt: ./agents/reviewer.md\n  board:');
    expect(out).toContain('maxTurns: 40 # long specs');
  });

  it('refuses a value of the wrong shape', () => {
    expect(() => applySection(doc, 'line', [])).toThrow(ModelInvalidError);
    expect(() => applySection(doc, 'connections', {})).toThrow(ModelInvalidError);
    expect(() => applySection(doc, 'line', { stations: [], maxRework: 1 })).toThrow(ModelInvalidError);
  });

  it('refuses a document that does not parse', () => {
    expect(() => applySection('spec: [\n', 'line', {})).toThrow(ParseError);
  });
});

describe('phase 4 forms (spec 4 §4.4)', () => {
  const v4 = `apiVersion: goal/v1
kind: Factory
metadata:
  name: acme-line
spec:
  runtime:
    model: claude-sonnet-5-5
    repository: { url: "https://github.com/acme/line" }
  connections:
    - github # pull requests
    - { name: legacy-mcp, type: mcp, url: "https://mcp.example.com/" }
`;
  const read = () => {
    const r = readModel(v4);
    if (r instanceof ParseError) throw r;
    return r.model;
  };

  it('reads an organization connection as {name} and an inline one as its form', () => {
    const connections = read().connections as Json[];
    expect(connections[0]).toEqual({ name: 'github' });
    expect(connections[1]).toMatchObject({ name: 'legacy-mcp', type: 'mcp', legacy: true });
  });

  it('writes an added organization connection as its bare name and keeps the comments', () => {
    const connections = read().connections as Json[];
    const out = applySection(v4, 'connections', [...connections, { name: 'datadog' }]);
    expect(out).toContain('    - github # pull requests\n');
    expect(out).toContain('    - datadog\n');
  });

  it('writes an inline connection replaced by the organization connection of its name as the name', () => {
    const connections = read().connections as Json[];
    const out = applySection(v4, 'connections', [connections[0], { name: 'legacy-mcp' }]);
    expect(out).toContain('    - legacy-mcp\n');
    expect(out).not.toContain('mcp.example.com');
  });

  it('writes the runtime keys in their place and reads them back', () => {
    const basics = read().basics as Json;
    const out = applySection(v4, 'basics', {
      ...basics,
      model_connection: 'claude',
      size: 'large',
      tools: ['go@1.26.10'],
      repository: { url: 'https://github.com/acme/line', connection: 'github' },
      knowledge_sources: ['handbook'],
    });
    expect(out).toContain('    model: claude-sonnet-5-5\n    modelConnection: claude\n    size: large\n    tools:');
    expect(out).toContain('connection: github');
    expect(out).toContain('  knowledge:\n    sources:');
    const back = readModel(out);
    if (back instanceof ParseError) throw back;
    expect(back.model.basics).toMatchObject({
      model_connection: 'claude',
      size: 'large',
      tools: ['go@1.26.10'],
      knowledge_sources: ['handbook'],
    });
  });

  it('reads the removed keys so their fixes can take them out', () => {
    const old = v4.replace('    model:', '    runner: docker\n    image: ghcr.io/acme/agent:1\n    model:');
    const r = readModel(old);
    if (r instanceof ParseError) throw r;
    const basics = r.model.basics as Json;
    expect(basics).toMatchObject({ runner: 'docker', image: 'ghcr.io/acme/agent:1' });
    const { runner: _r, image: _i, ...rest } = basics;
    expect(applySection(old, 'basics', rest)).toBe(v4);
  });
});

describe('sectionEtag', () => {
  it('names a value, whatever the order of its keys', () => {
    expect(sectionEtag({ a: 1, b: [{ c: 2, d: 3 }] })).toBe(sectionEtag({ b: [{ d: 3, c: 2 }], a: 1 }));
    expect(sectionEtag({ a: 1 })).not.toBe(sectionEtag({ a: 2 }));
    expect(sectionEtag({ a: 1 })).toMatch(/^[0-9a-f]{16}$/);
  });
});

describe('edited and revert', () => {
  it('marks a changed field with the base value and a moved station as nothing', () => {
    const base = model().model;
    const draft = clone(base);
    const list = stations(draft.line);
    const [spec] = list.splice(1, 1);
    list.splice(2, 0, spec as Json);
    (list[1] as Json).wip = 3; // build, now at index 1

    expect(edited(base, draft, 'line')).toEqual({ '/stations/1/wip': 2 });
  });

  it('marks a new station once and puts the base back on revert', () => {
    const base = model().model;
    const draft = clone(base);
    stations(draft.line).splice(3, 0, { id: 'review', kind: 'human' });
    (stations(draft.line)[2] as Json).wip = 5;

    const marks = edited(base, draft, 'line');
    expect(marks).toEqual({ '/stations/3': null, '/stations/2/wip': 2 });

    const back = revert(draft.line, marks, ['/stations/3', '/stations/2/wip']);
    expect(back).toEqual(base.line);
  });
});

describe('formatYaml', () => {
  it('keeps a formatted file as it is', () => {
    expect(formatYaml(doc)).toBe(doc);
  });

  it('normalizes indentation', () => {
    expect(formatYaml('a:\n    b: 1\n')).toBe('a:\n  b: 1\n');
  });
});
