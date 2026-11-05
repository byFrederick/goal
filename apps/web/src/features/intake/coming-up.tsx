// Coming up (Intake, ADR 0040 decision 4): the items that have not started, soonest first, each with its
// start or what it waits for, where it came from and Start now. A row that is started leaves the list
// (MOTION.md, A rule leaves a list); a new one fades in (A new item appears in a list).
import { useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { CornerDownRight, Ellipsis } from 'lucide-react';
import { intakeKeys } from '../../api/queries';
import type { IntakeComingUp } from '../../api/types';
import { refParam, useFactory, useFactoryParams } from '../../shared/factory';
import { formatStart, plural } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { CameFrom } from '../../shared/origin';
import { StartNowButton, useStartNow } from '../../shared/start-now';
import { useArrivals } from '../../ui/arrivals';
import { Button } from '../../ui/button';
import { cn } from '../../ui/cn';
import { Leaving, useLeaving } from '../../ui/leaving';
import { Menu, MenuContent, MenuItem, MenuTrigger } from '../../ui/menu';
import { Panel, rowClass } from './panel';

const info =
  'Items that have not started: they wait in the inbox for their time, or for the items they name. Start now releases one on the next tick.';

export function ComingUp({ items, total }: { items: readonly IntakeComingUp[]; total: number }) {
  const arrived = useArrivals(items.map((item) => item.id));
  return (
    <Panel title="Coming up" info={info} aside={<span className="tabular-nums">{total}</span>}>
      {items.length === 0 ? (
        <p className="m-0 border-t border-line px-5 py-4 text-body text-muted">
          Nothing is waiting to start. Items filed with a start time or after other items show here.
        </p>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          {items.map((item) => (
            <ComingUpRow key={item.id} item={item} arrived={arrived(item.id)} />
          ))}
        </ul>
      )}
    </Panel>
  );
}

function ComingUpRow({ item, arrived }: { item: IntakeComingUp; arrived: boolean }) {
  const org = useOrg();
  const factory = useFactory();
  const params = useFactoryParams();
  const client = useQueryClient();
  const { leaving, leave } = useLeaving(
    () => void client.invalidateQueries({ queryKey: intakeKeys.root(org.id, factory.id) }),
  );
  const when = item.start ? formatStart(item.start.at) : null;
  return (
    <li className={cn('border-t border-line first:border-t-0', arrived && 'motion-safe:animate-item-in')}>
      <Leaving leaving={leaving}>
        <div className={cn(rowClass, 'border-t-0 max-md:items-start')}>
          <span className="flex flex-col text-body font-medium tabular-nums md:col-span-1">
            {when ? (
              <>
                {when.day}
                <span className="text-secondary font-normal text-muted">{when.time}</span>
              </>
            ) : (
              <>
                Waiting
                <span className="text-secondary font-normal text-muted">
                  for {item.needs.length > 0 ? item.needs.join(', ') : 'its needs'}
                </span>
              </>
            )}
          </span>
          <span className="flex min-w-0 flex-col max-md:col-start-1">
            <Link
              to="/o/$org/f/$factory/items/$ref"
              params={{ ...params, ref: refParam(item.ref) }}
              className="truncate font-medium text-ink no-underline hover:text-run-ink"
              title={item.title}
            >
              {item.title}
            </Link>
            <span className="flex min-w-0 items-center gap-2 text-secondary text-muted">
              <CornerDownRight size={12} strokeWidth={1.75} aria-hidden="true" className="flex-none" />
              <span className="min-w-0 break-words">
                <CameFrom item={item} />
                {when && item.needs.length > 0 ? `, then waits for ${plural(item.needs.length, 'open item')}` : null}
              </span>
            </span>
          </span>
          <span className="flex items-center gap-2 max-md:col-start-2 max-md:row-span-2 max-md:row-start-1">
            {item.start ? (
              <>
                <StartNowButton
                  itemId={item.id}
                  factoryId={factory.id}
                  label={item.title}
                  onStarted={leave}
                  className="max-md:hidden"
                />
                <StartNowMenu itemId={item.id} factoryId={factory.id} title={item.title} onStarted={leave} />
              </>
            ) : null}
          </span>
        </div>
      </Leaving>
    </li>
  );
}

/** On phones Start now sits in a 44 px ⋯ menu instead of beside the row (MOTION.md, Menu, select, popover). */
function StartNowMenu({
  itemId,
  factoryId,
  title,
  onStarted,
}: {
  itemId: string;
  factoryId: string;
  title: string;
  onStarted: () => void;
}) {
  const { done, pending, start } = useStartNow(itemId, factoryId, onStarted);
  return (
    <Menu>
      <MenuTrigger asChild>
        <Button variant="ghost" size="icon" aria-label={`More actions: ${title}`} className="max-md:size-11 md:hidden">
          <Ellipsis size={18} strokeWidth={1.75} aria-hidden="true" />
        </Button>
      </MenuTrigger>
      <MenuContent align="end" side="bottom" className="w-48">
        <MenuItem disabled={pending || done} onSelect={start}>
          Start now
        </MenuItem>
      </MenuContent>
    </Menu>
  );
}
