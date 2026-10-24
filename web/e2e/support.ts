// What every flow checks: axe at WCAG 2.2 AA, no horizontal scroll, which chrome the width shows.
import AxeBuilder from '@axe-core/playwright';
import { expect, type Page } from '@playwright/test';

export const isPhone = (page: Page) => (page.viewportSize()?.width ?? 0) < 768;

export async function expectAccessible(page: Page) {
  const { violations } = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'])
    .analyze();
  expect(violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).join(', ')}`)).toEqual([]);
}

export async function expectNoHorizontalScroll(page: Page) {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(0);
}

/** The rail from 768 px, the top row below: the same landmark, one of them shown. */
export const orgNav = (page: Page) => page.getByRole('navigation', { name: 'Organization and factories' });

/** The organization mark of the shown navigation: it opens the organization menu or sheet. */
export const orgMarkOf = (page: Page, name = 'Ac, Acme organization, switch organization') =>
  orgNav(page).filter({ visible: true }).getByRole('button', { name });
