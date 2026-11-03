// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { useState } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { KnowledgeSource } from '../../../api/types';
import { MemoryCard, PreviewCard, SourcesCard } from './knowledge-cards';
import { type MemoryEntry, readMemory, writeMemory } from './memory-file';

vi.mock('../../../shared/org', () => ({ useOrg: () => ({ id: 'o1', slug: 'acme', name: 'Acme' }) }));
vi.mock('@tanstack/react-router', () => ({
  Link: ({ children, to: _to, params: _p, ...props }: { children?: ReactNode; to?: string; params?: unknown }) => (
    <a href="/x" {...props}>
      {children}
    </a>
  ),
}));

afterEach(cleanup);

const source = (name: string, read_when: string, files: number, bytes: number) =>
  ({
    name,
    read_when,
    kind: 'repository',
    latest_snapshot: { files, failed: 0, bytes, synced_at: '2026-11-03T09:00:00Z' },
  }) as unknown as KnowledgeSource;

const known = [
  source('handbook', 'Before you change a process', 12, 48_000),
  source('runbooks', 'When a deploy fails', 3, 9_000),
];

/** The card over a file's text, which each change rewrites, as the section does through the draft session. */
function MemoryHarness({ initial = '', readOnly = false }: { initial?: string; readOnly?: boolean }) {
  const [text, setText] = useState(initial);
  const entries = readMemory(text);
  return (
    <>
      <MemoryCard
        entries={entries}
        readOnly={readOnly}
        bytes={text.length}
        onChange={(next: MemoryEntry[]) => setText(writeMemory(next))}
      />
      <pre data-testid="file">{text}</pre>
    </>
  );
}

describe('MemoryCard', () => {
  it('adds a fact with today’s date and clears the field', () => {
    render(<MemoryHarness />);
    expect(screen.getByText('No facts yet.')).toBeTruthy();
    fireEvent.change(screen.getByLabelText('Add a fact'), { target: { value: 'Use pnpm, not npm.' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add fact' }));
    expect(screen.getByTestId('file').textContent).toMatch(
      /- text: Use pnpm, not npm\.\n {2}added: "?\d{4}-\d{2}-\d{2}"?/,
    );
    expect((screen.getByLabelText('Add a fact') as HTMLTextAreaElement).value).toBe('');
    expect(screen.getByLabelText('Fact 1')).toBeTruthy();
  });

  it('edits a fact when it is left and removes another', () => {
    render(<MemoryHarness initial={'- text: One.\n  added: "2026-10-01"\n- text: Two.\n  added: "2026-10-02"\n'} />);
    const first = screen.getByLabelText('Fact 1');
    fireEvent.change(first, { target: { value: 'One, changed.' } });
    fireEvent.blur(first);
    expect(screen.getByTestId('file').textContent).toContain('text: One, changed.');
    fireEvent.click(screen.getByRole('button', { name: 'Remove fact 2' }));
    expect(screen.getByTestId('file').textContent).not.toContain('Two.');
  });

  it('keeps an over-long fact out of the file and says why', () => {
    render(<MemoryHarness />);
    fireEvent.change(screen.getByLabelText('Add a fact'), { target: { value: 'x'.repeat(301) } });
    expect(screen.getByText('At most 300 characters (now 301).')).toBeTruthy();
    expect((screen.getByRole('button', { name: 'Add fact' }) as HTMLButtonElement).disabled).toBe(true);
  });

  it('only shows the facts when the caller may not edit', () => {
    render(<MemoryHarness initial={'- text: One.\n  added: "2026-10-01"\n'} readOnly />);
    expect(screen.queryByRole('button', { name: 'Add fact' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Remove fact 1' })).toBeNull();
  });
});

describe('SourcesCard', () => {
  it('shows who reads each source, its line and size, and adds and removes', () => {
    const onAdd = vi.fn();
    const onRemove = vi.fn();
    render(
      <SourcesCard
        entries={[
          {
            name: 'handbook',
            appliesTo: { agents: ['build'], stations: ['review'] },
            readWhen: 'Before you change CI',
          },
        ]}
        known={known}
        readOnly={false}
        onAdd={onAdd}
        onRemove={onRemove}
      />,
    );
    const row = screen.getByRole('listitem');
    expect(within(row).getByText('agents: build · stations: review')).toBeTruthy();
    expect(within(row).getByText('Before you change CI')).toBeTruthy();
    expect(within(row).getByText(/12 files/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Remove handbook' }));
    expect(onRemove).toHaveBeenCalledWith('handbook');
    expect((screen.getByRole('button', { name: 'Add source' }) as HTMLButtonElement).disabled).toBe(true);
  });

  it('says every agent when the entry names no scope, and names a source the organization lacks', () => {
    render(<SourcesCard entries={[{ name: 'gone' }]} known={known} readOnly onAdd={vi.fn()} onRemove={vi.fn()} />);
    expect(screen.getByText('Every agent')).toBeTruthy();
    expect(screen.getByText('Not in this organization’s sources.')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Remove gone' })).toBeNull();
  });
});

describe('PreviewCard', () => {
  it('shows the section as one agent gets it, or says there is none', () => {
    const { rerender } = render(
      <PreviewCard
        agents={['build']}
        agent="build"
        onAgent={vi.fn()}
        section="## Knowledge\n- handbook"
        loading={false}
        failed={false}
      />,
    );
    expect(screen.getByText(/## Knowledge/)).toBeTruthy();
    rerender(
      <PreviewCard agents={['build']} agent="build" onAgent={vi.fn()} section="" loading={false} failed={false} />,
    );
    expect(screen.getByText('This agent gets no Knowledge section.')).toBeTruthy();
    rerender(
      <PreviewCard agents={['build']} agent="build" onAgent={vi.fn()} section={undefined} loading={false} failed />,
    );
    expect(screen.getByText(/needs a draft that saves/)).toBeTruthy();
  });
});
