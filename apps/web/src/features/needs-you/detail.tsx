// The detail of the selected entry: a Decision shows its approval brief, the item's recent events and
// the action bar (Approve, Edit then approve, Deny); an Approval shows the tool call it asks to make,
// the same action bar and Always allow when offered (ADR 0031); a Question shows the item page's
// question card, with its quick answers and Write an answer; an Alert shows what pulled it or blocked
// it, and links to the item or the factory's Floor. The phone's approve view is the same content in
// one column (mobile/approve).
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import type { ReactNode } from 'react';
import { eventsQuery } from '../../api/queries';
import type { Factory, InboxEntry, ToolApproval } from '../../api/types';
import { formatAge, formatClock, formatPercent, formatUsd, humanize } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { NotAvailable } from '../../ui/not-available';
import { QuestionCard } from '../items/question-card';
import { decisionOf } from './actions';
import { ageOf, approvalTitle } from './rows';
import { typeLabel, typeOf } from './types';

export interface DetailProps {
  entry: InboxEntry;
  /** The entry's factory as the factories list has it, for the question card's answers; else the entry's own. */
  factory?: Factory | undefined;
  /** The phone's approve view: one column, with the actions stacked under the brief. */
  phone?: boolean;
  pending: boolean;
  /** Whether the Always allow checkbox is offered for an Approval (the API offers it and the caller may manage connections). */
  allowAlways: boolean;
  alwaysAllow: boolean;
  onAlwaysAllow: (checked: boolean) => void;
  onApprove: () => void;
  onDeny: () => void;
}

/** The selected entry's detail; an Approval gets the tool call and Approve and Deny, a Decision Approve and Deny, a Question its card, an Alert its facts and links. */
export function Detail({
  entry,
  factory,
  phone = false,
  pending,
  allowAlways,
  alwaysAllow,
  onAlwaysAllow,
  onApprove,
  onDeny,
}: DetailProps) {
  const type = typeOf(entry);
  const station = humanize(entry.station ?? entry.item?.station ?? '');
  const age = formatAge(ageOf(entry));
  return (
    <article className="flex min-w-0 flex-col gap-5 p-4 md:p-7" aria-labelledby="detail-title">
      <header className="flex flex-col gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <Chip tone={type === 'decision' || type === 'question' ? 'you' : 'rework'} size="lg">
            {typeLabel[type]}
          </Chip>
          <Crumb entry={entry} station={station} />
        </div>
        <h2 id="detail-title" className="m-0 text-detail-title font-semibold text-ink">
          {entry.tool_approval ? approvalTitle(entry.tool_approval) : entry.item ? entry.item.title : kindTitle(entry)}
        </h2>
        {/* The question card says how long ago the agent asked, so a question has no line of its own here. */}
        {type === 'question' ? null : (
          <p className="m-0 text-secondary text-muted">
            {type === 'decision' || type === 'approval'
              ? `Asked ${age} ago. The run is paused until you answer.`
              : `${age} ago. ${alertSummary(entry, station)}`}
          </p>
        )}
      </header>

      {type === 'approval' && entry.tool_approval ? (
        <>
          <ToolCall call={entry.tool_approval} />
          <ActionBar
            entry={entry}
            approval
            phone={phone}
            pending={pending}
            allowAlways={allowAlways}
            alwaysAllow={alwaysAllow}
            onAlwaysAllow={onAlwaysAllow}
            onApprove={onApprove}
            onDeny={onDeny}
          />
        </>
      ) : type === 'decision' ? (
        <>
          <Brief entry={entry} />
          <ActionBar entry={entry} phone={phone} pending={pending} onApprove={onApprove} onDeny={onDeny} />
        </>
      ) : type === 'question' ? (
        entry.item && entry.question ? (
          <QuestionCard itemKey={entry.item.id} factory={factory ?? entry.factory} question={entry.question} />
        ) : null
      ) : (
        <>
          <AlertFacts entry={entry} station={station} />
          <AlertLinks entry={entry} />
        </>
      )}

      {entry.item ? <RecentEvents itemId={entry.item.id} /> : null}
    </article>
  );
}

/** The crumb: factory / item / station, the factory and the item as links. */
function Crumb({ entry, station }: { entry: InboxEntry; station: string }) {
  const org = useOrg();
  const factory = entry.factory.name;
  return (
    <nav aria-label="Where it waits" className="flex min-w-0 flex-wrap items-center gap-2 text-secondary text-ink-soft">
      <Link to="/o/$org/f/$factory" params={{ org: org.slug, factory }} className="text-ink-soft hover:text-ink">
        {entry.factory.title ?? factory}
      </Link>
      {entry.item ? (
        <>
          <span aria-hidden="true">/</span>
          <Link
            to="/o/$org/f/$factory/items/$ref"
            params={{ org: org.slug, factory, ref: entry.item.ref }}
            className="text-ink-soft hover:text-ink"
          >
            {entry.item.ref}
          </Link>
        </>
      ) : null}
      {station ? (
        <>
          <span aria-hidden="true">/</span>
          <span>{station}</span>
        </>
      ) : null}
    </nav>
  );
}

