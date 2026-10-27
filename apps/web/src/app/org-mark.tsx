// The organization mark at the start of the phone's top row: the trigger of the organization sheet
// (C §10.4). The sheet keeps Create organization, so the dialog outlives the sheet that opened it.
// The sidebar's top row opens the same menu from its own trigger (sidebar.tsx), with the Goal mark.
import { useMatch } from '@tanstack/react-router';
import { useRef, useState } from 'react';
import type { OrgSummary } from '../api/types';
import { orgMark } from '../shared/marks';
import { Mark } from '../ui/mark';
import { Sheet, SheetTrigger } from '../ui/sheet';
import { CreateOrgDialog } from './create-org-dialog';
import { OrgSheetContent } from './org-menu';
import { railItem, railItemClass } from './rail-items';

export function OrgMark({ org }: { org: OrgSummary }) {
  // Ringed while one of the organization's own pages (not a factory's) is open, or its sheet.
  const onOrgPage = useMatch({ from: '/o/$org/_org', shouldThrow: false }) !== undefined;
  const [open, setOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const navigating = useRef(false);
  const mark = orgMark(org);

  return (
    <>
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetTrigger
          ref={trigger}
          aria-label={`${mark}, ${org.name} organization, switch organization`}
          className={railItemClass}
          {...railItem}
        >
          <Mark kind="org" current={onOrgPage || open}>
            {mark}
          </Mark>
        </SheetTrigger>
        <OrgSheetContent
          org={org}
          onCreate={() => {
            setOpen(false);
            setCreating(true);
          }}
          onClose={() => setOpen(false)}
          navigating={navigating}
        />
      </Sheet>
      <CreateOrgDialog open={creating} onOpenChange={setCreating} returnFocus={trigger} />
    </>
  );
}
