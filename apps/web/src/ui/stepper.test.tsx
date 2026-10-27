// @vitest-environment jsdom
import { cleanup, render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { Stepper } from './stepper';

afterEach(cleanup);

const steps = [
  { name: 'In progress', state: 'done' as const },
  { name: 'Build', detail: '2 h 12 min', state: 'current' as const },
  { name: 'Review', detail: 'Then merge', state: 'todo' as const },
];

describe('Stepper', () => {
  it('lists the steps in order under its label', () => {
    render(<Stepper label="Progress through the line" steps={steps} />);
    const list = screen.getByRole('list', { name: 'Progress through the line' });
    expect(
      within(list)
        .getAllByRole('listitem')
        .map((item) => item.textContent),
    ).toEqual(['In progress, Done', 'Build, Current step2 h 12 min', 'Review, Not startedThen merge']);
  });

  it('marks only the current step with aria-current', () => {
    render(<Stepper label="Progress" steps={steps} />);
    const items = screen.getAllByRole('listitem');
    expect(items[1]?.getAttribute('aria-current')).toBe('step');
    expect(items[0]?.getAttribute('aria-current')).toBeNull();
    expect(items[2]?.getAttribute('aria-current')).toBeNull();
  });

  it('says a done step is done in text, not only by its colour', () => {
    render(<Stepper label="Progress" steps={steps} />);
    expect(screen.getByText(', Done')).toBeTruthy();
  });

  it('says the current step waits on you when waiting is set', () => {
    render(<Stepper label="Progress" steps={steps} waiting />);
    expect(screen.getByText(', Current step, waiting on you')).toBeTruthy();
    expect(screen.queryByText(', Current step')).toBeNull();
  });

  it('colours the detail of a waiting current step as the violet ink', () => {
    render(<Stepper label="Progress" steps={steps} waiting />);
    expect(screen.getByText('2 h 12 min').className).toContain('text-you-ink');
  });

  it('says a step to come is not started', () => {
    render(<Stepper label="Progress" steps={steps} />);
    expect(screen.getByText(', Not started')).toBeTruthy();
  });
});
