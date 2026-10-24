// The rail (≥ 768 px): 72 px, pinned to the left edge at full height. Top to bottom: the
// organization mark, a rule, one square per factory of this organization (scrolling when there
// are many), New factory, and at the bottom Connector catalog, Credentials and the account
// (C §10.3, canvas "Main" and "Factories").
import { Plus } from 'lucide-react';
import type { OrgSummary } from '../api/types';
import { useFactorySquares } from './factory-squares';
import { OrgMark } from './org-mark';
import { AccountMenu, FactorySquareLink, LaterRailItem, NewFactoryLink, useRailKeys } from './rail-items';
import { catalogScreen, credentialsScreen } from './screens';

const iconProps = { size: 20, strokeWidth: 1.5, 'aria-hidden': true } as const;

export function Rail({ org }: { org: OrgSummary }) {
  const squares = useFactorySquares(org);
  const onKeyDown = useRailKeys('vertical');
  const Catalog = catalogScreen.icon;
  const Credentials = credentialsScreen.icon;

  return (
    <nav
      aria-label="Organization and factories"
      onKeyDown={onKeyDown}
      className="sticky top-0 z-10 hidden h-dvh w-[72px] shrink-0 flex-col items-center border-r border-rule bg-shell pt-3.5 pb-4 md:flex"
    >
      <OrgMark org={org} size="rail" />
      <span aria-hidden="true" className="my-2.5 h-px w-7 shrink-0 bg-rule" />
      {/* Room around the squares so their badges are not clipped when the list scrolls. */}
      <ul className="m-0 flex min-h-0 w-full list-none flex-col items-center gap-1.5 overflow-y-auto px-0 pt-1.5 pb-1">
        {squares.map((square) => (
          <li key={square.factory.id}>
            <FactorySquareLink org={org} square={square} size="rail" />
          </li>
        ))}
        <li>
          <NewFactoryLink org={org} size="rail">
            <Plus size={18} strokeWidth={1.5} aria-hidden="true" />
          </NewFactoryLink>
        </li>
      </ul>
      <div className="mt-auto flex flex-col items-center gap-1.5 pt-3">
        <LaterRailItem label={catalogScreen.label} kind="icon" size="rail">
          <Catalog {...iconProps} />
        </LaterRailItem>
        <LaterRailItem label={credentialsScreen.label} kind="icon" size="rail">
          <Credentials {...iconProps} />
        </LaterRailItem>
        <AccountMenu size="rail" />
      </div>
    </nav>
  );
}
