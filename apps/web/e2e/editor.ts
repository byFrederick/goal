// The Definition editor in Playwright: the frame's landmarks and the YAML editor, typed into the way
// a person does (Monaco's Find, then the line; the textarea on phones). goal's draft of v9 is the
// mock's seed.
import { expect, type Page } from '@playwright/test';
import { isPhone } from './support';

export const definition = '/o/acme/f/goal/definition';
export const label = 'factory.yaml, draft of v9';

export const editor = (page: Page) => page.getByRole('textbox', { name: label });
export const tiles = (page: Page) => page.getByRole('navigation', { name: /^Definition sections/ });
export const tile = (page: Page, name: RegExp) => tiles(page).getByRole('link', { name });
export const status = (page: Page) => page.getByRole('status').filter({ hasText: /Based on|runs now/ });
export const problems = (page: Page) => page.getByRole('region', { name: /^Problems/ });

/**
 * How long Monaco may take to show its first lines: its chunk is about 850 KB gzipped, a cold
 * load on a busy machine outlasts the default 5 s.
 */
export const monacoTimeout = 20_000;

/** The YAML editor is ready: Monaco shows its lines (the textarea on phones). */
export async function editorReady(page: Page) {
  await expect(editor(page)).toBeAttached({ timeout: monacoTimeout });
  if (!isPhone(page)) {
    await expect(page.locator('.monaco-editor .view-line').first()).toBeVisible({ timeout: monacoTimeout });
  }
}

export async function openYaml(page: Page, section = 'line', search = '') {
  await page.goto(`${definition}/${section}?mode=yaml${search}`);
  await expect(page.getByRole('heading', { level: 1, name: 'Definition' })).toBeVisible();
  await editorReady(page);
}

/**
 * Moves the cursor to the first `text` in Monaco the way a person would: Find (Ctrl/Cmd+F), the
 * text, Enter, Escape; the match is selected and scrolled into view.
 */
export async function find(page: Page, text: string) {
  await editor(page).focus();
  await page.keyboard.press('ControlOrMeta+f');
  await page.keyboard.insertText(text);
  await page.keyboard.press('Enter');
  await page.keyboard.press('Escape');
}

/** The editor shows `text`: on a phone in the textarea, in Monaco on a line found by Find. */
export async function expectShown(page: Page, text: string) {
  if (isPhone(page)) {
    await expect(editor(page)).toHaveValue(new RegExp(text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')));
    return;
  }
  await find(page, text);
  await expect(page.locator('.monaco-editor .view-line').filter({ hasText: text }).first()).toBeVisible();
}

/**
 * Replaces one line's content (without its indent) and leaves the cursor on it: in Monaco by
 * finding it and selecting from its end to its first character; on phones by editing the
 * textarea's value, then putting the cursor on that line (a key press reports it, as typing would).
 */
export async function replaceLine(page: Page, from: string, to: string) {
  if (isPhone(page)) {
    const area = editor(page);
    const value = await area.inputValue();
    const at = value.indexOf(from);
    expect(at).toBeGreaterThanOrEqual(0);
    await area.fill(value.replace(from, to));
    await area.evaluate((el: HTMLTextAreaElement, i: number) => el.setSelectionRange(i, i), at);
    await area.press('ArrowRight');
    return;
  }
  await find(page, from);
  await page.keyboard.press('End');
  await page.keyboard.press('Shift+Home');
  if (to === '') await page.keyboard.press('Backspace');
  else await page.keyboard.insertText(to);
}
