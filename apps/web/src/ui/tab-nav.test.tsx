// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
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
          Tests
        </a>
      </TabNav>,
    );
    const nav = screen.getByRole('navigation', { name: 'Jonah' });
    expect(nav.querySelector('[aria-current="page"]')?.textContent).toBe('Overview');
    expect(nav.querySelector('[aria-hidden="true"]')).not.toBeNull();
  });

  it('draws the underline variant with a 2px ink indicator and a rule under the row', () => {
    render(
      <TabNav label="Jonah" variant="underline">
        <a href="/a" aria-current="page" className={tabNavItem}>
          Overview
        </a>
      </TabNav>,
    );
    const nav = screen.getByRole('navigation', { name: 'Jonah' });
    expect(nav.className).toContain('border-b');
    expect(nav.querySelector('[aria-hidden="true"]')?.className).toContain('h-0.5');
    expect(nav.querySelector('[aria-hidden="true"]')?.className).toContain('bg-ink');
  });

  it('places the underline with transform only', () => {
    const left = vi.spyOn(HTMLElement.prototype, 'offsetLeft', 'get').mockReturnValue(40);
    const width = vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(200);
    const { container } = render(
      <TabNav label="Jonah" variant="underline">
        <a href="/a" aria-current="page" className={tabNavItem}>
          Overview
        </a>
      </TabNav>,
    );
    const bar = container.querySelector<HTMLElement>('[aria-hidden="true"]');
    expect(bar?.style.transform).toBe('translateX(40px) scaleX(2)');
    expect(bar?.className).toContain('h-0.5');
    left.mockRestore();
    width.mockRestore();
  });

  it('places the pill with transform only', () => {
    const left = vi.spyOn(HTMLElement.prototype, 'offsetLeft', 'get').mockReturnValue(40);
    const width = vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(200);
    const { container } = render(
      <TabNav label="Jonah">
        <a href="/a" aria-current="page" className={tabNavItem}>
          Overview
        </a>
      </TabNav>,
    );
    const pill = container.querySelector<HTMLElement>('[aria-hidden="true"]');
    expect(pill?.style.transform).toBe('translateX(40px) scaleX(2)');
    expect(pill?.style.width).toBe('');
    expect(pill?.style.transition).not.toContain('width');
    expect(pill?.className).not.toMatch(/width/);
    expect(Array.from(pill?.classList ?? [])).toEqual(
      expect.arrayContaining(['transition-transform', 'origin-left', 'duration-(--duration-fast)', 'ease-out']),
    );
    left.mockRestore();
    width.mockRestore();
  });
});
