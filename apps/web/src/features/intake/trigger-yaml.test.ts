import { describe, expect, it } from 'vitest';
import { parse } from 'yaml';
import { TriggerNotFoundError, withTriggerEnabled } from './trigger-yaml';

const file = `# The software factory. Keep this comment.
apiVersion: goal/v1
kind: Factory
metadata:
  name: widgets
spec:
  board:
    workTypes: [fix, feature]
    triggers:
      - name: flaky   # the first one
        on: github.check_run.failed
        create: { type: fix, station: build }
      - name: deploy
        enabled: true
        on: schedule
        create: { type: fix, station: spec }
`;

describe('withTriggerEnabled', () => {
  it("sets only the named trigger's enabled and keeps every other line and comment", () => {
    const next = withTriggerEnabled(file, 'flaky', false);

    expect(next).toContain('# The software factory. Keep this comment.');
    expect(next).toContain('# the first one');
    expect(parse(next).spec.board.triggers).toEqual([
      { name: 'flaky', on: 'github.check_run.failed', create: { type: 'fix', station: 'build' }, enabled: false },
      { name: 'deploy', enabled: true, on: 'schedule', create: { type: 'fix', station: 'spec' } },
    ]);
    const lines = (text: string) => text.split('\n');
    expect(lines(next).filter((line) => !lines(file).includes(line))).toEqual(['        enabled: false']);
    expect(lines(file).filter((line) => !lines(next).includes(line))).toEqual([]);
  });

  it('changes an enabled that is already written, in its place', () => {
    const next = withTriggerEnabled(file, 'deploy', false);

    expect(parse(next).spec.board.triggers[1].enabled).toBe(false);
    expect(next.split('\n').filter((line) => line.includes('enabled'))).toEqual(['        enabled: false']);
  });

  it('refuses a name the definition does not have', () => {
    expect(() => withTriggerEnabled(file, 'missing', false)).toThrow(TriggerNotFoundError);
  });

  it('refuses a file that does not parse', () => {
    expect(() => withTriggerEnabled('spec: [', 'flaky', false)).toThrow(/does not parse/);
  });
});
