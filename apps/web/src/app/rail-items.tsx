// The pieces the phone's top row shares: one stop of the arrow-key movement, a factory's square and
// New factory (the account is account-menu.tsx, the organization's mark is org-mark.tsx). The
// sidebar of 768 px and up is sidebar.tsx.
import { Link, useRouterState } from '@tanstack/react-router';
import { type KeyboardEvent, type ReactNode, useCallback } from 'react';
import type { OrgSummary } from '../api/types';
import { Mark } from '../ui/mark';
import type { FactorySquare } from './factory-squares';
import { type RailAxis, railFocusTarget } from './rail-focus';
import { newFactoryScreen } from './screens';

/** A 44px target around a mark; `group` passes hover to the mark. */
export const railItemClass =
  'group relative flex size-11 shrink-0 items-center justify-center rounded-cell text-ink no-underline';

/** Marks an element as one stop of the top row's arrow-key movement. */
export const railItem = { 'data-rail-item': '' } as const;

/** Arrow keys, Home and End move focus between the top row's items (every item stays in tab order). */
export function useRailKeys(axis: RailAxis) {
  return useCallback(
    (e: KeyboardEvent<HTMLElement>) => {
      // Keys pressed in a menu, sheet or dialog a row item opened bubble here through React's
      // portals; they are not the row's.
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

/** A factory's square: its floor, current (ink, aria-current) on any of its pages. */
export function FactorySquareLink({ org, square }: { org: OrgSummary; square: FactorySquare }) {
  const { factory, mark, attention } = square;
  return (
    <Link
      to="/o/$org/f/$factory"
      params={{ org: org.slug, factory: factory.name }}
      aria-label={square.label}
      className={railItemClass}
      {...railItem}
    >
      {({ isActive }) => (
        <Mark
          kind="factory"
          current={isActive}
          badge={attention.waiting}
          dot={attention.andon !== null || attention.blocked > 0}
        >
          {mark}
        </Mark>
      )}
    </Link>
  );
}

/** New factory: the dashed square, solid and ink while the wizard is open. */
export function NewFactoryLink({ org, children }: { org: OrgSummary; children: ReactNode }) {
  const open = useIsWizard();
  return (
    <Link
      to={newFactoryScreen.to}
      params={{ org: org.slug, step: 'start' }}
      aria-label={newFactoryScreen.label}
      className={railItemClass}
      {...railItem}
    >
      <Mark kind="add" current={open}>
        {children}
      </Mark>
    </Link>
  );
}

/** Whether a page of the New factory wizard is open (any of its steps). */
function useIsWizard(): boolean {
  return useRouterState({ select: (s) => s.matches.some((m) => m.routeId === '/o/$org/new/$step') });
}
