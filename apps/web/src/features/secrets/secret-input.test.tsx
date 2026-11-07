// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { SecretInput } from './secret-input';

afterEach(cleanup);

describe('SecretInput', () => {
  it('makes the Show button a 44 px box below md and draws a 32 px chip inside it', () => {
    render(<SecretInput aria-label="API key" />);
    const button = screen.getByRole('button', { name: 'Show the value' });
    expect(button.classList.contains('absolute')).toBe(true);
    expect(button.classList.contains('relative')).toBe(false);
    expect(button.classList.contains('max-md:min-h-11')).toBe(true);
    expect(button.classList.contains('max-md:min-w-11')).toBe(true);
    const chip = button.firstElementChild;
    expect(chip?.classList.contains('h-8')).toBe(true);
    expect(chip?.textContent).toBe('Show');
  });

  it('shows and hides the value', () => {
    render(<SecretInput aria-label="API key" />);
    const input = screen.getByLabelText('API key');
    expect(input.getAttribute('type')).toBe('password');
    fireEvent.click(screen.getByRole('button', { name: 'Show the value' }));
    expect(input.getAttribute('type')).toBe('text');
    expect(screen.getByRole('button', { name: 'Show the value' }).textContent).toBe('Hide');
  });
});
