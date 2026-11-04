// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import type { Validation } from '../../../api/types';
import type { Model } from '../../../shared/definition/model';
import { TooltipProvider } from '../../../ui/tooltip';
import { ChecksCard } from './checks-card';

afterEach(cleanup);

const model = {} as Model;

function renderChecks(validation: Validation | undefined, failed = false) {
  return render(
    <TooltipProvider>
      <ChecksCard validation={validation} model={model} failed={failed} />
    </TooltipProvider>,
  );
}

const passing = {
  checks: [{ id: 'schema', status: 'passed', summary: 'Every file matches the schema' }],
  problems: [],
  counts: {},
} as unknown as Validation;

describe('ChecksCard', () => {
  it('shows a skeleton while the draft is checked', () => {
    renderChecks(undefined);
    const busy = screen.getByText('Loading').closest('[aria-busy="true"]');
    expect(busy).not.toBeNull();
  });

  it('without a draft: a skeleton while the running version is checked, then its checks', () => {
    const { rerender } = renderChecks(undefined);
    expect(screen.getByText('Loading').closest('[aria-busy="true"]')).not.toBeNull();
    rerender(
      <TooltipProvider>
        <ChecksCard validation={passing} model={model} />
      </TooltipProvider>,
    );
    expect(screen.getByText('Every file matches the schema')).toBeTruthy();
  });

  it('says so when the checks cannot be read', () => {
    renderChecks(undefined, true);
    expect(screen.getByText(/could not be read/)).toBeTruthy();
    expect(screen.queryByText('Loading')).toBeNull();
  });

  it('lists each passed check', () => {
    renderChecks(passing);
    expect(screen.getByText('Every file matches the schema')).toBeTruthy();
  });
});
