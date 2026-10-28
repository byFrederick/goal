// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import type { Validation } from '../../../api/types';
import type { Model } from '../../../shared/definition/model';
import { TooltipProvider } from '../../../ui/tooltip';
import { ChecksCard } from './checks-card';

afterEach(cleanup);

const model = {} as Model;

function renderChecks(validation: Validation | undefined) {
  render(
    <TooltipProvider>
      <ChecksCard validation={validation} model={model} />
    </TooltipProvider>,
  );
}

const passing = {
  checks: [{ id: 'schema', status: 'passed', summary: 'Every file matches the schema' }],
  problems: [],
  counts: {},
} as unknown as Validation;

describe('ChecksCard', () => {
  it('says the checks are being read while the draft is checked', () => {
    renderChecks(undefined);
    const busy = screen.getByText('Loading the checks').closest('[aria-busy="true"]');
    expect(busy).not.toBeNull();
  });

  it('lists each passed check', () => {
    renderChecks(passing);
    expect(screen.getByText('Every file matches the schema')).toBeTruthy();
  });

  it('shows Evals before publish as unavailable: aria-disabled, not run, with the tip', async () => {
    renderChecks(passing);
    const evals = screen.getByRole('button', { name: 'Evals before publish' });
    expect(evals.getAttribute('aria-disabled')).toBe('true');

    evals.focus();
    expect(await screen.findByText('Not available yet')).toBeTruthy();
  });

  it('does nothing when the unavailable Evals row is activated', () => {
    renderChecks(passing);
    const evals = screen.getByRole('button', { name: 'Evals before publish' });
    fireEvent.click(evals);
    expect(evals.getAttribute('aria-disabled')).toBe('true');
    expect(screen.queryByRole('dialog')).toBeNull();
  });
});
