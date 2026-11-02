// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { AgentsSection } from './agents-section';

vi.mock('../draft/use-draft', () => ({
  useViewOnly: () => true,
  useSection: (name: string) =>
    name === 'agents'
      ? { agents: [{ name: 'architect-reviewer' }, { name: 'backend-engineer' }] }
      : { stations: [{ id: 'build', kind: 'agent', agent: 'architect-reviewer' }] },
}));
vi.mock('../frame/section-shell', () => ({
  bareSection: '',
  SectionShell: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));
vi.mock('../search-link', () => ({
  SearchLink: ({ patch: _patch, ...props }: { patch: unknown; className?: string; children?: ReactNode }) => (
    <a href="/x" {...props} />
  ),
}));
vi.mock('./agent-detail', () => ({ AgentDetail: () => <section>detail</section> }));
vi.mock('./agent-dialogs', () => ({ AddAgentButton: () => null, CopyAgentButton: () => null }));
vi.mock('./rules-editor', () => ({ RulesEditor: () => null }));

afterEach(cleanup);

describe('AgentsSection', () => {
  it('shows each agent with its full name in a link with no empty avatar track', () => {
    render(<AgentsSection place="definition" search={{}} />);

    const link = screen.getByRole('link', { name: /architect-reviewer/ });
    expect(link.className).not.toContain('grid-cols-[36px');
    expect(screen.getByRole('link', { name: /backend-engineer/ })).toBeTruthy();
  });
});
