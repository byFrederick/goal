// The desktop Board: every column's body starts at the same height, whatever it holds (an empty column,
// a column of cards, the drum's labelled column), and the header cards share one height.
import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll, isPhone } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

/**
 * The top edge of each column's first body element (a card, or the Empty text), and its header's height.
 * All boxes are read in one page.evaluate, so they come from one layout pass: reading them one await at a
 * time lets the board move between reads (the mock's latency shifts it once the columns have loaded).
 */
async function columnEdges(page: import('@playwright/test').Page) {
  return page.evaluate(() => {
    const columns = Array.from(
      document.querySelectorAll<HTMLElement>('section[aria-label="Stations"] > div > section'),
    );
    const tops: { name: string; top: number }[] = [];
    const heights: { name: string; height: number }[] = [];
    for (const column of columns) {
      const name = column.getAttribute('aria-label') ?? 'column';
      const header = column.querySelector<HTMLElement>(':scope > header');
      const zone = column.querySelector<HTMLElement>(':scope > div:last-of-type');
      const first = zone?.querySelector<HTMLElement>(':scope > article, :scope > p');
      if (!header || !first) throw new Error(`${name}: no header or first body element is laid out`);
      tops.push({ name, top: first.getBoundingClientRect().top });
      heights.push({ name, height: header.getBoundingClientRect().height });
    }
    return { tops, heights };
  });
}

test('the first card or Empty text of every desktop column is level, and the headers match', async ({ page }) => {
  test.skip(isPhone(page), 'the stations are side by side from 768 px');
  await page.goto('/o/acme/f/goal/board');
  await expect(page.getByRole('region', { name: 'Stations' })).toBeVisible();
  // Let the board finish loading before measuring: its first render is shifted by the mock's latency.
  await page.waitForLoadState('networkidle');

  const { tops, heights } = await columnEdges(page);
  expect(tops.length).toBeGreaterThan(1);
  const topValues = tops.map((t) => t.top);
  expect(Math.max(...topValues) - Math.min(...topValues), JSON.stringify(tops)).toBeLessThanOrEqual(1);
  const headerValues = heights.map((h) => h.height);
  expect(Math.max(...headerValues) - Math.min(...headerValues), JSON.stringify(heights)).toBeLessThanOrEqual(1);
});

test('on a phone the empty sub-header slot is not shown', async ({ page }) => {
  test.skip(!isPhone(page), 'the phone shows one column at a time');
  await page.goto('/o/acme/f/goal/board');
  // The phone's columns are behind the station tabs; the slot is hidden at every width below 768 px.
  const slots = page.locator('[aria-hidden="true"].min-h-\\[1lh\\]');
  for (let i = 0; i < (await slots.count()); i++) {
    await expect(slots.nth(i)).toBeHidden();
  }
});

test.describe('at 1680 px', () => {
  test.use({ viewport: { width: 1680, height: 1000 } });

  test('the columns sit side by side, every card lies in its column and no header overflows', async ({ page }) => {
    await page.goto('/o/acme/f/goal/board');
    await expect(page.getByRole('region', { name: 'Stations' })).toBeVisible();
    await page.waitForLoadState('networkidle');

    // Every column's box, its cards' boxes (the expedite card too) and its header's overflow, in one layout pass.
    const layout = await page.evaluate(() => {
      const columns = Array.from(
        document.querySelectorAll<HTMLElement>('section[aria-label="Stations"] > div > section'),
      );
      return columns.map((column) => {
        const box = column.getBoundingClientRect();
        const header = column.querySelector<HTMLElement>(':scope > header');
        return {
          name: column.getAttribute('aria-label') ?? 'column',
          left: box.left,
          right: box.right,
          cards: Array.from(column.querySelectorAll<HTMLElement>('article')).map((card) => {
            const rect = card.getBoundingClientRect();
            return { title: card.textContent?.slice(0, 40) ?? '', left: rect.left, right: rect.right };
          }),
          header: header ? { scrollWidth: header.scrollWidth, clientWidth: header.clientWidth } : null,
        };
      });
    });

    expect(layout.length).toBeGreaterThan(1);
    for (let i = 0; i + 1 < layout.length; i++) {
      const left = layout[i];
      const right = layout[i + 1];
      if (!left || !right) continue;
      expect(left.right, `${left.name} overlaps ${right.name}`).toBeLessThanOrEqual(right.left);
    }
    for (const column of layout) {
      for (const card of column.cards) {
        expect(card.left, `${card.title} starts before ${column.name}`).toBeGreaterThanOrEqual(column.left);
        expect(card.right, `${card.title} spills out of ${column.name}`).toBeLessThanOrEqual(column.right);
      }
      expect(column.header, `${column.name} has no header`).not.toBeNull();
      expect(column.header?.scrollWidth, `${column.name} header overflows`).toBeLessThanOrEqual(
        column.header?.clientWidth ?? 0,
      );
    }
    // Guard against a vacuous pass: the mock has cards, the expedite one among them.
    expect(layout.some((column) => column.cards.length > 0)).toBe(true);
  });
});

