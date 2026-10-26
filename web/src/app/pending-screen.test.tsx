// @vitest-environment jsdom
import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PendingScreen } from './states';

describe('the first-load state', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it('shows only the background while loading', () => {
    const { container } = render(<PendingScreen />);

    expect(screen.getByText('Loading')).toBeTruthy();
    expect(container.querySelector('[aria-busy="true"]')).not.toBeNull();
    expect(container.querySelector('.bg-card, .bg-control, .border')).toBeNull();
    expect(container.querySelector('[aria-hidden="true"]')).toBeNull();
  });

  it('adds a thin line fixed to the top once the load has taken a second', () => {
    const { container } = render(<PendingScreen />);

    act(() => vi.advanceTimersByTime(900));
    expect(container.querySelector('[aria-hidden="true"]')).toBeNull();

    act(() => vi.advanceTimersByTime(200));
    const line = container.querySelector('[aria-hidden="true"]');
    expect(line).not.toBeNull();
    expect(line?.className).toContain('fixed');
    expect(line?.className).toContain('top-0');
    expect(line?.className).toContain('h-0.5');
  });
});
