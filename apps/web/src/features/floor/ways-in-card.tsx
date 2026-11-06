// "How work gets in" on the Floor (ready.jpg, ADR 0032 decision 2): the ways an item reaches the factory.
// The Slack row connects while the
// organization has no slack connection. New item opens the composer (and C does too); goal run
// is the CLI's way in. Its states: loading (sr-only), error (with Retry) and ready, with an empty line
// when the factory has no triggers.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Hash, Plus, SquareTerminal } from 'lucide-react';
import type { ReactNode } from 'react';
import { connectionsQuery, intakeQuery, versionModelQuery } from '../../api/queries';
import { modelOf } from '../../shared/definition/model';
import { useFactory } from '../../shared/factory';
import { NewItemButton } from '../../shared/new-item';
import { useOrg } from '../../shared/org';
import { holds } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { CopyButton } from '../../ui/copy-button';
import { CardEmpty, CardFailed, CardLoading } from './card-states';

/** The CLI command the last row copies. */
export const runCommand = 'goal run';

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
        <li>
          <WayLine
            icon={<Hash size={14} strokeWidth={1.6} aria-hidden="true" />}
            text={
              <>
                Mention <Chip>@{factory.name}</Chip> in Slack
              </>
            }
          >
            {hasSlack ? null : (
              <Button asChild size="sm" variant="secondary">
                <Link to="/o/$org/secrets" params={{ org: org.slug }} search={{ new: true }}>
                  Connect
                </Link>
              </Button>
            )}
          </WayLine>
        </li>
        {canCreate ? (
          <li>
            <WayLine icon={<Plus size={14} strokeWidth={1.8} aria-hidden="true" />} text={null}>
              <NewItemButton
                factory={factory}
                trigger={
                  <button type="button" className="inline-flex items-center text-ink max-md:min-h-11">
                    New item
                    <span className="inline-flex items-center gap-x-2 pointer-coarse:hidden">
                      <span>, or press</span>
                      <Chip>C</Chip>
                    </span>
                  </button>
                }
              />
            </WayLine>
          </li>
        ) : null}
        <li>
          <WayLine
            icon={<SquareTerminal size={14} strokeWidth={1.6} aria-hidden="true" />}
            text={<span className="font-mono text-meta text-ink">{runCommand}</span>}
          >
            <CopyButton text={runCommand} aria-label="Copy command" />
          </WayLine>
        </li>
      </ul>
    </section>
  );
}

const card = 'flex min-w-0 flex-col rounded-card border border-line bg-card p-4 pb-2 md:p-5 md:pb-2';
const heading = 'm-0 text-heading font-semibold text-ink';

/** One row: its icon in muted, the text, and what the row offers (a chip, a copy or a link). No icon tile. */
function WayLine({ icon, text, children }: { icon: ReactNode; text: ReactNode; children?: ReactNode }) {
  return (
    <div className="flex items-center gap-3 border-t border-line py-2 text-body">
      <span className="flex size-4 shrink-0 items-center justify-center text-muted">{icon}</span>
      <span className="flex min-w-0 flex-1 flex-wrap items-center gap-x-2 gap-y-1 text-ink-soft">
        {text}
        {children}
      </span>
    </div>
  );
}