// The pictures for the pull request: the software factory's Board, whose Build column is the drum with
// its buffer (#7 and #8 under "Ready, buffer"), at 1440 and 390 px in both themes. They are attached to
// the test report; nothing is compared.
for (const theme of ['light', 'dark'] as const) {
  for (const size of [
    { name: '1440', width: 1440, height: 1000, path: '/o/acme/f/software/board' },
    { name: '390', width: 390, height: 844, path: '/o/acme/f/software/board?station=build' },
  ]) {
    test(`board screenshot, ${size.name} px, ${theme}`, async ({ page }, testInfo) => {
      await page.addInitScript((value) => window.localStorage.setItem('goal-theme', value), theme);
      await page.setViewportSize({ width: size.width, height: size.height });
      await page.goto(size.path);
      await expect(page.getByText(/^Ready, buffer/).first()).toBeVisible();
      await page.waitForLoadState('networkidle');
      await testInfo.attach(`board-${size.name}-${theme}`, {
        body: await page.screenshot({ fullPage: true }),
        contentType: 'image/png',
      });
    });
  }
}

test('fits the top bar at 768 with New item inside the viewport', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'tablet-768', 'the 768 project');
  await page.goto('/o/acme/f/goal/board');
  const button = page.getByRole('button', { name: 'New item' });
  await expect(button).toBeVisible();
  const box = await button.boundingBox();
  const width = page.viewportSize()?.width ?? 0;
  expect(box && box.x >= 0 && box.x + box.width <= width).toBe(true);
  await expectNoHorizontalScroll(page);
});

