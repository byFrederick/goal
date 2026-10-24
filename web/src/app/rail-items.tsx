// The pieces the rail and the phone's top row share: one stop of the arrow-key movement, a
// factory's square, a screen a later phase builds, and the account menu.
import { Link } from '@tanstack/react-router';
import { type KeyboardEvent, type ReactNode, useCallback } from 'react';
import type { OrgSummary } from '../api/types';
import { cn } from '../ui/cn';
import { Avatar, Mark } from '../ui/mark';
import { Menu, MenuContent, MenuItem, MenuLabel, MenuSeparator, MenuTrigger } from '../ui/menu';
import { Tooltip } from '../ui/tooltip';
import type { FactorySquare } from './factory-squares';
import { type RailAxis, railFocusTarget } from './rail-focus';
import { laterText } from './screens';

/** A 44px target around a mark; `group` passes hover to the mark. */
export const railItemClass =
  'group relative flex size-11 shrink-0 items-center justify-center rounded-[10px] text-ink no-underline';

/** Marks an element as one stop of the rail's arrow-key movement. */
export const railItem = { 'data-rail-item': '' } as const;

/** Arrow keys, Home and End move focus between the rail's items (every item stays in tab order). */
export function useRailKeys(axis: RailAxis) {
  return useCallback(
    (e: KeyboardEvent<HTMLElement>) => {
      // Keys pressed in a menu, sheet or dialog a rail item opened bubble here through React's
      // portals; they are not the rail's.
      if (!e.currentTarget.contains(e.target as Node)) return;
      const items = [...e.currentTarget.querySelectorAll<HTMLElement>('[data-rail-item]')];
      const index = items.indexOf(document.activeElement as HTMLElement);
      const target = railFocusTarget(e.key, index, items.length, axis);
      if (target === null) return;
      e.preventDefault();
      items[target]?.focus();
    },
    [axis],
  );
}

type Size = 'rail' | 'phone';

/** A factory's square: its floor, current (ink, aria-current) on any of its pages. */
export function FactorySquareLink({ org, square, size }: { org: OrgSummary; square: FactorySquare; size: Size }) {
  const { factory, mark, attention, label, detail } = square;
  const link = (
    <Link
      to="/o/$org/f/$factory"
      params={{ org: org.slug, factory: factory.name }}
      aria-label={label}
      className={railItemClass}
      {...railItem}
    >
      {({ isActive }) => (
        <Mark
          kind="factory"
          size={size}
          current={isActive}
          badge={attention.waiting}
          dot={attention.andon !== null || attention.blocked > 0}
        >
          {mark}
        </Mark>
      )}
    </Link>
  );
  return size === 'rail' ? (
    <Tooltip label={factory.name} detail={detail}>
      {link}
    </Tooltip>
  ) : (
    link
  );
}

/**
 * A screen a later phase builds (New factory, Connector catalog, Credentials): marked disabled,
 * going nowhere.
 */
export function LaterRailItem({
  label,
  kind,
  size,
  children,
}: {
  label: string;
  kind: 'add' | 'icon';
  size: Size;
  children: ReactNode;
}) {
  const item = (
    // aria-disabled, not disabled: it stays focusable so its tooltip can say why it does nothing.
    <button
      type="button"
      aria-disabled="true"
      aria-label={label}
      className={cn(railItemClass, 'cursor-default')}
      {...railItem}
    >
      <Mark kind={kind} size={size}>
        {children}
      </Mark>
    </button>
  );
  return size === 'rail' ? (
    <Tooltip label={label} detail={laterText}>
      {item}
    </Tooltip>
  ) : (
    item
  );
}

/**
 * A menu trigger on the rail opens its menu with Enter or Space; ArrowDown keeps moving along the
 * rail, as on every other rail item (Radix would open the menu on it).
 */
export function keepArrowsOnRail(e: KeyboardEvent<HTMLElement>) {
  if (e.key === 'ArrowDown') e.preventDefault();
}

/**
 * The person using the install and its menu. Phase 2.2 has one local user and no sign-in
 * (no-auth): the menu names them and the install; their role and Sign out arrive with phase 4.
 */
export function AccountMenu({ size }: { size: Size }) {
  const trigger = (
    <MenuTrigger
      aria-label="LU, Local user, account"
      className={railItemClass}
      onKeyDown={size === 'rail' ? keepArrowsOnRail : undefined}
      {...railItem}
    >
      <Avatar initials="LU" size={size} />
    </MenuTrigger>
  );
  return (
    <Menu>
      {size === 'rail' ? (
        <Tooltip label="Local user" detail="This install">
          {trigger}
        </Tooltip>
      ) : (
        trigger
      )}
      <MenuContent side={size === 'rail' ? 'right' : 'bottom'} align="end" className="w-64">
        <MenuLabel className="mt-1 flex items-center gap-3 text-ink">
          <Avatar initials="LU" size="phone" />
          <span className="min-w-0">
            <span className="block text-[15px] font-medium">Local user</span>
            <span className="block font-normal text-muted">This install</span>
          </span>
        </MenuLabel>
        <MenuSeparator />
        <MenuItem disabled>
          Sign-in and accounts
          <span className="ml-auto text-xs">{laterText}</span>
        </MenuItem>
      </MenuContent>
    </Menu>
  );
}
