// The organization menu (docs/design/screens/account/shell.html, "Organization menu"): the organizations
// the caller is in, each with its factory count and role and a check on the current one; Create
// organization for whoever may create one (platform admins, or everyone with sign-in off); and the
// current organization's pages. Members and roles shows the member count once it loads. Spend and
// billing and Models and policies have no pages yet, so they render disabled (ADR 0016, decision 4).
// A Radix DropdownMenu under the sidebar's top row (sidebar.tsx); a bottom sheet on phones, opened by
// the organization mark (org-mark.tsx). The sheet ends with the account (account-menu.tsx).
import { useQuery, useSuspenseQuery } from '@tanstack/react-query';
import { Link, useLocation, useNavigate } from '@tanstack/react-router';
import { Check, CreditCard, KeyRound, Plus, Settings, Shield, Users } from 'lucide-react';
import { type ReactNode, type RefObject, useId, useRef } from 'react';
import { membersQuery, meQuery, orgsQuery } from '../api/queries';
import type { OrgSummary } from '../api/types';
import { factoryCount, useOrgGrants } from '../shared/org';
import { cn } from '../ui/cn';
import { MenuContent, MenuGroup, MenuItem, MenuLabel, MenuSeparator, menuRow } from '../ui/menu';
import { NotAvailable } from '../ui/not-available';
import { SheetContent, SheetLabel } from '../ui/sheet';
import { AccountBlock } from './account-menu';
import { apiKeysScreen, membersScreen, screensFor, settingsScreen } from './screens';

interface OrgChoice {
  org: OrgSummary;
  /** "3 factories, owner" */
  detail: string;
  current: boolean;
}