function kindTitle(entry: InboxEntry): string {
  if (entry.kind === 'andon') return 'Line stopped';
  if (entry.kind === 'rework_warning' && entry.rework) {
    return `${humanize(entry.rework.station)} sent back ${entry.rework.rejected} of the last ${entry.rework.verdicts}`;
  }
  return humanize(entry.kind);
}

function alertSummary(entry: InboxEntry, station: string): string {
  if (entry.kind === 'andon') return entry.andon?.reason ?? 'The andon cord is pulled.';
  if (entry.kind === 'blocked') {
    return entry.blocked?.reason ?? (entry.blocked?.paused ? 'A person paused it.' : 'It is blocked.');
  }
  if (entry.kind === 'rework_warning') return `Rework is rising at ${station || 'its gate'}.`;
  return 'It waits in the expedite lane.';
}

/** One labelled line of facts: the label in a fixed column, the value beside it. */
function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex gap-4 text-secondary">
      <dt className="w-30 flex-none text-muted">{label}</dt>
      <dd className="m-0 min-w-0 text-ink">{children}</dd>
    </div>
  );
}

/** The approval brief: the summary, the facts, the pull request and the spend. */
function Brief({ entry }: { entry: InboxEntry }) {
  const brief = entry.approval;
  if (!brief) return null;
  const pr = brief.pull_request && /^https?:\/\//.test(brief.pull_request) ? brief.pull_request : null;
  return (
    <section
      aria-label="Approval brief"
      className="flex flex-col gap-4 rounded-card border border-line bg-card p-4 md:p-5"
    >
      {brief.summary ? <p className="m-0 text-body text-ink">{brief.summary}</p> : null}
      <dl className="m-0 flex flex-col gap-2">
        {brief.facts.map((fact) => (
          <Fact key={fact.key} label={humanize(fact.key)}>
            {fact.value}
          </Fact>
        ))}
        <Fact label="Pull request">
          {pr ? (
            <a href={pr} target="_blank" rel="noreferrer" className="text-run hover:text-run-ink">
              Open pull request
            </a>
          ) : (
            'None yet'
          )}
        </Fact>
        <Fact label="Spend">{formatUsd(brief.spend_usd)}</Fact>
      </dl>
    </section>
  );
}

/** The tool call an Approval asks to make: its input as canonical JSON, then its tool and connection. */
function ToolCall({ call }: { call: ToolApproval }) {
  return (
    <>
      <section aria-labelledby="tool-call-h" className="flex flex-col gap-2">
        <h3 id="tool-call-h" className="m-0 text-secondary font-semibold text-ink">
          The call it will make
        </h3>
        <pre className="m-0 overflow-x-auto whitespace-pre-wrap break-words rounded-cell border border-rule bg-panel px-4 py-3 font-mono text-secondary text-ink">
          {JSON.stringify(call.input, null, 2)}
        </pre>
      </section>
      <dl className="m-0 flex flex-col gap-2">
        <Fact label="Tool">
          <span className="font-mono">{call.tool}</span>
        </Fact>
        <Fact label="Connection">{call.connection}</Fact>
      </dl>
    </>
  );
}

/**
 * Approve (violet, A), Edit then approve (not built yet) and Deny (D). An Approval also gets the
 * Always allow checkbox when the API offers it to this caller (ADR 0031, decision 8).
 */
function ActionBar({
  entry,
  approval = false,
  phone,
  pending,
  allowAlways = false,
  alwaysAllow = false,
  onAlwaysAllow,
  onApprove,
  onDeny,
}: {
  entry: InboxEntry;
  approval?: boolean;
  phone: boolean;
  pending: boolean;
  allowAlways?: boolean;
  alwaysAllow?: boolean;
  onAlwaysAllow?: (checked: boolean) => void;
  onApprove: () => void;
  onDeny: () => void;
}) {
  const merge = !approval && decisionOf(entry) === 'merge';
  const full = phone ? 'w-full justify-center' : undefined;
  const call = entry.tool_approval;
  return (
    <div className={phone ? 'flex flex-col gap-2' : 'flex flex-wrap items-center gap-2'}>
      <Button variant="approve" onClick={onApprove} disabled={pending} className={full}>
        {merge ? 'Merge' : 'Approve'}
        <kbd className="font-control text-meta opacity-80">A</kbd>
      </Button>
      <NotAvailable label="Edit, then approve">
        <Button variant="secondary" className={full}>
          Edit, then approve
        </Button>
      </NotAvailable>
      <Button variant="secondary" onClick={onDeny} disabled={pending} className={full}>
        Deny
        <kbd className="font-control text-meta text-muted">D</kbd>
      </Button>
      {approval && allowAlways && call ? (
        <label className={cn('flex items-center gap-2 text-secondary text-ink-soft', !phone && 'ml-auto')}>
          <input
            type="checkbox"
            className="size-4 accent-you"
            checked={alwaysAllow}
            disabled={pending}
            onChange={(e) => onAlwaysAllow?.(e.target.checked)}
          />
          Always allow {call.agent} to {call.tool}
        </label>
      ) : null}
    </div>
  );
}