// Every column has its own Expedite box: the same top and height in every column, headers of one height, and
// from 1280 px every station fits without the board scrolling. Nothing outside the board's scroller passes the edge.
for (const width of [1440, 1280, 1024, 768]) {
  test(`the expedite boxes line up across the columns at ${width} px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 });
    await page.goto('/o/acme/f/goal/board');
    await expect(page.getByRole('region', { name: 'Stations', exact: true })).toBeVisible();
    await page.waitForLoadState('networkidle');

    const geometry = await page.evaluate(() => {
      const stations = document.querySelector<HTMLElement>('section[aria-label="Stations"]');
      const columns = Array.from(stations?.querySelectorAll<HTMLElement>(':scope > div > section') ?? []);
      const lastRight = columns[columns.length - 1]?.getBoundingClientRect().right ?? 0;
      return {
        scrollWidth: stations?.scrollWidth ?? 0,
        clientWidth: stations?.clientWidth ?? 0,
        lastRight,
        sectionRight: stations?.getBoundingClientRect().right ?? 0,
        boxes: columns.map((column) => {
          const box = column.querySelector('[data-zone="expedite"]')?.getBoundingClientRect();
          return box ? { top: box.top, height: box.height } : null;
        }),
        headers: columns.map((column) => column.querySelector(':scope > header')?.getBoundingClientRect().height ?? 0),
        pastColumn: columns.flatMap((column) => {
          const edge = column.getBoundingClientRect().right + 1;
          return Array.from(column.querySelectorAll<HTMLElement>('*'))
            .filter((el) => el.getBoundingClientRect().width > 0 && el.getBoundingClientRect().right > edge)
            .map((el) => el.tagName);
        }),
        overflowing: Array.from(document.querySelectorAll<HTMLElement>('body *'))
          .filter((el) => !stations?.contains(el) && el.getBoundingClientRect().right > window.innerWidth + 1)
          .map((el) => el.tagName),
      };
    });

    expect(geometry.boxes.length).toBeGreaterThan(0);
    for (const box of geometry.boxes) {
      expect(box).not.toBeNull();
      expect(Math.abs((box?.top ?? 0) - (geometry.boxes[0]?.top ?? 0))).toBeLessThanOrEqual(1);
      expect(Math.abs((box?.height ?? 0) - (geometry.boxes[0]?.height ?? 0))).toBeLessThanOrEqual(1);
    }
    for (const height of geometry.headers) expect(Math.abs(height - (geometry.headers[0] ?? 0))).toBeLessThanOrEqual(1);
    if (width >= 1280) {
      expect(geometry.scrollWidth).toBeLessThanOrEqual(geometry.clientWidth);
      expect(geometry.lastRight).toBeLessThanOrEqual(geometry.sectionRight + 1);
    }
    expect(geometry.pastColumn).toEqual([]);
    expect(geometry.overflowing).toEqual([]);
  });
}

test('on a phone the expedite lane shows only its chip and nothing reaches past the edge', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/o/acme/f/sre/board?station=investigate');
  const lane = page.getByRole('tabpanel').getByRole('group', { name: 'Expedite lane' });
  await expect(lane).toContainText('Expedite');
  await expect(lane).not.toContainText('in use');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});

// A crowded station: three expedite items in Build (one with a long title), one in Review. The cards stack in
// their station's own box, never overlap and are exactly as wide as their column; titles take at most two lines.
for (const width of [1440, 1280, 1024, 768]) {
  test(`expedite cards stack inside their box and fill the column at ${width} px`, async ({ page, request }) => {
    await request.post('/__mock/reset', { data: { expediteCrowd: true } });
    await page.setViewportSize({ width, height: 1000 });
    await page.goto('/o/acme/f/goal/board');
    await expect(page.getByRole('region', { name: 'Stations', exact: true })).toBeVisible();
    await page.waitForLoadState('networkidle');

    const g = await page.evaluate(() => {
      const rect = (el: Element) => {
        const r = el.getBoundingClientRect();
        return { left: r.left, right: r.right, top: r.top, bottom: r.bottom };
      };
      const columns = Array.from(
        document.querySelectorAll<HTMLElement>('section[aria-label="Stations"] > div > section'),
      );
      return columns.map((column) => {
        const box = column.querySelector('[data-zone="expedite"]');
        return {
          column: rect(column),
          box: box ? { ...rect(box), height: box.getBoundingClientRect().height } : null,
          cards: Array.from(box?.querySelectorAll<HTMLElement>('article') ?? []).map((card) => {
            const link = card.querySelector<HTMLElement>('h3 a');
            const lineHeight = link ? Number.parseFloat(getComputedStyle(link).lineHeight) : 0;
            return {
              ...rect(card),
              clipped: card.scrollWidth > card.clientWidth,
              title: link?.getAttribute('title') ?? '',
              text: link?.textContent ?? '',
              lines: link && lineHeight ? Math.round(link.getBoundingClientRect().height / lineHeight) : 0,
            };
          }),
        };
      });
    });

    const withCards = g.filter((c) => c.cards.length > 0);
    expect(withCards.map((c) => c.cards.length).sort()).toEqual([1, 3].sort());
    const tops = g.map((c) => c.box?.top ?? 0);
    const tallest = Math.max(...g.map((c) => c.box?.height ?? 0));
    for (const c of g) {
      expect(c.box).not.toBeNull();
      // (d) one top, and every box as tall as the tallest.
      expect(Math.abs((c.box?.top ?? 0) - (tops[0] ?? 0))).toBeLessThanOrEqual(1);
      expect(Math.abs((c.box?.height ?? 0) - tallest)).toBeLessThanOrEqual(1);
      c.cards.forEach((card, i) => {
        // (b) inside the column and the box; (c) as wide as the column, titles complete and clamped, nothing clipped.
        expect(card.left).toBeGreaterThanOrEqual((c.column.left ?? 0) - 1);
        expect(card.right).toBeLessThanOrEqual(c.column.right + 1);
        expect(card.left).toBeGreaterThanOrEqual((c.box?.left ?? 0) - 1);
        expect(card.right).toBeLessThanOrEqual((c.box?.right ?? 0) + 1);
        expect(card.bottom).toBeLessThanOrEqual((c.box?.bottom ?? 0) + 1);
        expect(card.right - card.left).toBeLessThanOrEqual(c.column.right - c.column.left + 1);
        expect(card.clipped).toBe(false);
        expect(card.title).toBe(card.text);
        expect(card.lines).toBeLessThanOrEqual(2);
        // (a) stacked one under the other, never overlapping.
        const next = c.cards[i + 1];
        if (next) {
          expect(next.top).toBeGreaterThanOrEqual(card.bottom);
          expect(next.top - card.bottom).toBeLessThanOrEqual(16);
        }
      });
    }
    // (b) no card meets a card of another column.
    const all = g.flatMap((c, ci) => c.cards.map((card) => ({ ci, ...card })));
    for (const a of all)
      for (const b of all)
        if (a.ci < b.ci)
          expect(a.right <= b.left || b.right <= a.left || a.bottom <= b.top || b.bottom <= a.top).toBe(true);
  });
}

test('on a phone the crowded station stacks its expedite cards inside the lane', async ({ page, request }) => {
  await request.post('/__mock/reset', { data: { expediteCrowd: true } });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/o/acme/f/goal/board?station=build');
  const lane = page.getByRole('tabpanel').getByRole('group', { name: 'Expedite lane' });
  await expect(lane.locator('article')).toHaveCount(3);
  const boxes = await lane.locator('article').evaluateAll((cards) =>
    cards.map((card) => {
      const r = card.getBoundingClientRect();
      return {
        left: r.left,
        right: r.right,
        top: r.top,
        bottom: r.bottom,
        clipped: card.scrollWidth > card.clientWidth,
      };
    }),
  );
  const laneBox = await lane.boundingBox();
  boxes.forEach((card, i) => {
    expect(card.clipped).toBe(false);
    expect(card.left).toBeGreaterThanOrEqual((laneBox?.x ?? 0) - 1);
    expect(card.right).toBeLessThanOrEqual((laneBox?.x ?? 0) + (laneBox?.width ?? 0) + 1);
    const next = boxes[i + 1];
    if (next) expect(next.top).toBeGreaterThanOrEqual(card.bottom);
  });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});

// A Done card with a long assembly title (mock item 21's parts) must truncate inside its column.
for (const width of [1280, 1440]) {
  test(`every card stays inside its column and the board does not scroll sideways at ${width} px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 });
    await page.goto('/o/acme/f/goal/board');
    // A phone shows the picked station's own region; the desktop shows all of them under 'Stations'.
    await expect(page.getByRole('region', { name: width < 768 ? 'Build' : 'Stations' })).toBeVisible();
    await page.waitForLoadState('networkidle');

    const result = await page.evaluate(() => {
      const region = document.querySelector<HTMLElement>('section[aria-label="Stations"]');
      const columns = Array.from(region?.querySelectorAll<HTMLElement>(':scope > div > section') ?? []);
      return {
        scrollWidth: region?.scrollWidth ?? 0,
        clientWidth: region?.clientWidth ?? 0,
        overflowing: columns.flatMap((column) =>
          Array.from(column.querySelectorAll<HTMLElement>('*'))
            .filter((el) => !el.closest('.sr-only'))
            .filter((el) => el.getBoundingClientRect().right > column.getBoundingClientRect().right + 0.5)
            .map((el) => `${el.tagName.toLowerCase()} "${el.textContent?.slice(0, 40) ?? ''}"`),
        ),
        ages: Array.from(document.querySelectorAll('article')).some((a) => /\b23 min\b/.test(a.textContent ?? '')),
        longTitle: Array.from(document.querySelectorAll('article span.truncate')).some(
          (span) => (span.textContent?.length ?? 0) > 80,
        ),
      };
    });
    expect(result.longTitle, 'the mock has a Done card with a long assembly title').toBe(true);
    expect(result.ages, 'the mock has a card whose age reads 23 min').toBe(true);
    expect(result.overflowing).toEqual([]);
    expect(result.scrollWidth).toBeLessThanOrEqual(result.clientWidth);
  });
}

