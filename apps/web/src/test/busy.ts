import { fireEvent, waitFor, within } from '@testing-library/react';
import { expect } from 'vitest';

/**
 * Clicks a button whose request is held, then checks the busy contract (MOTION.md, A button waits on the
 * server): aria-busy and aria-disabled, never `disabled`, focus kept, the busy label after 100 ms, and a
 * second click that does nothing. Returns once the label shows; the caller counts the requests.
 */
export async function expectBusyOnClick(button: HTMLElement, busyLabel: string) {
  button.focus();
  fireEvent.click(button);
  await waitFor(() => expect(button.getAttribute('aria-busy')).toBe('true'));
  expect(button.getAttribute('aria-disabled')).toBe('true');
  expect((button as HTMLButtonElement).disabled).toBe(false);
  expect(document.activeElement).toBe(button);
  await waitFor(() =>
    expect(within(button).getByText(busyLabel).closest('[aria-hidden]')?.getAttribute('aria-hidden')).toBe('false'),
  );
  fireEvent.click(button);
}
