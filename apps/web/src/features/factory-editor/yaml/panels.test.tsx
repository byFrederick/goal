// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { DefinitionProblem } from '../../../api/types';
import { TooltipProvider } from '../../../ui/tooltip';
import { Outline, ProblemBlock, ProblemsPanel } from './panels';

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const problem = (over: Partial<DefinitionProblem> = {}): DefinitionProblem =>
  ({
    code: 'enum',
    severity: 'error',
    message: 'size must be small, medium or large',
    line: 13,
    column: 1,
    end_line: 13,
    end_column: 0,
    file: 'factory.yaml',
    section: 'line',
    pointer: '/spec/runtime/size',
    fixes: [{ label: 'Use large', section: 'basics' }],
    ...over,
  }) as unknown as DefinitionProblem;

describe('ProblemsPanel', () => {
  it('counts the errors, names where each is, and says the checks run as you type', () => {
    render(
      <TooltipProvider>
        <ProblemsPanel problems={[problem()]} onGo={vi.fn()} headingId="p" />
      </TooltipProvider>,
    );
    expect(screen.getByRole('region', { name: 'Problems' }).textContent).toContain(
      'Checked as you type, the same checks as publish',
    );
    expect(screen.getByRole('button', { name: 'factory.yaml, line 13' })).toBeTruthy();
  });

  it('moves to the problem when its location is pressed', () => {
    const onGo = vi.fn();
    const p = problem();
    render(
      <TooltipProvider>
        <ProblemsPanel problems={[p]} onGo={onGo} headingId="p" />
      </TooltipProvider>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'factory.yaml, line 13' }));
    expect(onGo).toHaveBeenCalledWith(p);
  });

  it('leaves the quick fix to the line block, where it is disabled', () => {
    render(
      <TooltipProvider>
        <ProblemsPanel problems={[problem()]} onGo={vi.fn()} headingId="p" />
      </TooltipProvider>,
    );
    expect(screen.queryByRole('button', { name: 'Use large' })).toBeNull();
  });

  it('says so when there are no problems', () => {
    render(
      <TooltipProvider>
        <ProblemsPanel problems={[]} onGo={vi.fn()} headingId="p" />
      </TooltipProvider>,
    );
    expect(screen.getByText('No problems.')).toBeTruthy();
  });
});

describe('ProblemBlock', () => {
  it('shows the quick fix disabled, with aria-disabled, and sends nothing when pressed', () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch');
    render(
      <TooltipProvider>
        <ProblemBlock problems={[problem()]} />
      </TooltipProvider>,
    );
    const fix = screen.getByRole('button', { name: 'Use large' });
    expect(fix.getAttribute('aria-disabled')).toBe('true');
    fireEvent.click(fix);
    expect(fetchSpy).not.toHaveBeenCalled();
    fetchSpy.mockRestore();
  });
});

describe('Outline', () => {
  it("jumps to a key's line when it is pressed, and names its problems", () => {
    const onGo = vi.fn();
    render(
      <Outline
        items={[
          { key: 'runtime', line: 6, depth: 1, problems: 1 },
          { key: 'connections', line: 9, depth: 1, count: 3, problems: 0 },
        ]}
        onGo={onGo}
      />,
    );
    const outline = screen.getByRole('complementary', { name: 'Outline' });
    expect(outline.textContent).toContain('1 problem');
    expect(outline.textContent).toContain('3');
    fireEvent.click(screen.getByRole('button', { name: /^runtime/ }));
    expect(onGo).toHaveBeenCalledWith(6);
  });
});
