// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { touchTarget } from '../../ui/touch-target';
import { SecretInput } from './secret-input';

afterEach(cleanup);

describe('SecretInput', () => {
  it('keeps Show the value absolute and 32 px drawn, with the touchTarget hit area and no relative', () => {
    render(<SecretInput aria-label="API key" />);
    const button = screen.getByRole('button', { name: 'Show the value' });
    expect(button.classList.contains('absolute')).toBe(true);
    expect(button.classList.contains('h-8')).toBe(true);
    expect(button.classList.contains('relative')).toBe(false);
    for (const cls of touchTarget.split(' ').filter((c) => c.startsWith('max-md:before:'))) {
      expect(button.classList.contains(cls)).toBe(true);
    }
  });
});
