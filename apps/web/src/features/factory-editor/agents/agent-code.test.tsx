// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { BasicsSection } from '../../../shared/definition/model';

const edit = vi.hoisted(() => vi.fn());
const basics = vi.hoisted(() => ({ current: {} as BasicsSection }));
vi.mock('../draft/use-draft', () => ({
  useDraftSession: () => ({ edit }),
  useViewOnly: () => false,
  useSection: (name: string) =>
    name === 'basics' ? basics.current : { agents: [{ name: 'engineer' }, { name: 'reviewer', code: 'read' }] },
}));
vi.mock('../frame/section-shell', () => ({ EditedMark: () => null }));

import { TooltipProvider } from '../../../ui/tooltip';
import { CodeField } from './agent-detail';

afterEach(() => {
  cleanup();
  edit.mockClear();
});

describe('an agent’s code', () => {
  it('shows write when a repository writes and read otherwise', () => {
    basics.current = { repositories: [{ name: 'a', url: 'https://github.com/acme/a', access: 'write' }] };
    render(
      <TooltipProvider>
        <CodeField agent={{ name: 'engineer' }} at="/agents/agents/0" />
      </TooltipProvider>,
    );
    expect((screen.getByRole('radio', { name: 'Write' }) as HTMLInputElement).checked).toBe(true);
    cleanup();
    basics.current = { repositories: [{ name: 'a', url: 'https://github.com/acme/a', access: 'read' }] };
    render(
      <TooltipProvider>
        <CodeField agent={{ name: 'engineer' }} at="/agents/agents/0" />
      </TooltipProvider>,
    );
    expect((screen.getByRole('radio', { name: 'Read' }) as HTMLInputElement).checked).toBe(true);
  });

  it('writes code on that agent when chosen', () => {
    basics.current = { repository: { url: 'https://github.com/acme/a' } };
    render(
      <TooltipProvider>
        <CodeField agent={{ name: 'engineer' }} at="/agents/agents/0" />
      </TooltipProvider>,
    );
    fireEvent.click(screen.getByRole('radio', { name: 'Read' }));
    expect(edit.mock.lastCall?.[0].value.agents[0]).toEqual({ name: 'engineer', code: 'read' });
  });
});
