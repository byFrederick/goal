// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Menu, MenuContent, MenuItem, MenuTrigger } from './menu';

afterEach(cleanup);

function renderMenu(open: boolean, handlers: { onPointerMove?: () => void; onPointerLeave?: () => void } = {}) {
  render(
    <>
      <button type="button">elsewhere</button>
      <Menu open={open}>
        <MenuTrigger>Open</MenuTrigger>
        <MenuContent forceMount aria-label="Actions">
          <MenuItem {...handlers}>Act</MenuItem>
        </MenuContent>
      </Menu>
    </>,
  );
  const elsewhere = screen.getByText('elsewhere');
  elsewhere.focus();
  return { elsewhere, item: screen.getByRole('menuitem', { name: 'Act', hidden: true }) };
}

describe('a menu item', () => {
  it('leaves focus alone when the pointer moves over or off it while its panel closes', () => {
    const onPointerMove = vi.fn();
    const onPointerLeave = vi.fn();
    const { elsewhere, item } = renderMenu(false, { onPointerMove, onPointerLeave });
    expect(item.closest('[role="menu"]')?.getAttribute('data-state')).toBe('closed');
    fireEvent.pointerMove(item, { pointerType: 'mouse' });
    fireEvent.pointerLeave(item, { pointerType: 'mouse' });
    expect(document.activeElement).toBe(elsewhere);
    expect(onPointerMove).toHaveBeenCalledTimes(1);
    expect(onPointerLeave).toHaveBeenCalledTimes(1);
  });

  it('still takes focus under the pointer while its panel is open', () => {
    const { item } = renderMenu(true);
    fireEvent.pointerMove(item, { pointerType: 'mouse' });
    expect(document.activeElement).toBe(item);
  });
});
