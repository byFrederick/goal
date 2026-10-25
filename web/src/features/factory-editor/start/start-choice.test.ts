import { describe, expect, it } from 'vitest';
import type { DefinitionProblem, Validation } from '../../../api/types';
import {
  agentsSentence,
  freeName,
  importVerdict,
  lineChips,
  nameProblem,
  newFactoryRequest,
  sourceSentence,
  suggestedName,
} from './start-choice';
import { filesFromUpload } from './upload';

const problem = (
  code: string,
  check: string,
  severity: DefinitionProblem['severity'] = 'error',
): DefinitionProblem => ({
  code,
  check,
  severity,
  section: 'files',
  line: 3,
  column: 1,
  end_line: 3,
  end_column: 9,
  message: code,
  fixes: [],
});

const validation = (...problems: DefinitionProblem[]): Validation => ({
  ok: !problems.some((p) => p.severity === 'error'),
  revision: 0,
  counts: { stations: 0, agents: 0, skills: 0, connections: 0, contracts: 0, changes: 0 },
  checks: [],
  problems,
});

describe('the Start step', () => {
  it('offers a free name', () => {
    expect(freeName('cost-review', ['goal', 'sre'])).toBe('cost-review');
    expect(freeName('cost-review', ['cost-review', 'cost-review-2'])).toBe('cost-review-3');
    expect(freeName('a'.repeat(40), ['a'.repeat(40)])).toBe(`${'a'.repeat(38)}-2`);
    expect(suggestedName('blank', {}, ['new-factory'])).toBe('new-factory-2');
    expect(suggestedName('clone', { clone: 'sre' }, [])).toBe('sre-copy');
    expect(suggestedName('template', {}, [])).toBe('');
    expect(suggestedName('yaml', {}, [])).toBe('');
  });

  it('checks a name against the schema’s slug', () => {
    expect(nameProblem('cloud-costs')).toBeNull();
    expect(nameProblem('Cloud')).not.toBeNull();
    expect(nameProblem('9lives')).not.toBeNull();
    expect(nameProblem('ab')).not.toBeNull();
  });

  it('builds the request only once the choice is complete', () => {
    expect(newFactoryRequest({ kind: 'template', name: 'cloud-costs' })).toBeNull();
    expect(newFactoryRequest({ kind: 'template', name: 'cloud-costs', template: 'cost-review' })).toEqual({
      name: 'cloud-costs',
      start: { kind: 'template', template: 'cost-review' },
    });
    expect(newFactoryRequest({ kind: 'clone', name: ' sre-copy ', clone: 'f1' })).toEqual({
      name: 'sre-copy',
      start: { kind: 'clone', factory: 'f1' },
    });
    const files = { 'factory.yaml': { text: 'kind: Factory\n' } };
    // An import without a name keeps factory.yaml's.
    expect(newFactoryRequest({ kind: 'yaml', name: '', files })).toEqual({ start: { kind: 'import', files } });
    expect(newFactoryRequest({ kind: 'blank', name: '' })).toBeNull();
  });

  it('describes a template’s agents and line', () => {
    expect(
      agentsSentence([
        { name: 'cost-analyst', description: 'Reads the cost report.' },
        { name: 'proposer', description: 'Drafts the fix' },
        { name: 'reviewer' },
      ]),
    ).toBe('cost-analyst reads the cost report, proposer drafts the fix and reviewer.');
    expect(agentsSentence([])).toBe('No agents yet.');
    expect(
      lineChips([
        { id: 'approve-spec', kind: 'human' },
        { id: 'done', name: 'Done', kind: 'done' },
      ]),
    ).toEqual([
      { id: 'approve-spec', label: 'Approve spec', human: true },
      { id: 'done', label: 'Done', human: false },
    ]);
  });

  it('says where a new factory came from', () => {
    expect(sourceSentence({ kind: 'template', template: 'cost-review@2' }, { templateTitle: 'Cost review' })).toBe(
      'From the Cost review template.',
    );
    expect(sourceSentence({ kind: 'template', template: 'cost-review@2' }, {})).toBe('From the Cost review template.');
    expect(sourceSentence({ kind: 'clone', factory: 'f1', version: '3' }, { factoryName: 'sre' })).toBe(
      'A copy of sre v3.',
    );
    expect(sourceSentence({ kind: 'blank' }, {})).toBe('A blank factory.');
    expect(sourceSentence({ kind: 'import' }, {})).toBe('Imported from YAML.');
  });

  it('lets an import continue once it parses and passes the schema', () => {
    expect(importVerdict(validation(problem('schema-required', 'schema'))).ok).toBe(false);
    expect(importVerdict(validation(problem('yaml-invalid', 'yaml'))).ok).toBe(false);
    const v = importVerdict(
      validation(
        problem('prompt-not-found', 'references'),
        problem('name-taken', 'name'),
        problem('builtin-ask-unavailable', 'availability', 'warning'),
      ),
    );
    expect(v.ok).toBe(true);
    expect(v.nameTaken?.code).toBe('name-taken');
    expect(v.later.map((p) => p.code)).toEqual(['prompt-not-found', 'builtin-ask-unavailable']);
  });
});

describe('an upload', () => {
  const enc = (s: string) => new TextEncoder().encode(s);

  it('roots a folder at the definition and leaves hidden files out', () => {
    const r = filesFromUpload([
      { path: 'acme/factory.yaml', bytes: enc('kind: Factory\n') },
      { path: 'acme/agents/engineer.md', bytes: enc('# engineer\n') },
      { path: 'acme/.DS_Store', bytes: new Uint8Array([0, 1, 2]) },
      { path: 'acme/skills/x/logo.png', bytes: new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0, 0xff]) },
    ]);
    expect(r).toEqual({
      files: {
        'factory.yaml': { text: 'kind: Factory\n' },
        'agents/engineer.md': { text: '# engineer\n' },
        'skills/x/logo.png': { base64: btoa(String.fromCharCode(0x89, 0x50, 0x4e, 0x47, 0, 0xff)) },
      },
    });
  });

  it('takes one file as the factory.yaml and refuses a folder without one', () => {
    expect(filesFromUpload([{ path: 'cost.yml', bytes: enc('a: 1\n') }])).toEqual({
      files: { 'factory.yaml': { text: 'a: 1\n' } },
    });
    expect(filesFromUpload([{ path: 'acme/agents/a.md', bytes: enc('x') }])).toHaveProperty('error');
  });

  it('refuses more than 5 MiB', () => {
    const big = new Uint8Array(5 * 1024 * 1024 + 1).fill(97);
    expect(filesFromUpload([{ path: 'factory.yaml', bytes: big }])).toEqual({
      error: 'That is more than 5 MiB; a definition holds at most 5 MiB.',
    });
  });
});
