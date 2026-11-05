// The item page's Follow-ups and Notes from other items (ADR 0040 decisions 5 to 9). Follow-ups lists what the
// item's agents wrote and where each went: a note on another item, a new item of its own (another factory's
// included) or the triage item. A waiting one has Drop; a dropped one, or one whose triage item is closed, has
// File, which opens New item with its text. Notes lists what other items' agents and triggers' checks left here.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { CornerDownRight } from 'lucide-react';
import type { ReactNode } from 'react';
import { toast } from 'sonner';
import { ApiError, dropFollowUp } from '../../api/client';
import { itemQuery } from '../../api/queries';
import type { FollowUp, FollowUpStart, Item, ItemNoteFrom } from '../../api/types';
import { useFactory } from '../../shared/factory';
import { formatStart, plural } from '../../shared/format';
import { NewItemButton } from '../../shared/new-item';
import { useOrg } from '../../shared/org';
import { holds } from '../../shared/permissions';
import { RefLink } from '../../shared/ref-link';
import { Button } from '../../ui/button';
import { SectionCard } from '../../ui/card';
import { InfoTip } from '../../ui/info-tip';

/** The title a filed item starts with: a follow-up's text, cut to the title's 200 characters. */
function titleOf(text: string): string {
  return text.length > 200 ? `${text.slice(0, 197)}…` : text;
}

/** When a start follow-up's item may start, as the agent wrote it: ", starts 14 days later", ", then waits for goal#2". */
function startWords(start: FollowUpStart | undefined): string {
  if (!start) return '';
  const parts: string[] = [];
  if (start.at) {
    const when = formatStart(start.at);
    parts.push(`starts ${when.day}, ${when.time}`);
  }
  if (start.in) parts.push(`starts ${start.in} later`);
  if (start.after && start.after.length > 0) parts.push(`then waits for ${start.after.join(', ')}`);
  return parts.length > 0 ? `, ${parts.join(', ')}` : '';
}

/** Where a follow-up went, or will go, in a sentence with its refs as links. */
function Where({ followUp, source }: { followUp: FollowUp; source: string }) {
  const filed = followUp.filed_item ? <RefLink item={followUp.filed_item} /> : null;
  const sourceLink = <RefLink item={source} />;
  switch (followUp.state) {
    case 'dropped':
      return <>Dropped{followUp.reason ? `: ${followUp.reason}` : ''}</>;
    case 'delivered':
      return <>Note on {followUp.for ? <RefLink item={followUp.for} /> : 'another item'}, delivered</>;
    case 'filed':
      return followUp.target === 'triage' ? (
        <>In the triage item {filed ?? 'for the architect'}</>
      ) : (
        <>
          Filed as {filed ?? 'a new item'}
          {startWords(followUp.start)}
        </>
      );
    default:
      if (followUp.target === 'note') {
        return <>Note on {followUp.for ? <RefLink item={followUp.for} /> : 'another item'}</>;
      }
      if (followUp.target === 'start') {
        return (
          <>
            Filed{followUp.start?.factory ? ` in ${followUp.start.factory}` : ''} when {sourceLink} lands
            {startWords(followUp.start)}
          </>
        );
      }
      return (
        <>
          For the architect when {sourceLink} lands
          {followUp.reason ? `: ${followUp.reason}` : ''}
        </>
      );
  }
}

export function FollowUpsCard({ item }: { item: Item }) {
  const followUps = item.follow_ups ?? [];
  if (followUps.length === 0) return null;
  return (
    <SectionCard
      title={
        <span className="inline-flex items-center gap-1">
          Follow-ups
          <InfoTip about="follow-ups">
            Written by this item's agents. A note goes to the item it names now; the rest are filed when this item
            lands. Drop one before then if it is wrong.
          </InfoTip>
        </span>
      }
      aside={<span className="text-secondary text-muted tabular-nums">{plural(followUps.length, 'follow-up')}</span>}
    >
      <ul className="m-0 flex list-none flex-col p-0">
        {followUps.map((followUp) => (
          <FollowUpRow key={followUp.id} item={item} followUp={followUp} />
        ))}
      </ul>
    </SectionCard>
  );
}

