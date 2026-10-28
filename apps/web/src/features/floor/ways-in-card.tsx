// "How work gets in" on the Floor (ready.jpg, ADR 0032 decision 2): the ways an item reaches the factory.
// A GitHub row per enabled `github.issues.labeled` trigger, with the label the trigger matches (read from
// the published model's intake entry, where its filter says `label`). The Slack row connects while the
// organization has no slack connection. New item opens the composer (and C does too); goal item create
// is the CLI's way in. Its states: loading (sr-only), error (with Retry) and ready, with an empty line
// when the factory has no triggers.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Copy, Hash, Plus, SquareTerminal } from 'lucide-react';
import type { ReactNode } from 'react';
import { connectionsQuery, intakeQuery, versionModelQuery } from '../../api/queries';
import type { IntakeTrigger } from '../../api/types';
import { type Model, modelOf } from '../../shared/definition/model';
import { useFactory } from '../../shared/factory';
import { NewItemButton } from '../../shared/new-item';
import { useOrg } from '../../shared/org';
import { holds } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { GitHubMark } from '../../ui/github-mark';
import { CardEmpty, CardFailed, CardLoading } from './card-states';

/** The CLI command the last row copies. */
export const itemCreateCommand = 'goal item create';

/** One GitHub row: the label an enabled `github.issues.labeled` trigger matches. */
export interface LabelRow {
  name: string;
  label: string;
}

/**
 * The labels the enabled GitHub label triggers match. The label lives in the published intake entry of the
 * same name, as `filter.label`; a trigger with no label matches no row (there is nothing to copy).
 */
export function labelRows(triggers: readonly IntakeTrigger[], model: Model): LabelRow[] {
  const intake = model.line?.intake ?? [];
  return triggers
    .filter((trigger) => trigger.enabled && trigger.on === 'github.issues.labeled')
    .flatMap((trigger) => {
      const entry = intake.find((candidate) => candidate.name === trigger.name);
      const filter = entry?.filter as { label?: unknown } | undefined;
      return typeof filter?.label === 'string' && filter.label !== ''
        ? [{ name: trigger.name, label: filter.label }]
        : [];
    });
}

export function WaysInCard() {
  const org = useOrg();
  const factory = useFactory();
  const intake = useQuery(intakeQuery(org.id, factory.id));
  const model = useQuery({ ...versionModelQuery(org.id, factory.id, 'current'), select: (v) => modelOf(v.model) });
  const connections = useQuery(connectionsQuery(org.id));
  const queries = [intake, model, connections];

  if (queries.some((query) => query.isError)) {
    const retry = () => {
      for (const query of queries) if (query.isError) void query.refetch();
    };
    return (
      <section aria-labelledby="ways-h" className={card}>
        <h2 id="ways-h" className={heading}>
          How work gets in
        </h2>
        <div className="mt-3">
          <CardFailed what="how work gets in" onRetry={retry} />
        </div>
      </section>
    );
  }
  if (queries.some((query) => query.isPending) || !intake.data || !model.data) {
    return (
      <div aria-busy="true">
        <CardLoading />
      </div>
    );
  }

  const triggers = intake.data.triggers;
  const labels = labelRows(triggers, model.data);
  const hasSlack = (connections.data ?? []).some((connection) => connection.kind === 'slack');
  const canCreate = holds(factory, 'item:create');

  return (
    <section aria-labelledby="ways-h" className={card}>
      <h2 id="ways-h" className={cn(heading, 'mb-2')}>
        How work gets in
      </h2>
      {triggers.length === 0 ? (
        <CardEmpty>This factory has no triggers yet. Work comes in through New item or the CLI.</CardEmpty>
      ) : null}
      <ul className="m-0 flex list-none flex-col p-0">
        {labels.map((row) => (
          <li key={row.name}>
            <WayLine tile="bg-ink" icon={<GitHubMark className="text-white" />} text="Label an issue">
              <Chip>{row.label}</Chip>
              <CopyButton text={row.label} name={`label ${row.label}`} />
            </WayLine>
          </li>
        ))}
        <li>
          <WayLine
            tile="bg-you-ink"
            icon={<Hash size={14} strokeWidth={1.6} aria-hidden="true" />}
            text={
              <>
                Mention <Chip>@{factory.name}</Chip> in Slack
              </>
            }
          >
            {hasSlack ? null : (
              <Button asChild size="sm" variant="secondary">
                <Link to="/o/$org/connections/new" params={{ org: org.slug }} search={{ kind: 'slack' }}>
                  Connect
                </Link>
              </Button>
            )}
          </WayLine>
        </li>
        {canCreate ? (
          <li>
            <WayLine tile="bg-run" icon={<Plus size={14} strokeWidth={1.8} aria-hidden="true" />} text={null}>
              <NewItemButton
                factory={factory}
                trigger={
                  <button type="button" className="inline-flex flex-wrap items-center gap-x-2 gap-y-1 text-ink">
                    New item, or press <Chip>C</Chip>
                  </button>
                }
              />
            </WayLine>
          </li>
        ) : null}
        <li>
          <WayLine
            tile="bg-ink-soft"
            icon={<SquareTerminal size={14} strokeWidth={1.6} aria-hidden="true" />}
            text={<span className="font-mono text-meta text-ink">{itemCreateCommand}</span>}
          >
            <CopyButton text={itemCreateCommand} name="command" />
          </WayLine>
        </li>
      </ul>
    </section>
  );
}

const card = 'flex min-w-0 flex-col rounded-card border border-line bg-card p-4 pb-2 md:p-5 md:pb-2';
const heading = 'm-0 text-heading font-semibold text-ink';

/** One row: a tinted tile, the text, and what the row offers (a chip, a copy or a link). */
function WayLine({
  tile,
  icon,
  text,
  children,
}: {
  tile: string;
  icon: ReactNode;
  text: ReactNode;
  children?: ReactNode;
}) {
  return (
    <div className="flex items-center gap-3 border-t border-line py-2 text-body">
      <span className={`flex size-7.5 shrink-0 items-center justify-center rounded-control text-white ${tile}`}>
        {icon}
      </span>
      <span className="flex min-w-0 flex-1 flex-wrap items-center gap-x-2 gap-y-1 text-ink-soft">
        {text}
        {children}
      </span>
    </div>
  );
}

/** Copies the text; a browser that refuses the clipboard leaves the text to be selected by hand. */
function CopyButton({ text, name }: { text: string; name: string }) {
  return (
    <Button
      size="sm"
      aria-label={`Copy ${name}`}
      onClick={() => void navigator.clipboard?.writeText(text).catch(() => undefined)}
    >
      <Copy size={12} strokeWidth={1.75} aria-hidden="true" />
      Copy
    </Button>
  );
}