/** An Alert's facts: the andon pull, the rework tally, or why the item is blocked. */
function AlertFacts({ entry, station }: { entry: InboxEntry; station: string }) {
  if (entry.kind === 'andon' && entry.andon) {
    return (
      <section
        aria-label="The andon pull"
        className="flex flex-col gap-2 rounded-card border border-line bg-card p-4 md:p-5"
      >
        <p className="m-0 text-body text-ink">{entry.andon.reason}</p>
        <p className="m-0 text-secondary text-muted">
          Pulled {formatClock(entry.andon.pulled_at)}. The line releases nothing until someone clears it.
        </p>
      </section>
    );
  }
  if (entry.kind === 'rework_warning' && entry.rework) {
    const r = entry.rework;
    return (
      <section
        aria-label="Rework tally"
        className="flex flex-col gap-2 rounded-card border border-line bg-card p-4 md:p-5"
      >
        <p className="m-0 text-body text-ink">
          {r.rejected} of the last {r.verdicts} verdicts at {humanize(r.station)} sent the item back.
        </p>
        <p className="m-0 text-secondary text-muted">
          The andon policy pulls the cord at {formatPercent(r.threshold)} rework. The most frequent reason:{' '}
          {r.top_reason ? humanize(r.top_reason) : 'none given'}.
        </p>
      </section>
    );
  }
  if (entry.kind === 'blocked') {
    return (
      <section
        aria-label="Why it is blocked"
        className="flex flex-col gap-2 rounded-card border border-line bg-card p-4 md:p-5"
      >
        <p className="m-0 text-body text-ink">
          {entry.blocked?.paused ? 'A person paused it' : 'It is blocked'}
          {station ? ` at ${station}` : ''}.
        </p>
        <p className="m-0 text-secondary text-muted">{entry.blocked?.reason ?? 'No reason was recorded.'}</p>
      </section>
    );
  }
  return null;
}

/** Links out of an Alert: the item, and the factory's Floor. An Alert has no Approve or Deny. */
function AlertLinks({ entry }: { entry: InboxEntry }) {
  const org = useOrg();
  const factory = entry.factory.name;
  return (
    <div className="flex flex-wrap gap-2">
      {entry.item ? (
        <Button asChild variant="secondary">
          <Link to="/o/$org/f/$factory/items/$ref" params={{ org: org.slug, factory, ref: entry.item.ref }}>
            Open {entry.item.ref}
          </Link>
        </Button>
      ) : null}
      <Button asChild variant="secondary">
        <Link to="/o/$org/f/$factory" params={{ org: org.slug, factory }}>
          Open the {entry.factory.title ?? factory} Floor
        </Link>
      </Button>
    </div>
  );
}

/** The item's last few events, oldest first, from the events query the item page reads. */
function RecentEvents({ itemId }: { itemId: string }) {
  const client = useQueryClient();
  const org = useOrg();
  const { data: events = [], isPending, isError } = useQuery(eventsQuery(client, org.id, itemId));
  if (isPending) {
    return (
      <p aria-busy="true" className="m-0 text-secondary text-muted">
        Loading recent events
      </p>
    );
  }
  if (isError) return <p className="m-0 text-secondary text-rework-ink">Could not load recent events.</p>;
  const recent = events.slice(-5);
  if (recent.length === 0) return null;
  return (
    <section aria-labelledby="recent-events" className="flex flex-col gap-2">
      <h3 id="recent-events" className="m-0 text-heading font-semibold text-ink">
        Recent events on this item
      </h3>
      <ol className="m-0 flex list-none flex-col gap-2 p-0 text-secondary">
        {recent.map((e) => (
          <li key={e.id} className="flex gap-4">
            <time dateTime={e.at} className="w-28 flex-none text-muted">
              {formatClock(e.at)}
            </time>
            <span className="min-w-0 text-ink">
              {humanize(e.kind)}
              {e.station ? ` at ${humanize(e.station)}` : ''}
            </span>
          </li>
        ))}
      </ol>
    </section>
  );
}
