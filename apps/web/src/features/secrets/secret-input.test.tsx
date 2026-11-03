// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { SecretInput } from './secret-input';

afterEach(cleanup);

describe('SecretInput', () => {
  it('keeps Show the value absolute and 32 px drawn, with a 44 px hit area below md and no relative', () => {
    render(<SecretInput aria-label="API key" />);
    const button = screen.getByRole('button', { name: 'Show the value' });
    expect(button.classList.contains('absolute')).toBe(true);
    expect(button.classList.contains('h-8')).toBe(true);
    expect(button.classList.contains('relative')).toBe(false);
    expect(button.classList.contains('max-md:before:h-11')).toBe(true);
    expect(button.classList.contains('max-md:before:top-1/2')).toBe(true);
  });
});
