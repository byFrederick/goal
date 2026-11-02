// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { useCallback, useState } from 'react';
import { afterEach, describe, expect, it } from 'vitest';
import { ShortcutsDialog, useShortcutsKey } from './shortcuts-dialog';

/** A page that opens the list on ?, with a field to type into, as the app's pages have. */
function Page() {
  const [open, setOpen] = useState(false);
  const [instant, setInstant] = useState(false);
  useShortcutsKey(
    useCallback(() => {
      setInstant(true);
      setOpen(true);
    }, []),
  );
  return (
    <>
      <label>
        Title
        <input />
      </label>
      <ShortcutsDialog open={open} instant={instant} onOpenChange={setOpen} />
    </>
  );
}

afterEach(cleanup);

describe('the keyboard shortcuts list', () => {
  it('opens on ? with no transition, and lists only the shortcuts the app binds', async () => {
    render(<Page />);
    fireEvent.keyDown(document.body, { key: '?' });

    const dialog = await screen.findByRole('dialog', { name: 'Keyboard shortcuts' });
    expect(dialog.className).not.toContain('motion-dialog');
    const text = dialog.textContent ?? '';
    for (const label of ['Search', 'New item', 'Next and previous row', 'Move the item', 'Approve', 'Deny', 'Undo']) {
      expect(text).toContain(label);
    }
    // Shortcuts the export shows but nothing binds yet are not listed.
    expect(text).not.toContain('Go to');
    expect(text).not.toContain('Save');
  });

  it('does not open on ? typed into a field', () => {
    render(<Page />);
    fireEvent.keyDown(screen.getByRole('textbox', { name: 'Title' }), { key: '?' });

    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('does not open on ? with Ctrl, Meta or Alt held', () => {
    render(<Page />);
    fireEvent.keyDown(document.body, { key: '?', ctrlKey: true });
    fireEvent.keyDown(document.body, { key: '?', metaKey: true });
    fireEvent.keyDown(document.body, { key: '?', altKey: true });

    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('closes on Escape', async () => {
    render(<Page />);
    fireEvent.keyDown(document.body, { key: '?' });
    await screen.findByRole('dialog', { name: 'Keyboard shortcuts' });
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  });

  it('opened from a menu, uses the standard dialog transition', async () => {
    render(<ShortcutsDialog open instant={false} onOpenChange={() => {}} />);
    const dialog = await screen.findByRole('dialog', { name: 'Keyboard shortcuts' });

    expect(dialog.className).toContain('motion-dialog');
    expect(within(dialog).getByRole('heading', { name: 'Global' })).toBeTruthy();
  });
});
