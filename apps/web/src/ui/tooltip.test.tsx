// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { OPEN_DELAY_MS, Tooltip, TooltipProvider } from './tooltip';

afterEach(cleanup);

function renderTip() {
  return render(
    <TooltipProvider>
      <Tooltip label="Copy link" side="top">
        <button type="button">Copy</button>
      </Tooltip>
    </TooltipProvider>,
  );
}

describe('Tooltip', () => {
  it('shows its label when the trigger takes keyboard focus', async () => {
    renderTip();
    act(() => screen.getByRole('button', { name: 'Copy' }).focus());
    expect(await screen.findByText('Copy link')).toBeTruthy();
  });

  it('hides on Escape', async () => {
    renderTip();
    act(() => screen.getByRole('button', { name: 'Copy' }).focus());
    await screen.findByText('Copy link');
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByText('Copy link')).toBeNull());
  });

  it('opens with the fade and scale, from its trigger, and does not animate its close', async () => {
    renderTip();
    act(() => screen.getByRole('button', { name: 'Copy' }).focus());
    const tip = (await screen.findByText('Copy link')).closest('[data-state]');
    expect(Array.from(tip?.classList ?? [])).toEqual(
      expect.arrayContaining([
        'data-[state=delayed-open]:motion-safe:animate-tooltip-in',
        'data-[state=delayed-open]:motion-reduce:animate-fade-in',
        'origin-(--radix-tooltip-content-transform-origin)',
      ]),
    );
  });

  it('waits the micro duration before it opens', () => {
    expect(OPEN_DELAY_MS).toBe(80);
  });
});
