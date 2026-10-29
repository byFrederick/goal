// The pieces the collapsed rail shares with the account menu: a factory's square and New factory, and the
// 44 px target around a mark. The sidebar of 768 px and up is sidebar.tsx; its collapsed rail is sidebar-rail.tsx.
import { Link, useRouterState } from '@tanstack/react-router';
import type { ReactNode } from 'react';
import type { OrgSummary } from '../api/types';
import { Mark } from '../ui/mark';
import { Tooltip } from '../ui/tooltip';
import type { FactorySquare } from './factory-squares';
import { newFactoryScreen } from './screens';

/**
 * The product docs, a plain link out of the SPA into the same tab: /docs is served by the same origin
 * (the control Worker or goald), so it is not a TanStack Router route. The rail and the sidebar share it.
 */
export const docsLink = { label: 'Docs', href: '/docs' } as const;

/** A 44px target around a mark; `group` passes hover to the mark. */
export const railItemClass =
  'group relative flex size-11 shrink-0 items-center justify-center rounded-cell text-ink no-underline';

/**
 * A factory's square: its floor, current (ink, aria-current) on any of its pages. `tip` shows the
 * square's name and status on hover and focus, for the collapsed rail.
 */
export function FactorySquareLink({
  org,
  square,
  tip = false,
}: {
  org: OrgSummary;
  square: FactorySquare;
  tip?: boolean;
}) {
  const { factory, mark, attention } = square;
  const link = (
    <Link
      to="/o/$org/f/$factory"
      params={{ org: org.slug, factory: factory.name }}
      aria-label={square.label}
      className={railItemClass}
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
  return tip ? (
    <Tooltip label={factory.name} detail={square.detail}>
      {link}
    </Tooltip>
  ) : (
    link
  );
}

/** New factory: the dashed square, solid and ink while the wizard is open. `tip` as for FactorySquareLink. */
export function NewFactoryLink({
  org,
  children,
  tip = false,
}: {
  org: OrgSummary;
  children: ReactNode;
  tip?: boolean;
}) {
  const open = useIsWizard();
  const link = (
    <Link
      to={newFactoryScreen.to}
      params={{ org: org.slug, step: 'start' }}
      aria-label={newFactoryScreen.label}
      className={railItemClass}
    >
      <Mark kind="add" current={open}>
        {children}
      </Mark>
    </Link>
  );
  return tip ? <Tooltip label={newFactoryScreen.label}>{link}</Tooltip> : link;
}

/** Whether a page of the New factory wizard is open (any of its steps). */
function useIsWizard(): boolean {
  return useRouterState({ select: (s) => s.matches.some((m) => m.routeId === '/o/$org/new/$step') });
}