// Marks drawn one per unit (an assembly's parts, a station's WIP, a lane's slots) fit their card, header and
// column for any count: the software board seeds assemblies of 24 and 60 parts (apps/web/mock/fixtures.ts).
for (const width of [1440, 1280, 1024, 768, 390]) {
  test(`at ${width} px no card is wider than its content and no part count lands on another card`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 });
    await page.goto(width < 768 ? '/o/acme/f/software/board?station=build' : '/o/acme/f/software/board');
    // A phone shows the picked station's own region; the desktop shows all of them under 'Stations'.
    await expect(page.getByRole('region', { name: width < 768 ? 'Build' : 'Stations' })).toBeVisible();
    await page.waitForLoadState('networkidle');

    const result = await page.evaluate(() => {
      const cards = Array.from(document.querySelectorAll<HTMLElement>('article'));
      const boxes = cards.map((card) => card.getBoundingClientRect());
      const wide: string[] = [];
      const overlaps: string[] = [];
      const counts = Array.from(document.querySelectorAll<HTMLElement>('article a span')).filter((el) =>
        /^\d+ of \d+$/.test((el.textContent ?? '').trim()),
      );
      cards.forEach((card, i) => {
        const box = boxes[i] as DOMRect;
        if (card.scrollWidth > card.clientWidth) wide.push(`${card.getAttribute('aria-label') ?? i}: card`);
        for (const el of Array.from(card.querySelectorAll<HTMLElement>('*'))) {
          const r = el.getBoundingClientRect();
          if (r.width > 0 && r.right > box.right + 1)
            wide.push(`${card.getAttribute('aria-label') ?? i}: ${el.tagName}`);
        }
      });
      for (const el of counts) {
        const r = el.getBoundingClientRect();
        const own = el.closest('article');
        cards.forEach((card, i) => {
          const b = boxes[i] as DOMRect;
          if (card === own) return;
          if (r.left < b.right && r.right > b.left && r.top < b.bottom && r.bottom > b.top) {
            overlaps.push(`${el.textContent} overlaps another card`);
          }
        });
      }
      return { count: counts.length, wide, overlaps };
    });
    expect(result.wide).toEqual([]);
    expect(result.overlaps).toEqual([]);
    if (width >= 768) expect(result.count).toBeGreaterThan(0);
  });
}

