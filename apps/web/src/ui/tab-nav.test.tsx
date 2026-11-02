// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { TabNav, tabNavItem } from './tab-nav';

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
});
