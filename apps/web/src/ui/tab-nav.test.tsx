// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { TabNav, tabNavItem, tabNavUnderlineItem } from './tab-nav';

afterEach(cleanup);

describe('TabNav', () => {
  it('names the navigation and marks the current link', () => {
    render(
      <TabNav label="Jonah">
        <a href="/a" aria-current="page" className={tabNavItem}>
          Overview
        </a>
        <a href="/b" className={tabNavItem}>
          Judges
        </a>
      </TabNav>,
    );
    const nav = screen.getByRole('navigation', { name: 'Jonah' });
    expect(nav.querySelector('[aria-current="page"]')?.textContent).toBe('Overview');
    expect(nav.querySelector('[aria-hidden="true"]')).not.toBeNull();
  });

  it('draws the underline variant as a rule with a line under the current link', () => {
    render(
      <TabNav label="Views" variant="underline">
        <a href="/a" aria-current="page" className={tabNavUnderlineItem}>
          Overview
        </a>
      </TabNav>,
    );
    const nav = screen.getByRole('navigation', { name: 'Views' });
    expect(nav.className).toContain('border-b');
    expect(nav.className).not.toContain('bg-panel');
    expect(nav.querySelector('[aria-hidden="true"]')?.className).toContain('bg-ink');
  });

  it('moves the indicator without a transition when a key changed the link', () => {
    const tabs = (current: 'a' | 'b') => (
      <TabNav label="Views" variant="underline">
        <a href="/a" aria-current={current === 'a' ? 'page' : undefined} className={tabNavUnderlineItem}>
          A
        </a>
        <a href="/b" aria-current={current === 'b' ? 'page' : undefined} className={tabNavUnderlineItem}>
          B
        </a>
      </TabNav>
    );
    const { rerender } = render(tabs('a'));
    const nav = screen.getByRole('navigation', { name: 'Views' });
    const line = nav.querySelector<HTMLElement>('[aria-hidden="true"]');
    fireEvent.keyDown(nav.querySelector('a') as Element, { key: 'Enter' });
    rerender(tabs('b'));
    expect(line?.style.transition).toBe('none');
    fireEvent.pointerDown(nav.querySelector('a') as Element);
    rerender(tabs('a'));
    expect(line?.style.transition).toBe('');
  });
});
