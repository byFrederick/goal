// The organization menu (C §10.4, canvas "Org-Switcher" and "Mobile-Org-Switcher"): every
// organization on the install (switch by choosing one), the current organization's own pages,
// and Create organization, for platform admins only (spec 3 §4.2; everyone with sign-in off, the
// local user creates them). A Radix DropdownMenu on the rail; a bottom sheet on phones. Both are
// opened by the organization mark (org-mark.tsx) and fed by useOrgMenuModel. The sheet ends with
// the account (account-menu.tsx). No role per organization and no Members and roles link yet:
// both arrive with Members (spec 3, PR 3-11).
import { useQuery, useSuspenseQuery } from '@tanstack/react-query';
import { Link, useLocation, useNavigate } from '@tanstack/react-router';
import { Check, Plus } from 'lucide-react';
import { type ReactNode, type RefObject, useId, useRef } from 'react';
import { meQuery, orgsQuery } from '../api/queries';
import type { OrgSummary } from '../api/types';
import { orgMark } from '../shared/marks';
import { factoryCount } from '../shared/org';
import { cn } from '../ui/cn';
import {
  MenuContent,
  MenuGroup,
  MenuItem,
  MenuLabel,
  MenuRadioGroup,
  MenuRadioItem,
  MenuSeparator,
  menuRow,
} from '../ui/menu';
import { SheetContent, SheetLabel } from '../ui/sheet';
import { AccountBlock } from './account-menu';
import { catalogScreen, credentialsScreen, laterText, type OrgScreen, settingsScreen } from './screens';

interface OrgChoice {
  org: OrgSummary;
  mark: string;
  /** "3 factories" */
  detail: string;
  current: boolean;
}

interface OrgMenuModel {
  orgs: OrgChoice[];
  /** The current organization's pages: catalog and credentials (phase 4), settings (C7). */
  links: { screen: OrgScreen; label: string }[];
  /** Create organization is offered: to platform admins, or to the local user with sign-in off. */
  canCreate: boolean;
  /**
   * Whether going to `path` leaves the current page. Only then does the new page's heading take
   * focus; otherwise focus returns to the mark as the menu closes.
   */
  leaves: (path: string) => boolean;
}

/** The path a link of the menu opens: an organization's screen under its slug. */
const orgPath = (to: string, slug: string) => to.replace('$org', slug);

function useOrgMenuModel(current: OrgSummary): OrgMenuModel {
  // Already cached: the /o/$org layout resolved the organization from this list.
  const { data } = useSuspenseQuery(orgsQuery);
  const pathname = useLocation({ select: (l) => l.pathname });
  // Hidden until goald says who this is: only platform admins create organizations.
  const { data: canCreate = false } = useQuery({ ...meQuery, select: (me) => me.mode === 'none' || me.platform_admin });
  return {
    orgs: data.map((org) => ({
      org,
      mark: orgMark(org),
      detail: factoryCount(org.factory_count),
      current: org.id === current.id,
    })),
    links: [
      { screen: catalogScreen, label: catalogScreen.label },
      { screen: credentialsScreen, label: credentialsScreen.label },
      { screen: settingsScreen, label: 'Organization settings' },
    ],
    canCreate,
    leaves: (path) => path !== pathname,
  };
}

const iconProps = { size: 18, strokeWidth: 1.5, 'aria-hidden': true, className: 'shrink-0 text-muted' } as const;

/** An organization's 36 px tile in the list: ink when current, outlined otherwise. */
function OrgTile({ mark, current }: { mark: string; current: boolean }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        'box-border flex size-9 items-center justify-center rounded-[10px] text-[13px] font-semibold',
        current ? 'bg-ink text-white' : 'border border-ink bg-surface text-ink',
      )}
    >
      {mark}
    </span>
  );
}

function OrgText({ choice }: { choice: OrgChoice }) {
  return (
    <span className="min-w-0">
      <span className="block truncate text-[15px] font-medium">{choice.org.name}</span>
      <span className="block text-[13px] text-muted">{choice.detail}</span>
    </span>
  );
}

interface OrgMenuProps {
  org: OrgSummary;
  /** Opens Create organization (the owner keeps the dialog: the menu closes as it opens). */
  onCreate: () => void;
  /** Set just before choosing navigates, so the menu leaves focus to the new page's heading. */
  navigating: RefObject<boolean>;
}

