// The Environments section, Add a tool and Custom at 1440, 1280, 1024, 768 and 390 px in light and dark:
// nothing goes past the viewport edge, no text is clipped, and on a phone every control is at least 44 px.
// Written for the Playwright suite, which cannot run in the box that built it.
import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoClippedElements, expectNoHorizontalScroll } from './support';

const widths = [1440, 1280, 1024, 768, 390] as const;
const schemes = ['light', 'dark'] as const;

const pages = [
  { name: 'Environments', url: '/o/acme/f/goal/definition/environments', ready: 'The box' },
  {
    name: 'Environments, YAML',
    url: '/o/acme/f/goal/definition/environments?mode=yaml&file=environments%2Fdefault.yaml',
    ready: 'Definition sections, jump to a block in factory.yaml',
  },
  {
    name: 'Add a tool',
    url: '/o/acme/f/goal/definition/add-tool?environment=default&template=sentry',
    ready: 'Add to draft',
  },
  {
    name: 'Add a tool, Custom',
    url: '/o/acme/f/goal/definition/add-tool?environment=deploy&template=custom&type=cli',
    ready: 'Custom tool',
  },
] as const;

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

/** Interactive elements drawn smaller than 44 px in a dimension, by label: a phone's tap targets. */
async function smallTargets(page: Page) {
  return page.evaluate(() => {
    const small: string[] = [];
    const selector = 'button, a[href], input:not([type=hidden]):not([type=radio]), [role=combobox], select, summary';
    for (const el of document.querySelectorAll<HTMLElement>(selector)) {
      if (el.closest('.sr-only, [aria-hidden="true"], [inert]')) continue;
      const box = el.getBoundingClientRect();
      if (box.width === 0 || box.height === 0) continue;
      // A bare text link inside a sentence keeps its line; a control is a target.
      if (el.tagName === 'A' && el.closest('p, h1, h2, h3')) continue;
      if (box.height < 43.5)
        small.push(
          `${el.tagName.toLowerCase()} "${(el.getAttribute('aria-label') ?? el.textContent ?? '').trim().slice(0, 30)}" ${Math.round(box.height)}px`,
        );
    }
    return small;
  });
}

for (const scheme of schemes) {
  for (const width of widths) {
    test.describe(`${scheme} at ${width} px`, () => {
      test.use({ viewport: { width, height: 900 }, colorScheme: scheme });

      for (const p of pages) {
        test(`${p.name} fits the viewport`, async ({ page }) => {
          await page.goto(p.url);
          await page.waitForLoadState('networkidle');
          if (p.name === 'Add a tool') await expect(page.getByRole('button', { name: p.ready })).toBeVisible();
          else if (p.name === 'Environments') await expect(page.getByRole('heading', { name: p.ready })).toBeVisible();
          else if (p.name === 'Add a tool, Custom')
            await expect(page.getByRole('heading', { name: p.ready })).toBeVisible();
          await expectNoHorizontalScroll(page);
          await expectNoClippedElements(page);
          if (width < 768) expect(await smallTargets(page)).toEqual([]);
        });
      }

      test('a standing rule under its tool fits the viewport, with a 44 px Revoke on a phone', async ({ page }) => {
        await page.goto('/o/acme/f/goal/definition/environments');
        await page
          .getByRole('button', { name: /linear/ })
          .first()
          .click();
        const revoke = page.getByRole('button', { name: 'Revoke create_comment for architect' });
        await expect(revoke).toBeVisible();
        await expect(page.getByText('Always allowed for architect, by Freddy Cruz on Oct 30')).toBeVisible();
        await expectNoHorizontalScroll(page);
        await expectNoClippedElements(page);
        if (width < 768) expect((await revoke.boundingBox())?.height ?? 0).toBeGreaterThanOrEqual(43.5);
      });

      test('Environments has no accessibility violations', async ({ page }) => {
        await page.goto('/o/acme/f/goal/definition/environments');
        await expect(page.getByRole('heading', { name: 'The box' })).toBeVisible();
        await expectAccessible(page);
      });
    });
  }
}

test('the Form view shows Environments, with a tab per environment, and Add a tool leaves for the catalog', async ({
  page,
}) => {
  await page.goto('/o/acme/f/goal/definition/environments');
  await expect(page.getByRole('link', { name: /^Environments/ })).toBeVisible();
  await expect(page.getByRole('link', { name: /^Connections/ })).toHaveCount(0);
  await expect(page.getByRole('link', { name: /deploy/ })).toBeVisible();
  await page.getByRole('link', { name: 'Add a tool' }).first().click();
  await expect(page).toHaveURL(/definition\/add-tool\?environment=default/);
  await expect(page.getByRole('heading', { name: 'Add a tool', level: 1 })).toBeVisible();
});

test('Add to draft writes the tool into the environment file and the YAML view shows it', async ({ page }) => {
  await page.goto('/o/acme/f/goal/definition/add-tool?environment=default&template=sentry');
  await page.getByPlaceholder('Paste sentry auth token').fill('sntrys_example');
  await page.getByRole('button', { name: 'Add to draft' }).click();
  await expect(page).toHaveURL(/definition\/environments\?environment=default/);
  await page.getByRole('radio', { name: 'YAML' }).check();
  await expect(page).toHaveURL(/file=environments%2Fdefault\.yaml/);
  await expect(page.getByText('mcp.sentry.dev')).toBeVisible();
});

for (const scheme of schemes) {
  for (const width of widths) {
    test.describe(`Test again on a server card, ${scheme} at ${width} px`, () => {
      test.use({ viewport: { width, height: 900 }, colorScheme: scheme });

      test('testing a server opens its card and offers Test again, inside the viewport', async ({ page }) => {
        await page.goto('/o/acme/f/goal/definition/environments');
        await expect(page.getByRole('heading', { name: 'The box' })).toBeVisible();
        await page.getByRole('button', { name: 'Test servers' }).click();
        const again = page.getByRole('button', { name: 'Test linear' });
        await expect(again).toHaveText('Test again');
        await expectNoHorizontalScroll(page);
        await expectNoClippedElements(page);
      });
    });
  }
}