function FollowUpRow({ item, followUp }: { item: Item; followUp: FollowUp }) {
  const org = useOrg();
  const factory = useFactory();
  const client = useQueryClient();
  const canOperate = holds(factory, 'item:operate');
  const drop = useMutation({
    mutationFn: () => dropFollowUp(org.id, item.id, followUp.id),
    onSuccess: () => client.invalidateQueries({ queryKey: ['org', org.id, 'item'] }),
    onError: (err) =>
      toast.error('Could not drop the follow-up', { description: err instanceof ApiError ? err.message : '' }),
  });
  // A follow-up in a triage item can be filed once a person closed that item.
  const triage = useQuery({
    ...itemQuery(org.id, followUp.filed_item_id ?? ''),
    enabled: followUp.target === 'triage' && followUp.state === 'filed' && Boolean(followUp.filed_item_id),
  });
  const closedTriage = triage.data?.state === 'done' || triage.data?.state === 'cancelled';
  const fileable = followUp.state === 'dropped' || closedTriage;

  let action: ReactNode = null;
  if (followUp.state === 'waiting' && canOperate) {
    action = (
      <Button
        variant="secondary"
        size="sm"
        disabled={drop.isPending}
        onClick={() => drop.mutate()}
        aria-label={`Drop: ${followUp.text}`}
      >
        Drop
      </Button>
    );
  } else if (fileable) {
    action = (
      <NewItemButton
        factory={factory}
        title={titleOf(followUp.text)}
        {...(followUp.text.length > 200 ? { body: followUp.text } : {})}
        file={{ itemId: item.id, followUpId: followUp.id }}
        trigger={
          <Button variant="secondary" size="sm" aria-label={`File: ${followUp.text}`}>
            File
          </Button>
        }
      />
    );
  }

  return (
    <li className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-1 border-t border-line py-3 first:border-t-0 first:pt-0 last:pb-0">
      <span className="min-w-0 text-body break-words">{followUp.text}</span>
      <span className="row-span-2 flex items-center">{action}</span>
      <span className="flex min-w-0 items-center gap-2 text-secondary text-muted">
        <CornerDownRight size={12} strokeWidth={1.75} aria-hidden="true" className="flex-none" />
        <span key={followUp.state} className="min-w-0 break-words motion-safe:animate-text-states-swap">
          <Where followUp={followUp} source={item.ref} />
        </span>
      </span>
    </li>
  );
}

export function NotesCard({ notes }: { notes: readonly ItemNoteFrom[] }) {
  if (notes.length === 0) return null;
  return (
    <SectionCard
      title={
        <span className="inline-flex items-center gap-1">
          Notes from other items
          <InfoTip about="notes from other items">
            Agents' notes from other items, and the notes a trigger's check left. They reach this item's next run, after
            its brief, and are not a person's touch.
          </InfoTip>
        </span>
      }
      aside={<span className="text-secondary text-muted tabular-nums">{notes.length}</span>}
    >
      <ul className="m-0 flex list-none flex-col p-0">
        {notes.map((note) => (
          <li
            key={note.id}
            className="flex flex-col gap-1 border-t border-line py-3 first:border-t-0 first:pt-0 last:pb-0"
          >
            <span className="text-body whitespace-pre-wrap break-words">{note.text}</span>
            <span className="flex min-w-0 items-center gap-2 text-secondary text-muted">
              <CornerDownRight size={12} strokeWidth={1.75} aria-hidden="true" className="flex-none" />
              <span className="min-w-0 break-words">
                {note.from_item ? (
                  <>
                    From <RefLink item={note.from_item} />, {note.agent}
                    {note.station ? `, ${note.station}` : ''}
                  </>
                ) : (
                  <>
                    From the check of {note.trigger ?? 'a trigger'}, {note.agent}
                  </>
                )}
              </span>
            </span>
          </li>
        ))}
      </ul>
    </SectionCard>
  );
}
