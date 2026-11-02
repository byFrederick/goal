// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

const toastError = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { error: toastError, success: vi.fn() } }));

import { FactoryId } from './basics-section';

function mockClipboard(writeText: () => Promise<void>) {
  Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
}

describe('FactoryId', () => {
  afterEach(() => {
    cleanup();
    toastError.mockClear();
  });

  it('copies the id and shows the check', async () => {
    const writeText = vi.fn(() => Promise.resolve());
    mockClipboard(writeText);
    render(<FactoryId id="0b1c" />);
    fireEvent.click(screen.getByRole('button', { name: 'Copy factory id' }));
    expect(await screen.findByTestId('copy-check')).toBeTruthy();
    expect(writeText).toHaveBeenCalledWith('0b1c');
  });

  it('says so and shows no check when the browser refuses', async () => {
    mockClipboard(() => Promise.reject(new Error('denied')));
    render(<FactoryId id="0b1c" />);
    fireEvent.click(screen.getByRole('button', { name: 'Copy factory id' }));
    await waitFor(() => expect(toastError).toHaveBeenCalledWith('The factory id could not be copied'));
    expect(screen.queryByTestId('copy-check')).toBeNull();
  });
});
