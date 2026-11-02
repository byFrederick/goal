// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { PageHeader } from './page-header';

afterEach(cleanup);

describe('PageHeader', () => {
  it('renders the title in the page-title style at every width, and lets it shrink and wrap', () => {
    render(<PageHeader title="Members" />);
    const h1 = screen.getByRole('heading', { level: 1, name: 'Members' });
    expect(h1.classList.contains('text-page-title')).toBe(true);
    expect([...h1.classList].some((c) => c.startsWith('md:text-'))).toBe(false);
    expect(h1.classList.contains('font-normal')).toBe(false);
    expect(h1.className).toContain('text-balance');
    expect(h1.className).toContain('break-words');
    expect(h1.parentElement?.className).toContain('min-w-0');
    expect(h1.parentElement?.className).toContain('flex-1');
  });
});
