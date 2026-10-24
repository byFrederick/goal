import { describe, expect, it } from 'vitest';
import type { SkillFiles } from '../../../api/types';
import type { Model, Section, SectionValues } from '../../../shared/definition/model';
import {
  AgentInUseError,
  type AnyEdit,
  addAgent,
  addSkillFromCatalog,
  addStation,
  applyFix,
  copyAgent,
  duplicateAgent,
  moveStation,
  removeAgent,
  removeConnection,
  renameAgent,
  SkillExistsError,
  setBuiltin,
  setConnectionAgents,
  setStationKind,
  setStationName,
} from './ops';

const model = (): Model => ({
  basics: { name: 'goal', work_types: ['feature'], repository: { url: 'https://example.com/acme/goal' } },
  line: {
    stations: [
      { id: 'inbox', kind: 'queue' },
      { id: 'build', name: 'Build', kind: 'agent', route: { backend: 'engineer', frontend: 'web' }, wip: 2, agents: 2 },
      {
        id: 'review',
        name: 'Review',
        kind: 'agent',
        agent: 'engineer',
        wip: 1,
        gate: { on_reject: 'build', max_rework: 2 },
      },
      { id: 'merge', name: 'Merge', kind: 'human', action: 'merge' },
      { id: 'done', name: 'Done', kind: 'done' },
    ],
    flow: { constraint: 'build' },
    librarian: 'engineer',
  },
  agents: {
    agents: [
      {
        name: 'engineer',
        prompt: { path: './agents/engineer.md', words: 3 },
        skills: ['conventions'],
        connections: ['docs'],
      },
      { name: 'web', prompt: { path: './agents/web.md' } },
    ],
    skills: [{ name: 'conventions', source: './skills/conventions' }],
  },
  connections: [
    {
      name: 'github',
      connector: 'github',
      agents: ['engineer', 'web'],
      tools: { create_pr: { agents: ['engineer'] } },
    },
    { name: 'docs', type: 'mcp', legacy: true },
  ],
  review: { budget: { weekly_usd: 60 } },
});

/** The value of the edit of one section among several. */
function sectionValue<S extends Section>(edits: AnyEdit[], section: S): SectionValues[S] {
  const e = edits.find((x) => x.section === section);
  if (!e) throw new Error(`no ${section} edit`);
  return e.value as SectionValues[S];
}

const ids = (m: Model['line']) => m.stations?.map((s) => s.id);

describe('stations', () => {
  it('moves a station, and never past Done or Done itself', () => {
    const m = model();
    expect(ids(moveStation(m.line, 'review', 1)?.value ?? {})).toEqual(['inbox', 'review', 'build', 'merge', 'done']);
    expect(moveStation(m.line, 'merge', 4)).toBeUndefined();
    expect(moveStation(m.line, 'done', 2)).toBeUndefined();
    expect(moveStation(m.line, 'build', 1)).toBeUndefined(); // already there
    expect(m.line.stations?.[1]?.id).toBe('build'); // the input is untouched
  });

  it('adds a station before Done, or after the one given, with a free id', () => {
    const m = model();
    const before = addStation(m.line, 'agent', { name: 'Build' });
    expect(before.station).toMatchObject({ id: 'build-2', name: 'Build', kind: 'agent' });
    expect(ids(before.edit.value)).toEqual(['inbox', 'build', 'review', 'merge', 'build-2', 'done']);
    const after = addStation(m.line, 'human', { name: 'Approve spec', after: 'inbox' });
    expect(ids(after.edit.value)).toEqual(['inbox', 'approve-spec', 'build', 'review', 'merge', 'done']);
  });

  it('renames a new station with its id and every reference to it', () => {
    const { value } = setStationName(model().line, 'build', 'Make it', { followId: true });
    expect(ids(value)).toContain('make-it');
    expect(value.stations?.find((s) => s.id === 'review')?.gate?.on_reject).toBe('make-it');
    expect(value.flow?.constraint).toBe('make-it');
    expect(ids(setStationName(model().line, 'build', 'Make it', { followId: false }).value)).toContain('build');
  });

  it('keeps only the fields of the new kind and says which went', () => {
    const { edit, dropped } = setStationKind(model().line, 'review', 'human');
    expect(edit.value.stations?.find((s) => s.id === 'review')).toEqual({
      id: 'review',
      name: 'Review',
      kind: 'human',
      wip: 1,
      gate: { on_reject: 'build', max_rework: 2 },
    });
    expect(dropped).toEqual(['agent']);
  });
});

