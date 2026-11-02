// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { WorkingLine } from './working-line';

afterEach(cleanup);

describe('WorkingLine', () => {
  it('shimmers while the agent runs and is still otherwise', () => {
    const { rerender } = render(<WorkingLine text="Writing the spec" running />);
    expect(screen.getByText('Writing the spec').classList).toContain('thinking');
    rerender(<WorkingLine text="Writing the spec" running={false} />);
    expect(screen.getByText('Writing the spec').classList).not.toContain('thinking');
  });

  it('draws the next state with the swap, and not the first one', () => {
    const { rerender } = render(<WorkingLine text="Reading" running />);
    const first = screen.getByText('Reading');
    expect(first.className).not.toMatch(/animate-text-states-swap/);
    rerender(<WorkingLine text="Writing" running />);
    const next = screen.getByText('Writing');
    expect(next).not.toBe(first);
    expect(next.className).toMatch(/motion-safe:animate-text-states-swap/);
    expect(next.className).toMatch(/motion-reduce:animate-fade-in/);
  });
});
