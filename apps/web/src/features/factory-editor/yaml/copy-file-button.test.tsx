// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { CopyFileButton } from './yaml-view';

describe('CopyFileButton', () => {
  afterEach(cleanup);

  it('copies the file text and shows the check', async () => {
    const writeText = vi.fn(() => Promise.resolve());
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
    render(<CopyFileButton text="name: acme" />);
    fireEvent.click(screen.getByRole('button', { name: 'Copy' }));
    expect(await screen.findByTestId('copy-check')).toBeTruthy();
    expect(writeText).toHaveBeenCalledWith('name: acme');
  });
});