describe('agents', () => {
  it('adds an agent with its prompt file in the same save', () => {
    const edit = addAgent(model().agents, 'reviewer');
    expect(edit.value.agents?.at(-1)).toEqual({ name: 'reviewer', prompt: { path: './agents/reviewer.md' } });
    expect(edit.files).toEqual({ 'agents/reviewer.md': { text: '# reviewer\n' } });
  });

  it('duplicates an agent with a copy of its prompt', () => {
    const edit = duplicateAgent(model().agents, 'engineer', 'Build it.\n');
    expect(edit.value.agents?.at(-1)).toMatchObject({
      name: 'engineer-copy',
      prompt: { path: './agents/engineer-copy.md' },
      skills: ['conventions'],
    });
    expect(edit.files).toEqual({ 'agents/engineer-copy.md': { text: 'Build it.\n' } });
  });

  it('copies an agent from another factory with its prompt and new skill folders, not its connections', () => {
    const source = {
      name: 'engineer',
      description: 'Builds it.',
      model: 'claude-opus-5-5',
      prompt: { path: './agents/eng.md', sha256: 'abc', words: 2 },
      skills: ['conventions', 'terraform'],
      connections: ['github'],
    };
    const { edit, name } = copyAgent(model().agents, source, 'Build it.\n', [
      { skill: { name: 'conventions', source: './skills/conventions' }, files: { 'SKILL.md': { text: 'other' } } },
      {
        skill: {
          name: 'terraform',
          source: './skills/tf',
          origin: `skills.sh:acme/skills@terraform#${'a'.repeat(40)}`,
        },
        files: { 'SKILL.md': { text: '# terraform' }, 'refs/x.md': { text: 'x' } },
      },
    ]);
    expect(name).toBe('engineer-2');
    expect(edit.value.agents?.at(-1)).toEqual({
      name: 'engineer-2',
      description: 'Builds it.',
      model: 'claude-opus-5-5',
      prompt: { path: './agents/engineer-2.md' },
      skills: ['conventions', 'terraform'],
    });
    expect(edit.value.skills).toEqual([
      { name: 'conventions', source: './skills/conventions' },
      { name: 'terraform', source: './skills/terraform', origin: `skills.sh:acme/skills@terraform#${'a'.repeat(40)}` },
    ]);
    expect(edit.files).toEqual({
      'agents/engineer-2.md': { text: 'Build it.\n' },
      'skills/terraform/SKILL.md': { text: '# terraform' },
      'skills/terraform/refs/x.md': { text: 'x' },
    });
  });

  it('renames every reference: stations, lanes, the librarian, connections and the prompt file', () => {
    const edits = renameAgent(model(), 'engineer', 'builder', 'Build it.\n');
    const by = Object.fromEntries(edits.map((e) => [e.section, e]));

    const line = sectionValue(edits, 'line');
    expect(line.stations?.find((s) => s.id === 'build')?.route).toEqual({ backend: 'builder', frontend: 'web' });
    expect(line.stations?.find((s) => s.id === 'review')?.agent).toBe('builder');
    expect(line.librarian).toBe('builder');

    const agents = sectionValue(edits, 'agents');
    expect(agents.agents?.[0]).toMatchObject({ name: 'builder', prompt: { path: './agents/builder.md' } });
    expect(by.agents?.files).toEqual({ 'agents/builder.md': { text: 'Build it.\n' } });
    expect(by.agents?.removeFiles).toEqual(['agents/engineer.md']);

    const connections = sectionValue(edits, 'connections');
    expect(connections[0]).toMatchObject({ agents: ['builder', 'web'], tools: { create_pr: { agents: ['builder'] } } });
    expect(edits.map((e) => e.section).sort()).toEqual(['agents', 'connections', 'line']);
  });

  it('refuses to remove an agent a station uses, and removes it everywhere else', () => {
    expect(() => removeAgent(model(), 'engineer')).toThrow(AgentInUseError);
    const m = model();
    m.line.stations = (m.line.stations ?? []).map((s) =>
      s.id === 'build' ? { ...s, route: { backend: 'engineer' } } : s,
    );
    const edits = removeAgent(m, 'web');
    const by = Object.fromEntries(edits.map((e) => [e.section, e]));
    expect(sectionValue(edits, 'agents').agents?.map((a) => a.name)).toEqual(['engineer']);
    expect(by.agents?.removeFiles).toEqual(['agents/web.md']);
    expect(sectionValue(edits, 'connections')[0]?.agents).toEqual(['engineer']);
  });

  it('vendors a skills.sh skill: its files, the factory skill with its origin, and the agent', () => {
    const skill: SkillFiles = {
      skill: {
        key: 'acme/agent-skills/terraform',
        owner: 'acme',
        repo: 'agent-skills',
        skill: 'terraform',
        commit: 'a'.repeat(40),
        description: 'Terraform style',
        installs: 10,
        files: 2,
        bytes: 20,
      },
      files: { 'SKILL.md': { text: '# terraform\n' }, 'references/naming.md': { text: 'snake_case\n' } },
    };

    const edit = addSkillFromCatalog(model().agents, 'web', skill);

    expect(edit.files).toEqual({
      'skills/terraform/SKILL.md': { text: '# terraform\n' },
      'skills/terraform/references/naming.md': { text: 'snake_case\n' },
    });
    expect(edit.value.skills?.at(-1)).toEqual({
      name: 'terraform',
      source: './skills/terraform',
      origin: `skills.sh:acme/agent-skills@terraform#${'a'.repeat(40)}`,
    });
    expect(edit.value.agents?.find((a) => a.name === 'web')?.skills).toEqual(['terraform']);
    expect(() =>
      addSkillFromCatalog(model().agents, 'web', { ...skill, skill: { ...skill.skill, skill: 'conventions' } }),
    ).toThrow(SkillExistsError);
  });

  it('sets one built-in tool, keeping the others', () => {
    const m = model();
    const once = setBuiltin(m.agents, 'web', 'web', 'ask');
    const twice = setBuiltin(once.value, 'web', 'run', 'allowed');
    expect(twice.value.agents?.find((a) => a.name === 'web')?.builtins).toEqual({
      web: 'ask',
      run: { mode: 'allowed' },
    });
  });
});