/** The rail's menu (≥ 768 px): its content, beside the organization mark. */
export function OrgMenuContent({ org, onCreate, navigating }: OrgMenuProps) {
  const model = useOrgMenuModel(org);
  const navigate = useNavigate();
  const ids = useId();

  return (
    <MenuContent
      onCloseAutoFocus={(e) => {
        if (navigating.current) e.preventDefault();
        navigating.current = false;
      }}
    >
      <MenuGroup aria-labelledby={`${ids}-orgs`}>
        <MenuLabel id={`${ids}-orgs`}>Organizations</MenuLabel>
        <MenuRadioGroup value={org.id} className="flex flex-col gap-0.5">
          {model.orgs.map((choice) => (
            <MenuRadioItem
              key={choice.org.id}
              value={choice.org.id}
              onSelect={() => {
                navigating.current = model.leaves(orgPath('/o/$org', choice.org.slug));
                navigate({ to: '/o/$org', params: { org: choice.org.slug } });
              }}
            >
              <OrgTile mark={choice.mark} current={choice.current} />
              <OrgText choice={choice} />
            </MenuRadioItem>
          ))}
        </MenuRadioGroup>
      </MenuGroup>
      <MenuSeparator />
      <MenuGroup aria-labelledby={`${ids}-org`}>
        <MenuLabel id={`${ids}-org`}>{org.name}</MenuLabel>
        {model.links.map(({ screen, label }) =>
          screen.available ? (
            <MenuItem
              key={screen.id}
              asChild
              onSelect={() => {
                navigating.current = model.leaves(orgPath(screen.to, org.slug));
              }}
            >
              <Link to={screen.to} params={{ org: org.slug }}>
                <screen.icon {...iconProps} />
                {label}
              </Link>
            </MenuItem>
          ) : (
            <MenuItem key={screen.id} disabled>
              <screen.icon {...iconProps} />
              {label}
              <span className="ml-auto text-xs">{laterText}</span>
            </MenuItem>
          ),
        )}
      </MenuGroup>
      {model.canCreate ? (
        <>
          <MenuSeparator />
          <MenuItem onSelect={onCreate}>
            <Plus {...iconProps} />
            Create organization
          </MenuItem>
        </>
      ) : null}
    </MenuContent>
  );
}

const sheetRow = cn(menuRow, 'min-h-12 text-base hover:bg-shell');

interface OrgSheetProps extends OrgMenuProps {
  /** Closes the sheet; a link calls it as it navigates. */
  onClose: () => void;
}

/** The phone's sheet (< 768 px): the same groups as plain lists of links. */
export function OrgSheetContent({ org, onCreate, onClose, navigating }: OrgSheetProps) {
  const model = useOrgMenuModel(org);
  const ids = useId();
  const currentRef = useRef<HTMLAnchorElement>(null);
  const go = (path: string) => {
    navigating.current = model.leaves(path);
    onClose();
  };

  return (
    <SheetContent
      title="Switch organization"
      initialFocus={currentRef}
      onCloseAutoFocus={(e) => {
        if (navigating.current) e.preventDefault();
        navigating.current = false;
      }}
    >
      <SheetLabel id={`${ids}-orgs`}>Organizations</SheetLabel>
      <SheetList labelledBy={`${ids}-orgs`}>
        {model.orgs.map((choice) => (
          <li key={choice.org.id}>
            <Link
              ref={choice.current ? currentRef : undefined}
              to="/o/$org"
              params={{ org: choice.org.slug }}
              // The Link says "page" itself when the organization's Factories home is open.
              activeOptions={{ exact: true }}
              aria-current={choice.current ? 'true' : undefined}
              data-current={choice.current}
              onClick={() => go(orgPath('/o/$org', choice.org.slug))}
              className="grid min-h-[52px] grid-cols-[36px_minmax(0,1fr)_20px] items-center gap-3 rounded-control px-2 py-1.5 text-ink no-underline transition-colors duration-200 hover:bg-shell data-[current=true]:bg-shell"
            >
              <OrgTile mark={choice.mark} current={choice.current} />
              <OrgText choice={choice} />
              {choice.current ? (
                <>
                  <Check size={18} strokeWidth={1.75} aria-hidden="true" />
                  <span className="sr-only">, current organization</span>
                </>
              ) : null}
            </Link>
          </li>
        ))}
      </SheetList>
      <div aria-hidden="true" className="my-2 h-px bg-line" />
      <SheetLabel id={`${ids}-org`}>{org.name}</SheetLabel>
      <SheetList labelledBy={`${ids}-org`}>
        {model.links.map(({ screen, label }) =>
          screen.available ? (
            <li key={screen.id}>
              <Link
                to={screen.to}
                params={{ org: org.slug }}
                onClick={() => go(orgPath(screen.to, org.slug))}
                className={sheetRow}
              >
                <screen.icon {...iconProps} />
                {label}
              </Link>
            </li>
          ) : (
            <li key={screen.id} aria-disabled="true" className={cn(menuRow, 'min-h-12 text-base text-muted')}>
              <screen.icon {...iconProps} />
              {label}
              <span className="ml-auto text-xs">{laterText}</span>
            </li>
          ),
        )}
      </SheetList>
      <div aria-hidden="true" className="my-2 h-px bg-line" />
      {model.canCreate ? (
        <>
          <button type="button" onClick={onCreate} className={sheetRow}>
            <Plus {...iconProps} />
            Create organization
          </button>
          <div aria-hidden="true" className="my-2 h-px bg-line" />
        </>
      ) : null}
      <AccountBlock org={org} onAction={onClose} />
    </SheetContent>
  );
}

function SheetList({ labelledBy, children }: { labelledBy: string; children: ReactNode }) {
  return (
    <ul aria-labelledby={labelledBy} className="m-0 flex list-none flex-col gap-0.5 p-0">
      {children}
    </ul>
  );
}
