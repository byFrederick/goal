// The Inbox on the board header (E §10.2): a button with the count of what waits on people in this
// factory, opening a drawer of the inbox's groups (Ready to merge, Needs a decision, Blocked). Each
// row is a link to its item (or the board) and offers one action. Every action refetches the inbox
// (keys.inboxes), so a handled row leaves the drawer. What a row may do comes from its own factory's
// `can`, not the factory on screen.
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { GitMerge } from 'lucide-react';
import { useId, useMemo, useState } from 'react';
import { toast } from 'sonner';
import { mergeItem } from '../api/client';
import { factoriesQuery, inboxQuery, keys } from '../api/queries';
import type { Factory, ItemCard } from '../api/types';
import { Button } from '../ui/button';
import { Count } from '../ui/count';
import { Dialog, DialogClose, DialogContent } from '../ui/dialog';
import { Drawer, DrawerContent, DrawerTrigger } from '../ui/drawer';
import { SegmentedControl, type SegmentedOption } from '../ui/segmented-control';
import { StatusIcon } from '../ui/status-icon';
import { refParam } from './factory';
import { formatAge, plural } from './format';
import { type InboxGroup, type InboxRow, inboxCount, inboxGroups } from './inbox';
import { factoryWorkInvalidations } from './invalidation';
import { type DecidableItem, MergeDialog, mergeRefusal, useDecision } from './item-actions';
import { useOrg } from './org';
import { holds, reasons } from './permissions';

type Scope = 'all' | 'factory';

const scopes: SegmentedOption<Scope>[] = [
  { value: 'all', label: 'All factories' },
  { value: 'factory', label: 'This factory' },
];

/** The Inbox button with its count, and the drawer it opens. */
export function InboxButton({ factory }: { factory: Factory }) {
  const org = useOrg();
  const [open, setOpen] = useState(false);
  const { data } = useQuery(inboxQuery(org.id, factory.id));
  const count = useMemo(() => inboxCount(inboxGroups(data?.entries ?? [])), [data]);

  return (
    <Drawer open={open} onOpenChange={setOpen}>
      <DrawerTrigger asChild>
        <Button variant="raised">
          Inbox
          {count > 0 ? <Count value={count} label=" waiting on you" tone="you" /> : null}
        </Button>
      </DrawerTrigger>
      <DrawerContent title="Inbox" description="What waits on you, grouped by what you do about it.">
        <InboxBody factory={factory} onNavigate={() => setOpen(false)} />
      </DrawerContent>
    </Drawer>
  );
}

function InboxBody({ factory, onNavigate }: { factory: Factory; onNavigate: () => void }) {
  const org = useOrg();
  const [scope, setScope] = useState<Scope>('factory');
  const [failures, setFailures] = useState<string[]>([]);
  const inbox = useQuery(inboxQuery(org.id, scope === 'factory' ? factory.id : undefined));
  const factories = useQuery(factoriesQuery(org.id)).data ?? [];
  const groups = useMemo(() => inboxGroups(inbox.data?.entries ?? []), [inbox.data]);
  const canDecide = (row: InboxRow) =>
    holds(
      factories.find((f) => f.id === row.entry.factory.id),
      'item:decide',
    );

  return (
    <>
      <SegmentedControl
        label="Which factories"
        options={scopes}
        value={scope}
        onChange={setScope}
        className="self-start"
      />
      {inbox.isError ? (
        <p role="alert" className="m-0 text-sm text-rework-ink">
          Could not load the inbox.
        </p>
      ) : null}
      {inbox.isPending && !inbox.isError ? <p className="m-0 text-sm text-muted">Loading…</p> : null}
      {inbox.data && groups.length === 0 ? <p className="m-0 text-[15px] text-muted">Nothing needs you</p> : null}
      {groups.map((group) => (
        <InboxSection
          key={group.id}
          group={group}
          canDecide={canDecide}
          onNavigate={onNavigate}
          onFailures={setFailures}
        />
      ))}
      {failures.length > 0 ? (
        <div
          role="alert"
          className="flex flex-col gap-1 rounded-control border border-rework-tint bg-rework-tint px-3 py-2.5 text-sm leading-snug text-rework-ink"
        >
          <p className="m-0 font-medium">Not merged</p>
          <ul className="m-0 flex list-none flex-col gap-1 p-0">
            {failures.map((failure) => (
              <li key={failure}>{failure}</li>
            ))}
          </ul>
        </div>
      ) : null}
    </>
  );
}

