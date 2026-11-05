// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { InfoTip } from './info-tip';
import { TooltipProvider } from './tooltip';

afterEach(cleanup);

describe('InfoTip', () => {
  it('opens its tip on a tap', async () => {
    render(
      <TooltipProvider>
        <InfoTip about="Coming up">Items that have not started.</InfoTip>
      </TooltipProvider>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'About Coming up' }));
    expect((await screen.findAllByText('Items that have not started.')).length).toBeGreaterThan(0);
    expect(document.querySelector('[data-state]:not(button)')).toBeTruthy();
  });
});
