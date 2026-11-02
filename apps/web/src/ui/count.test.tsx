// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { Count } from './count';

afterEach(cleanup);

describe('Count', () => {
  it('draws an orange pill 18 px high with a white number', () => {
    render(<Count value={3} label=" waiting on you" />);
    const badge = screen.getByText('waiting on you', { exact: false }).parentElement as HTMLElement;
    expect(Array.from(badge.classList)).toEqual(
      expect.arrayContaining(['h-[18px]', 'min-w-[18px]', 'rounded-full', 'bg-you', 'text-white', 'px-[5px]']),
    );
    expect(badge.classList).not.toContain('rounded-chip');
  });

  it('keeps the badge entrance and its reduced-motion fade', () => {
    render(<Count value={3} label=" waiting on you" />);
    const badge = screen.getByText('waiting on you', { exact: false }).parentElement as HTMLElement;
    expect(Array.from(badge.classList)).toEqual(
      expect.arrayContaining(['motion-safe:animate-notification-badge', 'motion-reduce:animate-fade-in']),
    );
  });

  it('says what the number counts to screen readers', () => {
    render(<Count value={3} label=" waiting on you" />);
    expect(screen.getByText('waiting on you', { exact: false }).className).toContain('sr-only');
  });
});
