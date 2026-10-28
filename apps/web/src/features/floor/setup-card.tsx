// "Finish setting up" on the Floor (ready.jpg, ADR 0032 decision 2): the nine rows of the end of onboarding,
// each done or to do from the data the factory has (setup-progress.ts). A to-do row links to the page
// that does it. Closing the card is remembered per browser (localStorage), and the card is gone once all
// nine rows are done. Its states: loading (sr-only, no shape), error (with Retry) and ready.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Check, X } from 'lucide-react';
import { useState } from 'react';
import {
  connectionsQuery,
  intakeQuery,
  invitationsQuery,
  itemsQuery,
  knowledgeSourcesQuery,
  membersQuery,
  versionModelQuery,
} from '../../api/queries';
import type { Factory } from '../../api/types';
import { modelOf } from '../../shared/definition/model';
import { useFactory, useFactoryParams } from '../../shared/factory';
import { NewItemButton } from '../../shared/new-item';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { cn } from '../../ui/cn';
import { CardFailed, CardLoading } from './card-states';
import { doneCount, type SetupRow, type SetupRowId, setupRows, setupTotal } from './setup-progress';

/** The key the checklist's closed state is stored under, per factory and per browser. */
export const setupHiddenKey = (factoryId: string) => `goal.setup-hidden.${factoryId}`;

function readHidden(key: string): boolean {
  try {
    return window.localStorage.getItem(key) === '1';
  } catch {
    // Storage can be off (private windows, a full quota): the card then shows until it is closed again.
    return false;
  }
}

function writeHidden(key: string) {
  try {
    window.localStorage.setItem(key, '1');
  } catch {
    // Nothing to do: the card hides for this visit only.
  }
}

const card = 'flex min-w-0 flex-col rounded-card border border-line bg-card p-4 pb-2 md:p-5 md:pb-2';
const heading = 'm-0 text-heading font-semibold text-ink';

export function SetupCard() {
  const org = useOrg();
  const factory = useFactory();
  const key = setupHiddenKey(factory.id);
  const [hidden, setHidden] = useState(() => readHidden(key));

  const members = useQuery(membersQuery(org.id));
  const invitations = useQuery(invitationsQuery(org.id));
  const connections = useQuery(connectionsQuery(org.id));
  const knowledge = useQuery(knowledgeSourcesQuery(org.id));
  const model = useQuery({ ...versionModelQuery(org.id, factory.id, 'current'), select: (v) => modelOf(v.model) });
  const intake = useQuery(intakeQuery(org.id, factory.id));
  const finished = useQuery(itemsQuery(org.id, factory.id, { state: 'done', limit: 1 }));
  const queries = [members, invitations, connections, knowledge, model, intake, finished];

  if (hidden) return null;
  if (queries.some((query) => query.isError)) {
    const retry = () => {
      for (const query of queries) if (query.isError) void query.refetch();
    };
    return (
      <section aria-labelledby="setup-h" className={card}>
        <h2 id="setup-h" className={heading}>
          Finish setting up
        </h2>
        <div className="mt-3">
          <CardFailed what="the setup checklist" onRetry={retry} />
        </div>
      </section>
    );
  }
  if (queries.some((query) => query.isPending) || !model.data || !intake.data) {
    return (
      <div aria-busy="true">
        <CardLoading />
      </div>
    );
  }

  const rows = setupRows({
    members: members.data ?? [],
    invitations: invitations.data ?? [],
    connections: connections.data ?? [],
    knowledge: knowledge.data ?? [],
    model: model.data,
    triggers: intake.data.triggers,
    finishedItems: finished.data?.items.length ?? 0,
    factoryName: factory.name,
  });
  const done = doneCount(rows);
  if (done === setupTotal) return null;

  return (
    <section aria-labelledby="setup-h" className={card}>
      <div className="mb-2 flex items-center gap-3">
        <Ring done={done} />
        <div className="flex min-w-0 flex-1 flex-col">
          <h2 id="setup-h" className={heading}>
            Finish setting up
          </h2>
          <p className="m-0 text-meta text-muted">
            {done} of {setupTotal} done
          </p>
        </div>
        <Button
          variant="ghost"
          size="icon"
          aria-label="Hide setup"
          onClick={() => {
            writeHidden(key);
            setHidden(true);
          }}
        >
          <X size={14} strokeWidth={1.75} aria-hidden="true" />
        </Button>
      </div>
      <ul className="m-0 flex list-none flex-col p-0">
        {rows.map((row) => (
          <li key={row.id}>
            <SetupLine row={row} factory={factory} />
          </li>
        ))}
      </ul>
    </section>
  );
}

