// The header's actions (screens build/definition): Form | YAML, Versions, Review changes and
// Publish. From 1024 px up they sit in one wrapping row (the rail takes about 260 px, so main is narrower than the viewport). Below it they are one row that never
// wraps: Form | YAML, the primary action (Review, named Review changes) and a More menu holding Versions and Review changes.
import { Link } from '@tanstack/react-router';
import { Clock, Ellipsis } from 'lucide-react';
import { useSyncExternalStore } from 'react';
import { useFactoryParams } from '../../../shared/factory';
import { Button } from '../../../ui/button';
import { Menu, MenuContent, MenuItem, MenuTrigger } from '../../../ui/menu';
import type { EditorSection } from '../sections';
import { ViewToggle } from './view-toggle';

const narrowQuery = '(max-width: 1023px)';

/** Whether the viewport is below 1024 px (Tailwind's lg). False without matchMedia. */
function useNarrow(): boolean {
  return useSyncExternalStore(
    (onChange) => {
      if (typeof window.matchMedia !== 'function') return () => {};
      const query = window.matchMedia(narrowQuery);
      query.addEventListener('change', onChange);
      return () => query.removeEventListener('change', onChange);
    },
    () => typeof window.matchMedia === 'function' && window.matchMedia(narrowQuery).matches,
    () => false,
  );
}

interface PageActionsProps {
  section: EditorSection;
  /** The YAML view is open: it has its own Form | YAML toggle. */
  yaml: boolean;
  viewOnly: boolean;
  canPublish: boolean;
  publishNumber: number;
  /** Id of the sr-only sentence that says why Publish is off. */
  describedBy: string;
  /** Id of the sr-only "No changes yet", set on Review changes while nothing changed. */
  reviewDescribedBy: string | undefined;
}

export function PageActions(props: PageActionsProps) {
  const narrow = useNarrow();
  return narrow ? <NarrowActions {...props} /> : <WideActions {...props} />;
}

function WideActions({
  section,
  yaml,
  viewOnly,
  canPublish,
  publishNumber,
  describedBy,
  reviewDescribedBy,
}: PageActionsProps) {
  const params = useFactoryParams();
  return (
    <div className="flex flex-wrap items-center gap-2">
      {yaml ? null : <ViewToggle section={section} yaml={false} />}
      <Button asChild variant="white">
        <Link to="/o/$org/f/$factory/definition/$section" params={{ ...params, section: 'versions' }}>
          <Clock size={16} strokeWidth={1.5} aria-hidden="true" />
          Versions
        </Link>
      </Button>
      {viewOnly ? null : (
        <>
          <Button asChild variant="white">
            <Link
              to="/o/$org/f/$factory/definition/$section"
              params={{ ...params, section: 'review' }}
              aria-describedby={reviewDescribedBy}
            >
              Review changes
            </Link>
          </Button>
          <Publish canPublish={canPublish} number={publishNumber} describedBy={describedBy} />
        </>
      )}
    </div>
  );
}

function NarrowActions({
  section,
  yaml,
  viewOnly,
  canPublish,
  publishNumber,
  describedBy,
  reviewDescribedBy,
}: PageActionsProps) {
  const params = useFactoryParams();
  return (
    <div className="flex w-full flex-nowrap items-center justify-between gap-2">
      {yaml ? <span /> : <ViewToggle section={section} yaml={false} />}
      <div className="flex items-center gap-2">
        {viewOnly ? null : canPublish ? (
          <Publish canPublish number={publishNumber} describedBy={describedBy} />
        ) : (
          <Button asChild variant="primary" className="h-11">
            <Link
              to="/o/$org/f/$factory/definition/$section"
              params={{ ...params, section: 'review' }}
              aria-describedby={reviewDescribedBy}
              aria-label="Review changes"
            >
              Review
            </Link>
          </Button>
        )}
        <Menu>
          <MenuTrigger asChild>
            <Button variant="white" className="size-11 shrink-0 justify-center p-0" aria-label="More actions">
              <Ellipsis size={16} strokeWidth={1.5} aria-hidden="true" />
            </Button>
          </MenuTrigger>
          <MenuContent align="end" side="bottom" className="w-56">
            <MenuItem asChild>
              <Link to="/o/$org/f/$factory/definition/$section" params={{ ...params, section: 'versions' }}>
                <Clock size={16} strokeWidth={1.5} aria-hidden="true" />
                Versions
              </Link>
            </MenuItem>
            {viewOnly || !canPublish ? null : (
              <MenuItem asChild>
                <Link
                  to="/o/$org/f/$factory/definition/$section"
                  params={{ ...params, section: 'review' }}
                  aria-describedby={reviewDescribedBy}
                >
                  Review changes
                </Link>
              </MenuItem>
            )}
          </MenuContent>
        </Menu>
      </div>
    </div>
  );
}

function Publish({ canPublish, number, describedBy }: { canPublish: boolean; number: number; describedBy: string }) {
  const params = useFactoryParams();
  if (canPublish) {
    return (
      <Button asChild variant="primary">
        <Link to="/o/$org/f/$factory/definition/$section" params={{ ...params, section: 'review' }}>
          Publish v{number}
        </Link>
      </Button>
    );
  }
  return (
    <Button variant="primary" disabled aria-describedby={describedBy}>
      Publish v{number}
    </Button>
  );
}
