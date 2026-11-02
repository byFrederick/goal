// Test helpers for ui/select.tsx (Radix Select) in jsdom. Importing this file adds the pointer and
// scroll methods jsdom lacks and Radix Select calls; `choose` opens a select and clicks an option.
import { fireEvent, screen, within } from '@testing-library/react';

const proto = Element.prototype as unknown as Record<string, unknown>;
proto.hasPointerCapture ??= () => false;
proto.setPointerCapture ??= () => {};
proto.releasePointerCapture ??= () => {};
proto.scrollIntoView ??= () => {};

/** The select's trigger, by its accessible name. */
export const trigger = (name: string | RegExp) => screen.getByRole('combobox', { name });

/** The text a select shows for its chosen option. */
export const shownChoice = (name: string | RegExp) => trigger(name).textContent;

/** Opens the select with the keyboard and chooses the option with this label. */
export async function choose(name: string | RegExp, option: string | RegExp) {
  fireEvent.keyDown(trigger(name), { key: 'Enter' });
  const list = await screen.findByRole('listbox');
  fireEvent.click(within(list).getByRole('option', { name: option }));
}