/** The progress ring: a track and the done share of it, labelled "N of 9 done". */
function Ring({ done }: { done: number }) {
  const radius = 16;
  const length = 2 * Math.PI * radius;
  return (
    <svg
      width="40"
      height="40"
      viewBox="0 0 40 40"
      role="img"
      aria-label={`${done} of ${setupTotal} done`}
      className="shrink-0"
    >
      <circle cx="20" cy="20" r={radius} fill="none" strokeWidth={5} className="stroke-hover" />
      <circle
        cx="20"
        cy="20"
        r={radius}
        fill="none"
        strokeWidth={5}
        strokeLinecap="round"
        className="stroke-run"
        strokeDasharray={`${(length * done) / setupTotal} ${length}`}
        transform="rotate(-90 20 20)"
      />
    </svg>
  );
}

/** One row: the tick (or the dashed to-do circle), its text and, when it is to do, its action. */
function SetupLine({ row, factory }: { row: SetupRow; factory: Factory }) {
  return (
    <div className="flex items-center gap-3 border-t border-line py-2 text-body">
      <span
        className={cn(
          'flex size-5.5 shrink-0 items-center justify-center rounded-full',
          row.done ? 'bg-ok-tint text-ok-ink' : 'border-2 border-dashed border-rule',
        )}
      >
        {row.done ? <Check size={12} strokeWidth={2.2} aria-hidden="true" /> : null}
        <span className="sr-only">{row.done ? 'Done' : 'To do'}</span>
      </span>
      <span className="flex min-w-0 flex-1 flex-col">
        <span className={cn(row.done ? 'text-ink-soft' : 'font-semibold text-ink')}>{row.text}</span>
        {!row.done && row.hint ? <span className="text-meta text-muted">{row.hint}</span> : null}
      </span>
      {row.done ? null : <SetupAction id={row.id} factory={factory} />}
    </div>
  );
}

/** A to-do row's button: the page that does it. Connect Slack opens the add-connection page for slack. */
function SetupAction({ id, factory }: { id: SetupRowId; factory: Factory }) {
  const { org, factory: ref } = useFactoryParams();
  const orgSlug = useOrg().slug;
  switch (id) {
    case 'invite':
      return <ActionLink to="/o/$org/members" params={{ org: orgSlug }} label="Invite" />;
    case 'slack':
      return (
        <ActionLink to="/o/$org/connections/new" params={{ org: orgSlug }} search={{ kind: 'slack' }} label="Connect" />
      );
    case 'github':
      return (
        <ActionLink
          to="/o/$org/connections/new"
          params={{ org: orgSlug }}
          search={{ kind: 'github' }}
          label="Connect"
        />
      );
    case 'knowledge':
      return <ActionLink to="/o/$org/knowledge/new" params={{ org: orgSlug }} label="Add" />;
    case 'line':
      return (
        <ActionLink
          to="/o/$org/f/$factory/definition/$section"
          params={{ org, factory: ref, section: 'line' }}
          label="Open"
        />
      );
    case 'budget':
      return (
        <ActionLink
          to="/o/$org/f/$factory/definition/$section"
          params={{ org, factory: ref, section: 'review' }}
          label="Set"
        />
      );
    case 'triggers':
      return <ActionLink to="/o/$org/f/$factory/intake" params={{ org, factory: ref }} label="Open" />;
    case 'first-item':
      return <NewItemButton factory={factory} trigger={<Button size="sm">New item</Button>} />;
    case 'organization':
      return null;
  }
}

/** A secondary small button that links to a page; the row's action. */
function ActionLink(props: {
  to: string;
  params: Record<string, string>;
  search?: Record<string, string>;
  label: string;
}) {
  return (
    <Button asChild size="sm" variant="secondary">
      <Link to={props.to} params={props.params} search={props.search as never}>
        {props.label}
      </Link>
    </Button>
  );
}
