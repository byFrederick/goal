// The Items page's saved views in its nav (ADR 0020): the shared views, then the person's own, each a link
// to the page with the view's filters in its URL. A view a person may change (their own, or any shared one
// when they hold factory:manage) has a menu with Rename and Delete. Renaming uses the Save view dialog;
// deleting asks first.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Ellipsis } from 'lucide-react';
import { useState } from 'react';
import { deleteSavedView } from '../../api/client';
import { keys } from '../../api/queries';
import type { Factory, OrgSummary, SavedView, SavedViewFilters } from '../../api/types';
import { useFactoryParams } from '../../shared/factory';
import { holds } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { cn } from '../../ui/cn';
import { ConfirmDialog } from '../../ui/confirm-dialog';
import { Menu, MenuContent, MenuItem, MenuTrigger } from '../../ui/menu';
import { filtersKey, searchOf } from './items-filters';
import { failureOf, SaveViewDialog } from './save-view-dialog';

/** A tab of the nav: 44 px on a phone. Shared with the page's built-in tabs. */
export const tabLink =
  'inline-flex min-h-11 items-center gap-2 border-b-2 px-3 text-body no-underline transition-colors md:min-h-9';

interface RowProps {
  org: OrgSummary;
  factory: Factory;
  view: SavedView;
  current: boolean;
}

/** The view's link, and its menu when the person may rename or delete it. */
function SavedViewRow({ org, factory, view, current }: RowProps) {
  const params = useFactoryParams();
  const [renaming, setRenaming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  // A shared view needs factory:manage to change; a personal one is the caller's own.
  const manageable = view.shared ? holds(factory, 'factory:manage') : view.mine;
  const aria = current ? 'page' : 'false';

  return (
    <div className="flex items-center">
      <Link
        to="/o/$org/f/$factory/items"
        params={params}
        search={searchOf(view.filters)}
        activeProps={{ 'aria-current': aria }}
        inactiveProps={{ 'aria-current': aria }}
        className={cn(
          tabLink,
          '-mb-px',
          current ? 'border-run font-semibold text-ink' : 'border-transparent text-ink-soft hover:text-ink',
        )}
      >
        {view.name}
      </Link>

      {manageable ? (
        <Menu>
          <MenuTrigger asChild>
            {/* 44 px on a phone, 28 px on desktop; the Button's touch area covers the phone's target. */}
            <Button variant="white" size="icon" className="size-11 md:size-7" aria-label={`Actions for ${view.name}`}>
              <Ellipsis size={18} strokeWidth={1.5} aria-hidden="true" />
            </Button>
          </MenuTrigger>
          <MenuContent side="bottom" align="end" className="w-56">
            <MenuItem onSelect={() => setRenaming(true)}>Rename…</MenuItem>
            <MenuItem onSelect={() => setDeleting(true)}>Delete…</MenuItem>
          </MenuContent>
        </Menu>
      ) : null}

      {renaming ? (
        <SaveViewDialog
          org={org}
          factory={factory}
          view={view}
          filters={view.filters}
          onClose={() => setRenaming(false)}
        />
      ) : null}
      {deleting ? (
        <DeleteViewDialog org={org} factory={factory} view={view} onClose={() => setDeleting(false)} />
      ) : null}
    </div>
  );
}

/** Asks before a view is deleted. A refusal (view-not-found, forbidden) stays in the dialog. */
function DeleteViewDialog({ org, factory, view, onClose }: Omit<RowProps, 'current'> & { onClose: () => void }) {
  const queryClient = useQueryClient();
  const remove = useMutation({
    mutationFn: () => deleteSavedView(org.id, factory.id, view.id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: keys.views(org.id, factory.id) });
      onClose();
    },
  });

  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => (open ? undefined : onClose())}
      title={`Delete “${view.name}”?`}
      description={view.shared ? 'Everyone in the factory loses this view.' : 'Only you lose this view.'}
      confirmLabel="Delete"
      pendingLabel="Deleting"
      tone="danger"
      pending={remove.isPending}
      onConfirm={() => remove.mutate()}
    >
      {remove.isError ? (
        <div aria-live="polite">
          <Callout tone="rework">{failureOf(remove.error, 'Could not delete the view. Try again.')}</Callout>
        </div>
      ) : null}
    </ConfirmDialog>
  );
}

interface SavedViewLinksProps {
  org: OrgSummary;
  factory: Factory;
  /** The factory's views as the API lists them: shared first, then the caller's own. */
  views: SavedView[];
  /** The page's filters as the URL has them now, for the current view. */
  currentFilters: SavedViewFilters;
  /** A built-in tab is the current one, so no saved view is: the built-in tab wins when filters coincide. */
  builtInCurrent: boolean;
  failed: boolean;
  onRetry: () => void;
}

/** The saved views after the built-in tabs: nothing for an empty list, one line with Try again on an error. */
export function SavedViewLinks({
  org,
  factory,
  views,
  currentFilters,
  builtInCurrent,
  failed,
  onRetry,
}: SavedViewLinksProps) {
  const shared = views.filter((v) => v.shared);
  const own = views.filter((v) => !v.shared && v.mine);
  const currentKey = filtersKey(currentFilters);

  return (
    <>
      {[...shared, ...own].map((view) => (
        <SavedViewRow
          key={view.id}
          org={org}
          factory={factory}
          view={view}
          current={!builtInCurrent && filtersKey(view.filters) === currentKey}
        />
      ))}
      {failed ? (
        <span role="alert" className="inline-flex items-center gap-2 self-center text-secondary text-ink-soft">
          Could not load the saved views.
          <Button variant="secondary" onClick={onRetry}>
            Try again
          </Button>
        </span>
      ) : null}
    </>
  );
}