test('a station with a WIP limit of 50 and a lane with 50 slots fit their column', async ({ page }) => {
  test.skip(isPhone(page), 'the stations are side by side from 768 px');
  await page.route(/\/factories\/[^/]+\/board(\?|$)/, async (route) => {
    const response = await route.fetch();
    const board = await response.json();
    for (const station of board.stations) {
      if (station.kind !== 'agent') continue;
      station.wip_limit = 50;
      for (const lane of station.lanes ?? []) {
        if (lane.slots !== null) lane.slots = 50;
      }
    }
    await route.fulfill({ response, json: board });
  });
  await page.goto('/o/acme/f/software/board');
  await expect(page.getByRole('region', { name: 'Stations' })).toBeVisible();
  await page.waitForLoadState('networkidle');
  // Proves the intercept applied before the overflow check means anything.
  await expect(page.getByRole('img', { name: /of 50 in progress/ }).first()).toBeVisible();

  const overflowing = await page.evaluate(() => {
    const out: string[] = [];
    for (const column of Array.from(
      document.querySelectorAll<HTMLElement>('section[aria-label="Stations"] > div > section'),
    )) {
      const box = column.getBoundingClientRect();
      for (const el of Array.from(column.querySelectorAll<HTMLElement>('header, header *, [role="img"]'))) {
        const r = el.getBoundingClientRect();
        if (r.width > 0 && (r.right > box.right + 1 || r.left < box.left - 1)) {
          out.push(`${column.getAttribute('aria-label')}: ${el.tagName} ${Math.round(r.right - box.right)}px`);
        }
      }
    }
    return out;
  });
  expect(overflowing).toEqual([]);
});
