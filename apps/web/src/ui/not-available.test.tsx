// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { NotAvailable } from './not-available';
import { TooltipProvider } from './tooltip';

afterEach(cleanup);

function renderControl(onClick = vi.fn()) {
  render(
    <TooltipProvider>
      <NotAvailable>
        <button type="button" onClick={onClick}>
          Export
        </button>
      </NotAvailable>
    </TooltipProvider>,
  );
  return { button: screen.getByRole('button', { name: 'Export' }), onClick };
}

describe('NotAvailable', () => {
  it('marks the control aria-disabled without the disabled attribute, so it keeps its name and stays focusable', () => {
    const { button } = renderControl();
    expect(button.getAttribute('aria-disabled')).toBe('true');
    expect((button as HTMLButtonElement).disabled).toBe(false);

    button.focus();
    expect(document.activeElement).toBe(button);
  });

  it('shows "Not available yet" when the control takes focus', async () => {
    const { button } = renderControl();
    button.focus();
    expect((await screen.findAllByText('Not available yet')).length).toBeGreaterThan(0);
  });

  it('does not run the control action on click', () => {
    const { button, onClick } = renderControl();
    fireEvent.click(button);
    expect(onClick).not.toHaveBeenCalled();
  });

  it('does not run the control action on Enter or Space', () => {
    const { button, onClick } = renderControl();
    // A prevented keydown is what stops a browser from turning Enter or Space into a click.
    expect(fireEvent.keyDown(button, { key: 'Enter' })).toBe(false);
    expect(fireEvent.keyDown(button, { key: ' ' })).toBe(false);
    fireEvent.click(button);
    expect(onClick).not.toHaveBeenCalled();
  });
});
