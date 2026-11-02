// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { SuccessLabel } from './success-label';

afterEach(cleanup);

describe('SuccessLabel', () => {
  it('shows the idle label until the action succeeded', () => {
    const { container } = render(
      <SuccessLabel done={false} doneLabel="Approved">
        Approve
      </SuccessLabel>,
    );
    expect(screen.getByText('Approve')).toBeTruthy();
    expect(container.querySelector('svg')).toBeNull();
  });

  it('swaps the label for a check and the past-tense label, both motion-safe', () => {
    const { container } = render(
      <SuccessLabel done doneLabel="Approved">
        Approve
      </SuccessLabel>,
    );
    expect(screen.queryByText('Approve')).toBeNull();
    expect(container.querySelector('svg')?.getAttribute('class')).toContain('motion-safe:animate-success-check');
    expect(screen.getByRole('status').className).toContain('motion-safe:animate-text-states-swap');
    expect(screen.getByRole('status').textContent).toBe('Approved');
  });
});
