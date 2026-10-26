// The organization mark at the top of the rail and at the start of the phone's top row: the
// trigger of the organization menu (C §10.4). On the rail it opens a Radix DropdownMenu beside
// it; on phones the organization sheet. Both can open Create organization, which this component
// keeps so the dialog outlives the menu that opened it.
import { useMatch } from '@tanstack/react-router';
import { useRef, useState } from 'react';
import type { OrgSummary } from '../api/types';
import { orgMark } from '../shared/marks';
import { Mark } from '../ui/mark';
import { Menu, MenuTrigger } from '../ui/menu';
import { Sheet, SheetTrigger } from '../ui/sheet';
import { Tooltip } from '../ui/tooltip';
import { CreateOrgDialog } from './create-org-dialog';
import { OrgMenuContent, OrgSheetContent } from './org-menu';
import { keepArrowsOnRail, railItem, railItemClass } from './rail-items';

export function OrgMark({ org, size }: { org: OrgSummary; size: 'rail' | 'phone' }) {
  // Ringed while one of the organization's own pages (not a factory's) is open, or its menu.
  const onOrgPage = useMatch({ from: '/o/$org/_org', shouldThrow: false }) !== undefined;
  const [open, setOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const navigating = useRef(false);
  const mark = orgMark(org);

  const markTile = (
    <Mark kind="org" size={size} current={onOrgPage || open}>
      {mark}
    </Mark>
  );
  const triggerProps = {
    ref: trigger,
    'aria-label': `${mark}, ${org.name} organization, switch organization`,
    className: railItemClass,
    ...railItem,
  };
  const create = () => {
    setOpen(false);
    setCreating(true);
  };
  const dialog = <CreateOrgDialog open={creating} onOpenChange={setCreating} returnFocus={trigger} />;

  if (size === 'phone') {
    return (
      <>
        <Sheet open={open} onOpenChange={setOpen}>
          <SheetTrigger {...triggerProps}>{markTile}</SheetTrigger>
          <OrgSheetContent org={org} onCreate={create} onClose={() => setOpen(false)} navigating={navigating} />
        </Sheet>
        {dialog}
      </>
    );
  }

  return (
    <>
      <Menu open={open} onOpenChange={setOpen}>
        <Tooltip label={org.name} detail="Switch organization">
          <MenuTrigger {...triggerProps} onKeyDown={keepArrowsOnRail}>
            {markTile}
          </MenuTrigger>
        </Tooltip>
        <OrgMenuContent org={org} onCreate={create} navigating={navigating} />
      </Menu>
      {dialog}
    </>
  );
}