function InboxSection({
  group,
  canDecide,
  onNavigate,
  onFailures,
}: {
  group: InboxGroup;
  canDecide: (row: InboxRow) => boolean;
  onNavigate: () => void;
  onFailures: (failures: string[]) => void;
}) {
  const headingId = useId();
  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-1">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-baseline gap-2">
          <h3 id={headingId} className="m-0 text-sm font-medium">
            {group.title}
          </h3>
          <span className="text-sm text-muted tabular-nums">{group.rows.length}</span>
        </div>
        {group.id === 'ready' && group.rows.length > 1 ? (
          <MergeAll rows={group.rows} canDecide={canDecide} onFailures={onFailures} />
        ) : null}
      </div>
      <ul className="m-0 flex list-none flex-col p-0">
        {group.rows.map((row) => (
          <li key={row.key} className="border-t border-line first:border-t-0">
            <InboxRowView row={row} mayDecide={canDecide(row)} onNavigate={onNavigate} />
          </li>
        ))}
      </ul>
    </section>
  );
}

/** One row: a link to its item, its status and why it waits, its age, and its one action. */
function InboxRowView({ row, mayDecide, onNavigate }: { row: InboxRow; mayDecide: boolean; onNavigate: () => void }) {
  const org = useOrg();
  const { entry, item } = row;
  const params = { org: org.slug, factory: entry.factory.name };
  const age = formatAge(Math.max(0, (Date.now() - Date.parse(entry.since)) / 1000));
  const linkClass =
    'grid min-w-0 flex-1 grid-cols-[minmax(0,1fr)_auto] items-start gap-3 py-3 text-ink no-underline transition-colors duration-200 hover:bg-shell';
  const body = (
    <>
      <span className="flex min-w-0 flex-col gap-1">
        <span className="flex min-w-0 items-center gap-2 text-[13px] text-muted">
          <StatusIcon status={row.status} />
          {item ? (
            <span className="min-w-0 truncate" title={item.ref}>
              {item.ref}
            </span>
          ) : null}
        </span>
        <span className="line-clamp-2 text-[15px] leading-snug font-medium break-words" title={row.title}>
          {row.title}
        </span>
        <span className="min-w-0 truncate text-sm text-muted" title={row.why}>
          {row.why}
        </span>
      </span>
      <span className="text-[13px] whitespace-nowrap text-muted tabular-nums">{age}</span>
    </>
  );

  return (
    <div className="flex items-start gap-3">
      {row.to === 'item' && item ? (
        <Link
          to="/o/$org/f/$factory/items/$ref"
          params={{ ...params, ref: refParam(item.ref) }}
          onClick={onNavigate}
          className={linkClass}
        >
          {body}
        </Link>
      ) : (
        <Link to="/o/$org/f/$factory/board" params={params} onClick={onNavigate} className={linkClass}>
          {body}
        </Link>
      )}
      <div className="flex shrink-0 items-center py-2.5">
        {item && row.action !== 'open' ? (
          <DecisionButton
            item={item}
            action={row.action}
            factory={entry.factory.id}
            factoryName={entry.factory.name}
            mayDecide={mayDecide}
          />
        ) : (
          <OpenButton row={row} onNavigate={onNavigate} />
        )}
      </div>
    </div>
  );
}

/** The item as a decision sees it. */
function decidable(item: ItemCard): DecidableItem {
  return { key: item.ref, ref: item.ref, title: item.title, state: item.state, pull_requests: item.pull_requests };
}

/** Merge, approve or retry an item. Merge asks first; the others do not (as the Item page). */
function DecisionButton({
  item,
  action,
  factory,
  factoryName,
  mayDecide,
}: {
  item: ItemCard;
  action: 'merge' | 'approve' | 'retry';
  factory: string;
  factoryName: string;
  mayDecide: boolean;
}) {
  const decide = useDecision(decidable(item), factory);
  const [merging, setMerging] = useState(false);
  const reasonId = useId();
  const pending = decide.isPending ? decide.variables?.kind : undefined;
  const label = { merge: 'Merge', approve: 'Approve', retry: 'Retry' }[action];
  const busy = pending !== undefined;
  const reason = reasons.decide(factoryName);

  return (
    <>
      <Button
        variant={action === 'retry' ? 'secondary' : 'primary'}
        disabled={!mayDecide || busy}
        aria-label={`${label} ${item.ref}`}
        aria-describedby={mayDecide ? undefined : reasonId}
        title={mayDecide ? undefined : reason}
        onClick={() => {
          if (action === 'merge') setMerging(true);
          else decide.mutate({ kind: action });
        }}
      >
        {busy ? `${label}…` : label}
      </Button>
      {mayDecide ? null : (
        <span id={reasonId} className="sr-only">
          {reason}
        </span>
      )}
      {action === 'merge' ? (
        <MergeDialog
          item={decidable(item)}
          open={merging}
          onOpenChange={(open) => {
            setMerging(open);
            // A refusal said in the dialog goes with it.
            if (!open) decide.reset();
          }}
          onConfirm={() => decide.mutate({ kind: 'merge' }, { onSuccess: () => setMerging(false) })}
          pending={pending === 'merge'}
          refusal={decide.variables?.kind === 'merge' ? mergeRefusal(decide.error) : undefined}
        />
      ) : null}
    </>
  );
}