describe('connections', () => {
  it("grants a connector-form connection to agents, and a legacy one through each agent's list", () => {
    const m = model();
    expect(setConnectionAgents(m, 'github', ['web'])).toMatchObject({
      section: 'connections',
      value: [{ agents: ['web'] }, {}],
    });
    const legacy = setConnectionAgents(m, 'docs', ['web']);
    expect(legacy.section).toBe('agents');
    const agents = (legacy.value as Model['agents']).agents;
    expect(agents?.map((a) => a.connections)).toEqual([[], ['docs']]);
  });

  it('removes a connection with every reference to it', () => {
    const edits = removeConnection(model(), 'docs');
    expect(sectionValue(edits, 'connections').map((c) => c.name)).toEqual(['github']);
    expect(sectionValue(edits, 'agents').agents?.[0]?.connections).toEqual([]);
  });
});

describe('fixes', () => {
  it("applies a problem's patch to its section", () => {
    const m = model();
    const edit = applyFix(m, {
      label: 'Set WIP to 2',
      section: 'line',
      patch: [{ op: 'replace', path: '/stations/2/wip', value: 2 }],
    });
    expect(edit.section).toBe('line');
    expect((edit.value as Model['line']).stations?.[2]?.wip).toBe(2);
    const removed = applyFix(m, {
      label: 'Remove the merge step',
      section: 'line',
      patch: [{ op: 'remove', path: '/stations/3/action' }],
    });
    expect((removed.value as Model['line']).stations?.[3]).toEqual({ id: 'merge', name: 'Merge', kind: 'human' });
    const added = applyFix(m, {
      label: 'Add',
      section: 'basics',
      patch: [{ op: 'add', path: '/work_types/0', value: 'fix' }],
    });
    expect((added.value as Model['basics']).work_types).toEqual(['fix', 'feature']);
  });
});
