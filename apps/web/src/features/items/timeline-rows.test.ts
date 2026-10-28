import { describe, expect, it } from 'vitest';
import { formatDuration } from '../../shared/format';
import type { ToolCall } from './timeline';
import { checkStep, commonDirectory, type StepRow, toolSteps } from './timeline-rows';

const call = (over: Partial<ToolCall> & Pick<ToolCall, 'tool' | 'argument'>): ToolCall => ({
  id: `t-${over.argument}-${over.tool}`,
  isError: false,
  at: '2026-10-25T08:00:00Z',
  ...over,
});

/** The one step a list holds; a test that gets none fails here, with its name. */
function one(steps: StepRow[]): StepRow {
  const step = steps[0];
  if (!step) throw new Error('expected a step');
  return step;
}

/** A step's sentence as the words a person reads, without the code styling. */
const sentence = (step: { segments: { text: string }[] }) => step.segments.map((s) => s.text).join('');

describe('toolSteps', () => {
  it('groups consecutive reads under their common directory', () => {
    const steps = toolSteps([
      call({ tool: 'Read', argument: 'apps/web/src/features/a.ts' }),
      call({ tool: 'Read', argument: 'apps/web/src/shared/b.ts' }),
      call({ tool: 'Read', argument: 'apps/web/src/ui/c.tsx' }),
    ]);

    expect(steps).toHaveLength(1);
    expect(sentence(one(steps))).toBe('Read 3 files in apps/web/src');
  });

  it('says the path of a single read, and falls back to a count when reads share no directory', () => {
    expect(sentence(one(toolSteps([call({ tool: 'Read', argument: 'apps/web/src/a.ts' })])))).toBe(
      'Read apps/web/src/a.ts',
    );

    const apart = toolSteps([call({ tool: 'Read', argument: 'a.ts' }), call({ tool: 'Read', argument: 'docs/b.md' })]);
    expect(sentence(one(apart))).toBe('Read 2 files');
  });

  it('counts consecutive edits as one step, and leaves other calls between them as their own rows', () => {
    const steps = toolSteps([
      call({ tool: 'Edit', argument: 'a.go' }),
      call({ tool: 'Write', argument: 'b.go' }),
      call({ tool: 'Bash', argument: 'go test ./...' }),
      call({ tool: 'Edit', argument: 'c.go' }),
    ]);

    expect(steps.map(sentence)).toEqual(['Edited 2 files', 'Ran go…', 'Edited c.go']);
  });

  it('shortens a long Bash command to one line', () => {
    const command = `go test ./internal/engine/ ${'-run TestClaimExpediteBeforePriority '.repeat(20)}`;
    const step = one(toolSteps([call({ tool: 'Bash', argument: command.slice(0, 139) })]));

    expect(sentence(step)).toBe('Ran go…');
    expect(step.segments.find((s) => s.code)?.text).toBe('go…');
  });

  it('never uses call.summary as the right label', () => {
    const output = 'a very long tool output that must stay out of the right column';
    const ok = one(toolSteps([call({ tool: 'Read', argument: 'a.ts', summary: output })]));
    const failed = one(toolSteps([call({ tool: 'Bash', argument: 'task check', summary: output, isError: true })]));

    expect(ok.right).toBeUndefined();
    expect(failed.right).toBe('failed');
    expect(failed.right).not.toBe(output);
    expect(failed.output).toEqual([output]);
  });

  it('counts the failed calls of a group in its right label', () => {
    const step = one(
      toolSteps([
        call({ tool: 'Edit', argument: 'a.go' }),
        call({ tool: 'Edit', argument: 'b.go', isError: true }),
        call({ tool: 'Edit', argument: 'c.go' }),
      ]),
    );

    expect(step.failed).toBe(true);
    expect(step.right).toBe('1 of 3 failed');
  });
});

describe('checkStep', () => {
  it('says a check passed with how long it took, and a failed one with its exit code', () => {
    const passed = checkStep({
      kind: 'check',
      key: 'c1',
      at: '2026-10-25T08:00:00Z',
      station: 'build',
      command: 'task check:changed',
      passed: true,
      durationMs: 190_000,
    });
    const failed = checkStep({
      kind: 'check',
      key: 'c2',
      at: '2026-10-25T08:00:00Z',
      station: 'build',
      command: 'task check:changed',
      passed: false,
      exitCode: 1,
    });

    expect(passed.segments.map((s) => s.text).join('')).toBe('task check:changed passed');
    expect(passed.right).toBe(formatDuration(190_000));
    expect(failed.segments.map((s) => s.text).join('')).toBe('task check:changed failed, exit 1');
  });
});

describe('commonDirectory', () => {
  it('is the directory the paths share, and empty when they share none', () => {
    expect(commonDirectory(['apps/web/src/a/x.ts', 'apps/web/src/b/y.ts'])).toBe('apps/web/src');
    expect(commonDirectory(['a.ts', 'b.ts'])).toBe('');
  });
});
