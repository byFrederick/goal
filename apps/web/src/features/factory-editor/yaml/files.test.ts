import { describe, expect, it } from 'vitest';
import { linesText } from './blocks';
import { changedLines, fileNameProblem, fileTree, folderOf, isReadOnlyPath, newFileText, type TreeNode } from './files';

const info = (path: string) => ({ path, sha256: path, size: 10 });

describe('fileTree', () => {
  it('lists factory.yaml first, then folders by name with their file counts, then the other files', () => {
    const tree = fileTree(
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
    const names = (nodes: TreeNode[]): unknown[] =>
      nodes.map((n) => (n.kind === 'file' ? n.path : [n.path, n.count, names(n.children)]));

    expect(names(tree)).toEqual([
      'factory.yaml',
      ['agents', 2, ['agents/a.md', 'agents/b.md']],
      ['contracts', 1, ['contracts/spec.json']],
      ['evals', 1, ['evals/smoke.yaml']],
      [
        'skills',
        3,
        [
          ['skills/acme', 2, ['skills/acme/SKILL.md', 'skills/acme/notes.md']],
          ['skills/web', 1, ['skills/web/SKILL.md']],
        ],
      ],
      'README.md',
    ]);
  });

  it('keeps agents/ and contracts/ when empty, so New file… has a place', () => {
    expect(fileTree([info('factory.yaml')]).map((n) => n.name)).toEqual(['factory.yaml', 'agents', 'contracts']);
  });

  it('knows which folders are read only and which folder a file is in', () => {
    expect(isReadOnlyPath('evals')).toBe(true);
    expect(isReadOnlyPath('evals/smoke.yaml')).toBe(true);
    expect(isReadOnlyPath('evalsx/smoke.yaml')).toBe(false);
    expect(folderOf('agents/a.md')).toBe('agents');
    expect(folderOf('factory.yaml')).toBe('');
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
