// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { describeState } from '../../shared/item-state';
import { ItemStateChip } from './item-state-chip';

afterEach(cleanup);

/** The chip's status icon, if it has one: a StatusIcon is a shrink-0 svg. */
function iconClass(container: HTMLElement): string | null {
  return container.querySelector('svg.shrink-0')?.getAttribute('class') ?? null;
}

describe('ItemStateChip', () => {
  it('shows the working icon before the words of an item in progress', () => {
    const { container } = render(
      <ItemStateChip
        state="in_progress"
        label={describeState({ state: 'in_progress', agent: null, rework_count: 0 })}
      />,
    );

    expect(screen.getByText('In progress')).toBeTruthy();
    expect(iconClass(container)).toContain('text-run');
  });

  it('shows the clock for an item waiting on you', () => {
    const { container } = render(
      <ItemStateChip
        state="waiting_human"
        label={describeState({ state: 'waiting_human', agent: null, rework_count: 0 })}
      />,
    );

    expect(screen.getByText('Waiting on you')).toBeTruthy();
    expect(iconClass(container)).toContain('text-amber');
  });

  it('shows the check for a ready item, which the page has no buffer for', () => {
    const { container } = render(
      <ItemStateChip state="ready" label={describeState({ state: 'ready', agent: null, rework_count: 0 })} />,
    );

    expect(screen.getByText('Ready')).toBeTruthy();
    expect(iconClass(container)).toContain('text-ok-dot');
  });

  it('shows no icon for a blocked item', () => {
    const { container } = render(
      <ItemStateChip state="blocked" label={describeState({ state: 'blocked', agent: null, rework_count: 0 })} />,
    );

    expect(screen.getByText('Blocked, needs a retry')).toBeTruthy();
    expect(iconClass(container)).toBeNull();
  });
});
