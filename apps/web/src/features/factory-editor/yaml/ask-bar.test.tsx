// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { TooltipProvider } from '../../../ui/tooltip';
import { AskBar } from './ask-bar';

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

function renderBar() {
  render(
    <TooltipProvider>
      <AskBar />
    </TooltipProvider>,
  );
}

describe('AskBar', () => {
  it('renders the input and Ask disabled with aria-disabled, not removed from the tab order', () => {
    renderBar();
    const input = screen.getByRole('textbox', { name: 'Ask the factory to change it' });
    const ask = screen.getByRole('button', { name: 'Ask' });
    expect(input.getAttribute('aria-disabled')).toBe('true');
    expect(ask.getAttribute('aria-disabled')).toBe('true');
    expect((ask as HTMLButtonElement).disabled).toBe(false);
    input.focus();
    expect(document.activeElement).toBe(input);
  });

  it('shows "Not available yet" on focus', async () => {
    renderBar();
    screen.getByRole('button', { name: 'Ask' }).focus();
    expect((await screen.findAllByText('Not available yet')).length).toBeGreaterThan(0);
  });

  it('sends no request when Ask is pressed or Enter is pressed in the input', () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch');
    renderBar();
    fireEvent.click(screen.getByRole('button', { name: 'Ask' }));
    fireEvent.keyDown(screen.getByRole('textbox', { name: 'Ask the factory to change it' }), { key: 'Enter' });
    expect(fetchSpy).not.toHaveBeenCalled();
  });
});
