// The trigger's YAML: what the page reads, the one trigger it rewrites and the rest of the file it leaves
// as written, byte for byte, comments included (spec.board.triggers only, ADR 0026).
import { describe, expect, it } from 'vitest';
import type { Trigger } from '../../shared/definition/model';
import { keyLines, readBoard, readTrigger, readTriggers, triggerYaml, writeTrigger } from './trigger-yaml';

const file = `# Software: turns intake into reviewed pull requests.
apiVersion: goal/v1
kind: Factory
metadata:
  id: 0c3e8f42-7a19-4d6b-9e25-5b1f0a7c3d88
  name: software
spec:
  board:
    workTypes: [feature, fix]
    stations:
      - { id: intake, kind: queue }
      - { id: build, kind: agent }
    triggers:
      # Failing checks on main become fix items.
      - name: flaky-test-on-main
        on: github.check_run.failed
        filter: { branch: main, repo: byFrederick/goal }
        only: { failuresInARow: 2, dedupe: check.name }
        create: { type: fix, station: build, expedite: true, title: "Fix flaky {{check.name}} on main" }
        limits: { perDay: 3, pauseOnBudget: true }

      - name: lint
        on: github.check_run.failed
        create: { type: fix, station: build }
`;

const flaky = readTrigger(file, 'flaky-test-on-main') as Trigger;

describe('reading the triggers', () => {
  it('lists the triggers and finds one by name', () => {
    expect(readTriggers(file).map((t) => t.name)).toEqual(['flaky-test-on-main', 'lint']);
    expect(readTrigger(file, 'lint')?.create).toEqual({ type: 'fix', station: 'build' });
    expect(readTrigger(file, 'missing')).toBeUndefined();
  });

  it('reads the work types and station ids the Create section chooses from', () => {
    expect(readBoard(file)).toEqual({ workTypes: ['feature', 'fix'], stations: ['intake', 'build'] });
  });

  it('gives the line of each key, so a problem can be placed under its field', () => {
    const lines = keyLines(file, 'flaky-test-on-main');
    const start = file.split('\n').findIndex((l) => l.includes('name: flaky-test-on-main')) + 1;
    expect(lines.name?.from).toBe(start);
    expect(lines.limits?.from).toBe(start + 5);
    expect(lines.create?.from).toBe(start + 4);
  });
});

describe('writing a trigger', () => {
  it('changes only the key the form changed and leaves every other line and comment as written', () => {
    const next = writeTrigger(file, { ...flaky, limits: { perDay: 5, pauseOnBudget: true } });
    expect(next).toBe(
      file.replace('limits: { perDay: 3, pauseOnBudget: true }', 'limits: { perDay: 5, pauseOnBudget: true }'),
    );
  });

  it('keeps the file byte for byte when nothing changed', () => {
    expect(writeTrigger(file, flaky)).toBe(file);
  });

  it('removes a key the form cleared and keeps the other triggers untouched', () => {
    const next = writeTrigger(file, { ...flaky, only: undefined });
    expect(next).not.toContain('only:');
    expect(next).toContain('# Failing checks on main become fix items.');
    expect(
      next.endsWith(
        '      - name: lint\n        on: github.check_run.failed\n        create: { type: fix, station: build }\n',
      ),
    ).toBe(true);
  });

  it('writes enabled only when the trigger is off, since the schema defaults it to on', () => {
    expect(writeTrigger(file, { ...flaky, enabled: false })).toContain('        enabled: false\n');
    expect(writeTrigger(file, { ...flaky, enabled: true })).not.toContain('enabled:');
  });

  it('adds a key the trigger did not have after its last key, in the same column', () => {
    const lint = readTrigger(file, 'lint') as Trigger;
    const next = writeTrigger(file, { ...lint, limits: { perDay: 2 } });
    expect(next).toBe(
      file.replace(
        '        create: { type: fix, station: build }\n',
        '        create: { type: fix, station: build }\n        limits: { perDay: 2 }\n',
      ),
    );
  });

  it('replaces a flow map the form changed and keeps the keys around it', () => {
    const next = writeTrigger(file, {
      ...flaky,
      filter: { branch: 'release', repo: 'byFrederick/goal', skipBots: true },
    });
    expect(next).toBe(
      file.replace(
        'filter: { branch: main, repo: byFrederick/goal }',
        'filter: { branch: release, repo: byFrederick/goal, skipBots: true }',
      ),
    );
  });

  it('adds a new trigger at the end of the list', () => {
    const next = writeTrigger(file, {
      name: 'nightly',
      on: 'schedule',
      cron: '0 8 * * *',
      create: { type: 'fix', station: 'build' },
    });
    expect(readTriggers(next).map((t) => t.name)).toEqual(['flaky-test-on-main', 'lint', 'nightly']);
    expect(next.startsWith(file)).toBe(true);
  });

  it('refuses a file that does not parse', () => {
    expect(() => writeTrigger('spec: [', flaky)).toThrow();
  });
});

describe('showing a trigger as YAML', () => {
  it('writes the list entry with its maps in flow style, as the definition card shows it', () => {
    const yaml = triggerYaml(flaky);
    expect(yaml).toContain('- name: flaky-test-on-main\n  on: github.check_run.failed\n');
    expect(yaml).toContain('  filter: { branch: main, repo: byFrederick/goal }\n');
    expect(yaml).toContain('  limits: { perDay: 3, pauseOnBudget: true }');
  });
});
