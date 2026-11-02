// @vitest-environment jsdom
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { SectionCard } from './card';

describe('SectionCard', () => {
  it('draws its heading with the shared section heading class', () => {
    render(<SectionCard title="Members">body</SectionCard>);
    const heading = screen.getByRole('heading', { name: 'Members' });
    expect(heading.classList.contains('text-heading')).toBe(true);
    expect(heading.classList.contains('text-lg')).toBe(false);
    expect(heading.classList.contains('font-medium')).toBe(false);
  });
});
