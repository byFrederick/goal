// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { FileTree } from './file-tree';
import { fileTree } from './files';

afterEach(cleanup);

const info = (path: string) => ({ path, sha256: path, size: 10 });
const nodes = fileTree(
  ['factory.yaml', 'agents/spec-writer.md', 'agents/reviewer.md', 'contracts/spec.md', 'evals/smoke.yaml'].map(info),
);

function renderTree(overrides: Partial<Parameters<typeof FileTree>[0]> = {}) {
  const props = {
    nodes,
    path: 'factory.yaml',
    changed: new Set(['agents/reviewer.md']),
    problems: new Map([['factory.yaml', 1]]),
    factoryName: 'Software',
    onOpen: vi.fn(),
    onNew: vi.fn(),
    onRename: vi.fn(),
    onDelete: vi.fn(),
    ...overrides,
  };
  render(<FileTree {...props} />);
  return props;
}

describe('FileTree', () => {
  it('filters the files by "Find a file" and lists the matches by their full path', () => {
    renderTree();
    fireEvent.change(screen.getByRole('searchbox', { name: 'Find a file' }), { target: { value: 'SPEC' } });
    const matches = screen.getByRole('list', { name: 'Matching files' });
    expect(within(matches).getByRole('button', { name: /agents\/spec-writer\.md/ })).toBeTruthy();
    expect(within(matches).getByRole('button', { name: /contracts\/spec\.md/ })).toBeTruthy();
    expect(screen.queryByRole('button', { name: /reviewer/ })).toBeNull();
  });

  it('says when no file matches', () => {
    renderTree();
    fireEvent.change(screen.getByRole('searchbox', { name: 'Find a file' }), { target: { value: 'nothing' } });
    expect(screen.getByText(/No file matches/)).toBeTruthy();
  });

  it('opens and closes a folder, with its file count', () => {
    renderTree();
    const agents = screen.getByRole('button', { name: /^agents, 2 files/ });
    expect(agents.getAttribute('aria-expanded')).toBe('true');
    fireEvent.click(agents);
    expect(agents.getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByRole('button', { name: /reviewer\.md/ })).toBeNull();
  });

  it('marks the changed file and folder, and the file with errors, in words too', () => {
    renderTree();
    expect(screen.getByRole('button', { name: /reviewer\.md, changed/ })).toBeTruthy();
    expect(screen.getByRole('button', { name: /^agents, 2 files, changed/ })).toBeTruthy();
    expect(screen.getByRole('button', { name: /factory\.yaml, 1 error/ }).getAttribute('aria-current')).toBe('true');
  });

  it('starts a file in a folder from the folder row, and offers no add in evals/', () => {
    const { onNew } = renderTree();
    fireEvent.click(screen.getByRole('button', { name: 'New file in agents' }));
    expect(onNew).toHaveBeenCalledWith('agents');
    expect(screen.queryByRole('button', { name: 'New file in evals' })).toBeNull();
  });

  it('hides the file operations of a view-only editor', () => {
    renderTree({ readOnly: true });
    expect(screen.queryByRole('button', { name: 'New file in agents' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'New file in the definition' })).toBeNull();
  });
});
