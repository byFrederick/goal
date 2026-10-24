import { describe, expect, it } from 'vitest';
import { linesText } from './blocks';
import { changedLines, fileGroups, fileNameProblem, groupOf, newFileText } from './files';

const info = (path: string) => ({ path, sha256: path, size: 10 });

describe('fileGroups', () => {
  it('groups the tree as the tabs show it: factory.yaml, agents, each skill folder, contracts, evals, the rest', () => {
    const groups = fileGroups(
      [
        'skills/acme/SKILL.md',
        'agents/b.md',
        'factory.yaml',
        'evals/smoke.yaml',
        'agents/a.md',
        'contracts/spec.json',
        'skills/acme/notes.md',
        'skills/web/SKILL.md',
        'README.md',
      ].map(info),
    );

    expect(groups.map((g) => [g.label, g.files.map((f) => f.path)])).toEqual([
      ['factory.yaml', ['factory.yaml']],
      ['agents/ (2)', ['agents/a.md', 'agents/b.md']],
      ['skills/acme', ['skills/acme/SKILL.md', 'skills/acme/notes.md']],
      ['skills/web', ['skills/web/SKILL.md']],
      ['contracts/ (1)', ['contracts/spec.json']],
      ['evals/ (1)', ['evals/smoke.yaml']],
      ['other files (1)', ['README.md']],
    ]);
    expect(groups.find((g) => g.id === 'evals')?.readOnly).toBe(true);
  });

  it('keeps agents/ and contracts/ when empty, so New file… has a place', () => {
    expect(fileGroups([info('factory.yaml')]).map((g) => g.label)).toEqual([
      'factory.yaml',
      'agents/ (0)',
      'contracts/ (0)',
    ]);
  });

  it('finds the group of a file the list does not have yet by its folder', () => {
    const groups = fileGroups([info('factory.yaml'), info('agents/a.md')]);
    expect(groupOf(groups, 'agents/new.md')?.id).toBe('agents');
    expect(groupOf(groups, 'factory.yaml')?.id).toBe('factory.yaml');
  });
});

describe('changedLines', () => {
  it('marks the lines added or changed since the base, not the ones around them', () => {
    const base = 'a: 1\nb: 2\nc: 3\n';
    const text = 'a: 1\nb: 20\nnew: x\nc: 3\n';
    expect(changedLines(base, text)).toEqual([2, 3]);
  });

  it('marks nothing for a removal and every line of a new file', () => {
    expect(changedLines('a: 1\nb: 2\n', 'a: 1\n')).toEqual([]);
    expect(changedLines(null, '# x\n\ntext\n')).toEqual([1, 2, 3]);
  });
});

describe('names and text', () => {
  it('refuses paths, hidden files and odd characters', () => {
    expect(fileNameProblem('notes.md')).toBeUndefined();
    expect(fileNameProblem('a/b.md')).toMatch(/not a path/);
    expect(fileNameProblem('.env')).toMatch(/Hidden/);
    expect(fileNameProblem('my file.md')).toMatch(/Letters/);
    expect(fileNameProblem('')).toBe('Name the file.');
  });

  it('starts a Markdown file with its title', () => {
    expect(newFileText('agents/triager.md')).toBe('# triager\n');
    expect(newFileText('contracts/spec.json')).toBe('');
  });

  it('says where a block is', () => {
    expect(linesText([31, 45])).toBe('lines 31–45');
    expect(linesText([30, 30])).toBe('line 30');
    expect(linesText([0, 0])).toBe('not in the file yet');
  });
});
