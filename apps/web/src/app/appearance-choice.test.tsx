// @vitest-environment jsdom
import { render, screen } from '@testing-library/react';
import { beforeAll, describe, expect, it } from 'vitest';
import { AppearanceChoice } from './appearance-choice';

const halves = (name: string) =>
  Array.from(screen.getByRole('button', { name }).querySelectorAll(':scope > span > span'));

describe('AppearanceChoice cards', () => {
  beforeAll(() => {
    window.matchMedia = ((query: string) => ({
      matches: false,
      media: query,
      addEventListener: () => {},
      removeEventListener: () => {},
    })) as unknown as typeof window.matchMedia;
  });

  it('draws each swatch from the theme-stable preview tokens', () => {
    render(<AppearanceChoice layout="cards" />);
    const classes = (name: string) => halves(name).map((h) => h.className);
    expect(classes('Match device').join(' ')).toMatch(/bg-preview-light.*bg-preview-dark/);
    expect(classes('Light').join(' ')).toMatch(/bg-preview-light-side.*bg-preview-light(?!-)/);
    expect(classes('Dark').join(' ')).toMatch(/bg-preview-dark-side.*bg-preview-dark(?!-)/);
  });
});
