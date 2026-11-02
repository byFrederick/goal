// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { CopyButton } from './copy-button';

function mockClipboard(writeText: () => Promise<void>) {
  Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
}

describe('CopyButton', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it('shows the check with the icon-swap class, then the copy icon after 1500 ms', async () => {
    const writeText = vi.fn(() => Promise.resolve());
    mockClipboard(writeText);
    render(<CopyButton text="abc" />);
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Copy' }));
    });
    expect(writeText).toHaveBeenCalledWith('abc');
    expect(screen.getByTestId('copy-check').className).toContain('animate-icon-swap');
    expect(screen.getByTestId('copy-status').textContent).toBe('Copied');
    act(() => {
      vi.advanceTimersByTime(1500);
    });
    expect(screen.queryByTestId('copy-check')).toBeNull();
    expect(screen.getByTestId('copy-icon')).toBeTruthy();
    expect(screen.getByTestId('copy-status').textContent).toBe('');
  });

  it('calls onFailed and shows no check when the clipboard refuses', async () => {
    mockClipboard(() => Promise.reject(new Error('denied')));
    const onFailed = vi.fn();
    render(<CopyButton text="abc" onFailed={onFailed} />);
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Copy' }));
    });
    expect(onFailed).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId('copy-check')).toBeNull();
  });
});
