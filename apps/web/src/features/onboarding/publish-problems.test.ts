import { describe, expect, it } from 'vitest';
import type { DefinitionProblem } from '../../api/types';
import { explainProblems, stepsToLink, withoutSchemaPath } from './publish-problems';

const problem = (p: Partial<DefinitionProblem>): DefinitionProblem =>
  ({ code: 'x', severity: 'error', message: 'm', ...p }) as DefinitionProblem;

describe('explainProblems', () => {
  it('sends line and agents problems to the Line step', () => {
    const out = explainProblems([
      problem({ section: 'line', message: 'station Build has no agent' }),
      problem({ section: 'agents', message: 'agent Coder has no model' }),
    ]);
    expect(out.map((p) => p.step)).toEqual(['line', 'line']);
    expect(out[0]?.sentence).toBe('Station Build has no agent. Fix it on the Line step.');
  });

  it('sends the repository to Code and other basics to Factory', () => {
    const out = explainProblems([
      problem({ section: 'basics', pointer: '/basics/repository/url', message: 'bad url' }),
      problem({ section: 'basics', pointer: '/basics/name', message: 'name is empty' }),
    ]);
    expect(out.map((p) => p.step)).toEqual(['code', 'factory']);
  });

  it('keeps budget problems on Budget', () => {
    const out = explainProblems([
      problem({ section: 'review', pointer: '/review/budget/daily_usd', message: 'too low' }),
    ]);
    expect(out).toEqual([{ sentence: 'Too low.', step: 'budget' }]);
  });

  it('reads a missing agents or board at the top of the file as a Line problem', () => {
    const out = explainProblems([
      problem({ section: 'files', code: 'schema-required', pointer: '', message: "spec: missing property 'agents'" }),
      problem({
        section: 'files',
        code: 'schema-required',
        pointer: '',
        message: "spec.board: missing property 'stations'",
      }),
    ]);
    expect(out).toEqual([
      { sentence: 'The factory has no agents. Add one on the Line step.', step: 'line' },
      { sentence: 'The line has no stations. Add one on the Line step.', step: 'line' },
    ]);
  });

  it('shows an unknown section by its message without the schema path, with no step', () => {
    expect(explainProblems([problem({ section: 'foo', message: 'spec.foo: bad' })])).toEqual([
      { sentence: 'Bad.', step: undefined },
    ]);
    expect(
      explainProblems([
        problem({ section: 'files', code: 'schema-required', message: "spec: missing property 'x'" }),
      ])[0]?.step,
    ).toBeUndefined();
  });

  it('drops warnings and info', () => {
    expect(explainProblems([problem({ severity: 'warning' }), problem({ severity: 'info' })])).toEqual([]);
  });
});

describe('stepsToLink', () => {
  it('lists each step once and never Budget or an unknown step', () => {
    const steps = stepsToLink(
      explainProblems([
        problem({ section: 'line', message: 'a' }),
        problem({ section: 'agents', message: 'b' }),
        problem({ section: 'review', pointer: '/review/budget', message: 'c' }),
        problem({ section: 'zzz', message: 'd' }),
      ]),
    );
    expect(steps).toEqual(['line']);
  });
});

describe('withoutSchemaPath', () => {
  it('removes a leading spec path', () => {
    expect(withoutSchemaPath('spec.board: missing property')).toBe('missing property');
    expect(withoutSchemaPath('spec: bad')).toBe('bad');
    expect(withoutSchemaPath('plain')).toBe('plain');
  });
});
