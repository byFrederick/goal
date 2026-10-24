// The organization mark at the top of the rail and at the start of the phone's top row. Until the
// organization menu (C4: switch organization, settings) it links to the organization's Factories
// home; C4 turns this one component into the menu's trigger (a Radix DropdownMenu on the rail, the
// organization sheet on phones) without touching the rail or the top row.
import { Link, useMatch } from '@tanstack/react-router';
import type { OrgSummary } from '../api/types';
import { orgMark } from '../shared/marks';
import { Mark } from '../ui/mark';
import { Tooltip } from '../ui/tooltip';
import { railItem, railItemClass } from './rail-items';

export function OrgMark({ org, size }: { org: OrgSummary; size: 'rail' | 'phone' }) {
  // Ringed while one of the organization's own pages (not a factory's) is open.
  const onOrgPage = useMatch({ from: '/o/$org/_org', shouldThrow: false }) !== undefined;
  const mark = orgMark(org);
  const link = (
    <Link
      to="/o/$org"
      params={{ org: org.slug }}
      activeOptions={{ exact: true }}
      aria-label={`${mark}, ${org.name} organization, factories`}
      className={railItemClass}
      {...railItem}
    >
      <Mark kind="org" size={size} current={onOrgPage}>
        {mark}
      </Mark>
    </Link>
  );
  return size === 'rail' ? (
    <Tooltip label={org.name} detail="Factories">
      {link}
    </Tooltip>
  ) : (
    link
  );
}