interface OrgMenuModel {
  orgs: OrgChoice[];
  /** Members and roles is offered to whoever may read the members (member:read). */
  canMembers: boolean;
  /** API keys is offered to whoever may read keys (api_key:read or personal_token:read). */
  canApiKeys: boolean;
  /** Create organization is offered: to platform admins, or to the local user with sign-in off. */
  canCreate: boolean;
  /**
   * Whether going to `path` leaves the current page. Only then does the new page's heading take
   * focus; otherwise focus returns to the trigger as the menu closes.
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
  const can = useOrgGrants()?.can;
  return {
    orgs: data.map((org) => ({
      org,
      detail: org.role ? `${factoryCount(org.factory_count)}, ${org.role}` : factoryCount(org.factory_count),
      current: org.id === current.id,
    })),
    canMembers: can?.includes('member:read') ?? false,
    canApiKeys: screensFor([apiKeysScreen], can).length > 0,
    canCreate,
    leaves: (path) => path !== pathname,
  };
}

const iconProps = { size: 16, strokeWidth: 1.5, 'aria-hidden': true, className: 'shrink-0 text-muted' } as const;

/** A row of the menu: the hover fill of the picture (hover token), 14 px body text. */
const rowClass = cn(menuRow, 'cursor-pointer text-body hover:bg-hover data-[highlighted]:bg-hover');

/** The check on the current organization; its label is what a screen reader hears for it. */
function CurrentCheck() {
  return <Check role="img" aria-label="Current" size={16} strokeWidth={1.75} className="shrink-0 text-run" />;
}

/** The member count, from the members list. It mounts only while the menu is open and the caller may read members. */
function MembersCount({ orgId }: { orgId: string }) {
  const { data, isPending, isError } = useQuery(membersQuery(orgId));
  // A failed count leaves the number out; the row stays usable.
  if (isError) return null;
  if (isPending) {
    return (
      <span aria-busy="true" className="ml-auto text-meta text-muted">
        <span className="sr-only">Loading</span>
      </span>
    );
  }
  return <span className="ml-auto text-meta text-muted tabular-nums">{data.length}</span>;
}

function OrgText({ choice }: { choice: OrgChoice }) {
  return (
    <span className="min-w-0 flex-1">
      <span className="block truncate text-heading font-medium">{choice.org.name}</span>
      <span className="block text-secondary text-muted">{choice.detail}</span>
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

/** The sidebar's menu (≥ 768 px): its content, below the sidebar's top row. */
export function OrgMenuContent({ org, onCreate, navigating }: OrgMenuProps) {
  const model = useOrgMenuModel(org);
  const navigate = useNavigate();
  const ids = useId();

  return (
    <MenuContent
      side="bottom"
      onCloseAutoFocus={(e) => {
        if (navigating.current) e.preventDefault();
        navigating.current = false;
      }}
    >
      <MenuGroup aria-labelledby={`${ids}-orgs`}>
        <MenuLabel id={`${ids}-orgs`}>Organizations</MenuLabel>
        {model.orgs.map((choice) => (
          <MenuItem
            key={choice.org.id}
            aria-current={choice.current ? 'true' : undefined}
            className={rowClass}
            onSelect={() => {
              navigating.current = model.leaves(orgPath('/o/$org', choice.org.slug));
              navigate({ to: '/o/$org', params: { org: choice.org.slug } });
            }}
          >
            <OrgText choice={choice} />
            {choice.current ? <CurrentCheck /> : null}
          </MenuItem>
        ))}
        {model.canCreate ? (
          <MenuItem onSelect={onCreate} className={rowClass}>
            <Plus {...iconProps} />
            Create organization
          </MenuItem>
        ) : null}
      </MenuGroup>
      <MenuSeparator />
      <MenuGroup aria-labelledby={`${ids}-org`}>
        <MenuLabel id={`${ids}-org`}>{org.name}</MenuLabel>
        {model.canMembers ? (
          <MenuItem
            asChild
            className={rowClass}
            onSelect={() => {
              navigating.current = model.leaves(orgPath(membersScreen.to, org.slug));
            }}
          >
            <Link to={membersScreen.to} params={{ org: org.slug }}>
              <Users {...iconProps} />
              {membersScreen.label}
              <MembersCount orgId={org.id} />
            </Link>
          </MenuItem>
        ) : null}
        <NotAvailable>
          <MenuItem className={rowClass}>
            <CreditCard {...iconProps} />
            Spend and billing
          </MenuItem>
        </NotAvailable>
        <NotAvailable>
          <MenuItem className={rowClass}>
            <Shield {...iconProps} />
            Models and policies
          </MenuItem>
        </NotAvailable>
        {model.canApiKeys ? (
          <MenuItem
            asChild
            className={rowClass}
            onSelect={() => {
              navigating.current = model.leaves(orgPath(apiKeysScreen.to, org.slug));
            }}
          >
            <Link to={apiKeysScreen.to} params={{ org: org.slug }}>
              <KeyRound {...iconProps} />
              {apiKeysScreen.label}
            </Link>
          </MenuItem>
        ) : null}
        <MenuItem
          asChild
          className={rowClass}
          onSelect={() => {
            navigating.current = model.leaves(orgPath(settingsScreen.to, org.slug));
          }}
        >
          <Link to={settingsScreen.to} params={{ org: org.slug }}>
            <Settings {...iconProps} />
            Organization settings
          </Link>
        </MenuItem>
      </MenuGroup>
    </MenuContent>
  );
}

const sheetRow = cn(menuRow, 'min-h-13 text-body hover:bg-hover');

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
              onClick={() => go(orgPath('/o/$org', choice.org.slug))}
              className={sheetRow}
            >
              <OrgText choice={choice} />
              {choice.current ? <CurrentCheck /> : null}
            </Link>
          </li>
        ))}
        {model.canCreate ? (
          <li>
            <button type="button" onClick={onCreate} className={sheetRow}>
              <Plus {...iconProps} />
              Create organization
            </button>
          </li>
        ) : null}
      </SheetList>
      <div aria-hidden="true" className="my-2 h-px bg-line" />
      <SheetLabel id={`${ids}-org`}>{org.name}</SheetLabel>
      <SheetList labelledBy={`${ids}-org`}>
        {model.canMembers ? (
          <li>
            <Link
              to={membersScreen.to}
              params={{ org: org.slug }}
              onClick={() => go(orgPath(membersScreen.to, org.slug))}
              className={sheetRow}
            >
              <Users {...iconProps} />
              {membersScreen.label}
              <MembersCount orgId={org.id} />
            </Link>
          </li>
        ) : null}
        <li>
          <NotAvailable>
            <button type="button" className={sheetRow}>
              <CreditCard {...iconProps} />
              Spend and billing
            </button>
          </NotAvailable>
        </li>
        <li>
          <NotAvailable>
            <button type="button" className={sheetRow}>
              <Shield {...iconProps} />
              Models and policies
            </button>
          </NotAvailable>
        </li>
        {model.canApiKeys ? (
          <li>
            <Link
              to={apiKeysScreen.to}
              params={{ org: org.slug }}
              onClick={() => go(orgPath(apiKeysScreen.to, org.slug))}
              className={sheetRow}
            >
              <KeyRound {...iconProps} />
              {apiKeysScreen.label}
            </Link>
          </li>
        ) : null}
        <li>
          <Link
            to={settingsScreen.to}
            params={{ org: org.slug }}
            onClick={() => go(orgPath(settingsScreen.to, org.slug))}
            className={sheetRow}
          >
            <Settings {...iconProps} />
            Organization settings
          </Link>
        </li>
      </SheetList>
      <div aria-hidden="true" className="my-2 h-px bg-line" />
      <AccountBlock org={org} onAction={onClose} />
    </SheetContent>
  );
}

function SheetList({ labelledBy, children }: { labelledBy: string; children: ReactNode }) {
  return (
    <ul aria-labelledby={labelledBy} className="m-0 flex list-none flex-col gap-1 p-0">
      {children}
    </ul>
  );
}