/** Open the item, or the board for what is about no item: the row's one action when it is not a decision. */
function OpenButton({ row, onNavigate }: { row: InboxRow; onNavigate: () => void }) {
  const org = useOrg();
  const navigate = useNavigate();
  const params = { org: org.slug, factory: row.entry.factory.name };
  const item = row.item;
  return (
    <Button
      aria-label={item ? `Open ${item.ref}` : `Open the ${row.entry.factory.name} board`}
      onClick={() => {
        onNavigate();
        if (row.to === 'item' && item) {
          navigate({ to: '/o/$org/f/$factory/items/$ref', params: { ...params, ref: refParam(item.ref) } });
        } else {
          navigate({ to: '/o/$org/f/$factory/board', params });
        }
      }}
    >
      Open
    </Button>
  );
}

/**
 * Merges every ready item in turn (Ready to merge with more than one row), after a confirmation that
 * names the count. goald refuses each item on its own: an item it refuses is named in the drawer and
 * the others still merge. Every merge then refetches the inbox, so the merged rows leave it.
 */
function MergeAll({
  rows,
  canDecide,
  onFailures,
}: {
  rows: InboxRow[];
  canDecide: (row: InboxRow) => boolean;
  onFailures: (failures: string[]) => void;
}) {
  const org = useOrg();
  const client = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const [pending, setPending] = useState(false);
  const items = rows.flatMap((row) => (row.item ? [{ row, item: row.item }] : []));
  const names = [...new Set(rows.filter((row) => !canDecide(row)).map((row) => row.entry.factory.name))];
  const n = items.length;

  const mergeEach = async () => {
    setPending(true);
    onFailures([]);
    const failed: string[] = [];
    let merged = 0;
    for (const { item } of items) {
      try {
        await mergeItem(org.id, item.ref);
        merged += 1;
      } catch (err) {
        const why = mergeRefusal(err) ?? (err instanceof Error ? err.message : 'goald refused it');
        failed.push(`${item.ref}: ${why}`);
      }
    }
    await Promise.all([
      client.invalidateQueries({ queryKey: keys.inboxes(org.id) }),
      client.invalidateQueries({ queryKey: keys.attention(org.id) }),
      ...items.flatMap(({ row, item }) => [
        ...factoryWorkInvalidations(org.id, row.entry.factory.id).map((queryKey) =>
          client.invalidateQueries({ queryKey }),
        ),
        client.invalidateQueries({ queryKey: keys.item(org.id, item.ref) }),
      ]),
    ]);
    setPending(false);
    setConfirming(false);
    onFailures(failed);
    if (merged > 0) toast.success(`Merged ${plural(merged, 'item')}.`);
  };

  return (
    <>
      <Button
        variant="primary"
        size="sm"
        disabled={names.length > 0 || pending}
        title={names.length > 0 ? reasons.decide(names.join(', ')) : undefined}
        onClick={() => setConfirming(true)}
      >
        Merge all
      </Button>
      <Dialog open={confirming} onOpenChange={(open) => !pending && setConfirming(open)}>
        <DialogContent
          title={`Merge ${plural(n, 'item')}?`}
          description="goald squash-merges each item's pull requests, one item after another, and approves each. An item it refuses is named in the inbox and the others still merge."
        >
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary">Cancel</Button>
            </DialogClose>
            <Button variant="primary" disabled={pending} onClick={() => void mergeEach()}>
              <GitMerge size={16} strokeWidth={1.75} aria-hidden="true" />
              {pending ? 'Merging…' : `Merge ${plural(n, 'item')}`}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
