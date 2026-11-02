// @vitest-environment jsdom
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { PageHeader } from './page-header';

describe('PageHeader', () => {
  it('renders the title in the page-title style at every width', () => {
    render(<PageHeader title="Members" />);
    const h1 = screen.getByRole('heading', { level: 1, name: 'Members' });
    expect(h1.classList.contains('text-page-title')).toBe(true);
    expect([...h1.classList].some((c) => c.startsWith('md:text-'))).toBe(false);
    expect(h1.classList.contains('font-normal')).toBe(false);
  });
});
