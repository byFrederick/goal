// Connections and knowledge (spec 4 §4.4, §10, canvas Define-Connections and New-Connections):
// which of the organization's connections the factory's runs get (a tick puts its name in
// spec.connections), which of them each agent's runs get (the agent's `connections`; the model and
// repository connections go to every run), and the knowledge sources its agents may search
// (knowledge.sources; none exist before the knowledge base, 4-17). Beside them, the variables
// runs get. Each kind's modes show as badges; secrets stay with the connections, managed on the
// Connections page. B's problems show where they are fixed: an entry the organization lacks or
// an inline form phase 4 removed under the list, two connections setting one variable under the
// agent, each with its quick fixes.
import { Link } from '@tanstack/react-router';
import { Fragment, useId } from 'react';
import type { Connection, ConnectionKind } from '../../../api/types';
import { runEnv } from '../../../shared/connections';
import type { Model } from '../../../shared/definition/model';
import { useOrg, useOrgHolds } from '../../../shared/org';
import { Chip } from '../../../ui/chip';
import { useOrgConnections } from '../basics/runtime-fields';
import { setAgentConnection, setBasics, setFactoryConnection } from '../draft/ops';
import { useDraftSession, useSection, useViewOnly } from '../draft/use-draft';
import { ProblemFixes } from '../frame/problem-fixes';
import { bareSection, SectionShell } from '../frame/section-shell';
import { useProblems } from '../frame/use-editor';
import type { SectionViewProps } from '../section-views';
import { runsGetSummary } from './runs-get';

const card = 'flex min-w-0 flex-col gap-3 rounded-card border border-line bg-card p-4 md:p-5';
const checkbox = 'size-[18px] shrink-0 cursor-pointer accent-ink disabled:cursor-default';

export function ConnectionsSection(_: SectionViewProps) {
  const org = useOrg();
  const basics = useSection('basics');
  const factory = basics.name ?? 'this factory';
  return (
    <SectionShell
      section="connections"
      title="Connections and knowledge"
      description={
        <>
          Which of {org.name}’s connections {factory}’s runs get, per agent, and what its agents may search. Secrets
          stay with the connections; owners and admins manage them in{' '}
          <Link to="/o/$org/connections" params={{ org: org.slug }} className="text-ink underline underline-offset-2">
            Connections
          </Link>
          .
        </>
      }
      className={bareSection}
    >
      <div className="grid items-start gap-4 @[960px]:grid-cols-[minmax(0,1fr)_320px]">
        <div className="flex min-w-0 flex-col gap-4">
          <FactoryConnections factory={factory} />
          <PerAgent />
          <Knowledge factory={factory} />
        </div>
        <RunsGet />
      </div>
    </SectionShell>
  );
}

/** The current value of every section, as a fix or a multi-section edit reads it. */
function useModel(): Model {
  return {
    basics: useSection('basics'),
    line: useSection('line'),
    agents: useSection('agents'),
    connections: useSection('connections'),
    review: useSection('review'),
  };
}

function FactoryConnections({ factory }: { factory: string }) {
  const org = useOrg();
  const session = useDraftSession();
  const viewOnly = useViewOnly();
  const manage = useOrgHolds('connection:manage');
  const model = useModel();
  const { connections, kindOf } = useOrgConnections();
  const named = new Set(model.connections.map((c) => c.name));
  const id = useId();
  // Entries that are not one of the organization's connections: a name it lacks, an inline form.
  const others = model.connections
    .map((c, index) => ({ c, index }))
    .filter(({ c }) => c.type || c.connector || !connections.some((x) => x.name === c.name));

  return (
    <section aria-labelledby={id} className={card}>
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h3 id={id} className="m-0 text-lg font-medium">
          Connections {factory} uses
        </h3>
        {manage && !viewOnly ? (
          <Link
            to="/o/$org/connections/new"
            params={{ org: org.slug }}
            className="inline-flex min-h-11 items-center text-sm text-ink underline underline-offset-3 hover:text-run md:min-h-6"
          >
            New connection
          </Link>
        ) : null}
      </div>
      {connections.length === 0 ? (
        <p className="m-0 text-sm text-muted">
          {org.name} has no connections yet.{' '}
          {manage ? 'Add one in Connections, then tick it here.' : 'Owners and admins add them in Connections.'}
        </p>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          <li
            aria-hidden="true"
            className="hidden grid-cols-[28px_minmax(0,1.2fr)_minmax(0,1fr)_minmax(0,1.3fr)] gap-3 pb-2 text-[13px] text-muted @[640px]:grid"
          >
            <span />
            <span>Connection</span>
            <span>Modes</span>
            <span>Runs get</span>
          </li>
          {connections.map((c) => (
            <ConnectionRow
              key={c.id}
              connection={c}
              kind={kindOf(c)}
              checked={named.has(c.name)}
              disabled={viewOnly}
              onChange={(on) => session.edit(setFactoryConnection(model, c.name, on))}
            />
          ))}
        </ul>
      )}
      {others.length > 0 ? (
        <div className="flex flex-col gap-2 rounded-control border border-line bg-surface p-3">
          {others.map(({ c, index }) => (
            <div key={`${c.name}-${index}`} className="flex flex-col gap-1">
              <span className="text-sm">
                <span translate="no" className="font-mono font-medium">
                  {c.name}
                </span>
                <span className="text-muted">
                  {c.type || c.connector ? ', an inline connection' : `, not one of ${org.name}’s connections`}
                </span>
              </span>
              <ProblemFixes pointer={`/connections/${index}`} />
            </div>
          ))}
        </div>
      ) : null}
    </section>
  );
}

function ConnectionRow({
  connection: c,
  kind,
  checked,
  disabled,
  onChange,
}: {
  connection: Connection;
  kind: ConnectionKind | undefined;
  checked: boolean;
  disabled: boolean;
  onChange: (on: boolean) => void;
}) {
  const id = useId();
  const env = runEnv(kind, c);
  const summary = runsGetSummary(kind, env);
  return (
    <li className="grid grid-cols-[28px_minmax(0,1fr)] items-start gap-x-3 gap-y-1.5 border-t border-line py-2.5 @[640px]:grid-cols-[28px_minmax(0,1.2fr)_minmax(0,1fr)_minmax(0,1.3fr)] @[640px]:items-center">
      <span className="flex min-h-11 items-center md:min-h-6">
        <input
          id={id}
          type="checkbox"
          checked={checked}
          disabled={disabled}
          onChange={(e) => onChange(e.target.checked)}
          className={checkbox}
        />
      </span>
      <label htmlFor={id} className="flex min-h-11 cursor-pointer flex-col justify-center md:min-h-0">
        <span translate="no" className="font-mono text-[15px] font-medium">
          {c.name}
        </span>
        <span className="text-[13px] text-muted">{kind?.title ?? c.kind}</span>
      </label>
      <span className="col-start-2 flex flex-wrap gap-1 @[640px]:col-start-auto">
        {kind?.modes.map((m) => (
          <Chip key={m} className="font-mono font-normal">
            {m === 'cli' ? 'CLI' : m === 'mcp' ? 'MCP' : m}
          </Chip>
        ))}
      </span>
      <span className="col-start-2 flex flex-wrap items-center gap-1 text-[13px] @[640px]:col-start-auto">
        {summary.names.length === 0 ? (
          <span className="text-muted">None</span>
        ) : (
          summary.names.map((n) => (
            <code
              key={n}
              translate="no"
              className="rounded-chip border border-line bg-surface px-1.5 py-0.5 font-mono text-xs"
            >
              {n}
            </code>
          ))
        )}
        {summary.more > 0 ? (
          <span className="rounded-chip border border-line bg-surface px-1.5 py-0.5 text-xs">
            +{summary.more}
            <span className="sr-only"> more</span>
          </span>
        ) : null}
        {summary.minted ? <span className="text-run">minted per run</span> : null}
      </span>
    </li>
  );
}

function PerAgent() {
  const session = useDraftSession();
  const viewOnly = useViewOnly();
  const agentsSection = useSection('agents');
  const connections = useSection('connections');
  const { connections: org } = useOrgConnections();
  const id = useId();
  const agents = agentsSection.agents ?? [];
  // The factory's organization connections, in its order.
  const names = connections
    .filter((c) => !c.type && !c.connector && org.some((x) => x.name === c.name))
    .map((c) => c.name);

  return (
    <section aria-labelledby={id} className={card}>
      <div className="flex flex-col gap-1">
        <h3 id={id} className="m-0 text-lg font-medium">
          Per agent
        </h3>
        <p className="m-0 text-sm text-muted">
          What each agent’s runs get. The model and repository connections go to every run.
        </p>
      </div>
      {agents.length === 0 || names.length === 0 ? (
        <p className="m-0 text-sm text-muted">
          {agents.length === 0 ? 'Add agents first.' : 'Tick connections above, then choose which agents get each.'}
        </p>
      ) : (
        <>
          {/* A matrix where it fits: an agent per row, a connection per column. */}
          <table className="hidden w-full border-collapse text-sm @[640px]:table">
            <caption className="sr-only">Connections each agent’s runs get</caption>
            <thead>
              <tr className="text-left text-[13px] text-muted [&>th]:pb-2 [&>th]:font-normal">
                <th scope="col">Agent</th>
                {names.map((n) => (
                  <th key={n} scope="col" translate="no" className="font-mono">
                    {n}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {agents.map((a, i) => (
                <Fragment key={a.name}>
                  <tr className="border-t border-line [&>td]:py-2">
                    <th scope="row" translate="no" className="text-left font-mono text-sm font-normal">
                      {a.name}
                    </th>
                    {names.map((n) => (
                      <td key={n}>
                        <input
                          type="checkbox"
                          aria-label={`${a.name} gets ${n}`}
                          checked={a.connections?.includes(n) ?? false}
                          disabled={viewOnly}
                          onChange={(e) => session.edit(setAgentConnection(agentsSection, a.name, n, e.target.checked))}
                          className={checkbox}
                        />
                      </td>
                    ))}
                  </tr>
                  <AgentProblems index={i} span={names.length + 1} />
                </Fragment>
              ))}
            </tbody>
          </table>
          <ul className="m-0 flex list-none flex-col p-0 @[640px]:hidden">
            {agents.map((a, i) => (
              <li key={a.name} className="flex flex-col gap-2 border-t border-line py-2.5 first:border-t-0 first:pt-0">
                <fieldset className="m-0 flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1 border-0 p-0">
                  <legend className="mb-0.5 p-0 font-mono text-sm" translate="no">
                    {a.name}
                  </legend>
                  {names.map((n) => {
                    const on = a.connections?.includes(n) ?? false;
                    return (
                      <label
                        key={n}
                        className="inline-flex min-h-11 cursor-pointer items-center gap-2 text-sm has-[:disabled]:cursor-default md:min-h-8"
                      >
                        <input
                          type="checkbox"
                          aria-label={`${a.name} gets ${n}`}
                          checked={on}
                          disabled={viewOnly}
                          onChange={(e) => session.edit(setAgentConnection(agentsSection, a.name, n, e.target.checked))}
                          className={checkbox}
                        />
                        <span translate="no" className="font-mono">
                          {n}
                        </span>
                      </label>
                    );
                  })}
                </fieldset>
                <ProblemFixes pointer={`/agents/agents/${i}/connections`} className={problemBox} />
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  );
}

const problemBox = 'rounded-control bg-rework-tint/50 p-2';

/** The agent's problems (two connections setting one variable) in a row under its own. */
function AgentProblems({ index, span }: { index: number; span: number }) {
  const problems = useProblems(`/agents/agents/${index}/connections`);
  if (problems.length === 0) return null;
  return (
    <tr>
      <td colSpan={span} className="pb-2">
        <ProblemFixes pointer={`/agents/agents/${index}/connections`} className={problemBox} />
      </td>
    </tr>
  );
}

function Knowledge({ factory }: { factory: string }) {
  const session = useDraftSession();
  const viewOnly = useViewOnly();
  const basics = useSection('basics');
  const sources = basics.knowledge_sources ?? [];
  const id = useId();
  return (
    <section aria-labelledby={id} className={card}>
      <div className="flex flex-col gap-1">
        <h3 id={id} className="m-0 text-lg font-medium">
          Knowledge {factory} may search
        </h3>
        <p className="m-0 text-sm text-muted">
          Agents search the ticked sources with <code className="font-mono">knowledge__search</code>.
        </p>
      </div>
      {sources.length === 0 ? (
        <p className="m-0 rounded-control border border-dashed border-rule px-3 py-2.5 text-sm text-muted">
          No knowledge sources yet: they arrive with Knowledge.
        </p>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          {sources.map((s, i) => (
            <li key={s} className="flex flex-col gap-1.5 border-t border-line py-2.5 first:border-t-0 first:pt-0">
              <label className="inline-flex min-h-11 cursor-pointer items-center gap-2 text-[15px] has-[:disabled]:cursor-default md:min-h-8">
                <input
                  type="checkbox"
                  checked
                  disabled={viewOnly}
                  onChange={() =>
                    session.edit(
                      setBasics(
                        basics,
                        'knowledge_sources',
                        sources.filter((x) => x !== s),
                      ),
                    )
                  }
                  className={checkbox}
                />
                {s}
              </label>
              <ProblemFixes pointer={`/basics/knowledge_sources/${i}`} />
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

/** What runs get (canvas "What runs get"): the model connection, each ticked connection's variables, knowledge. */
function RunsGet() {
  const basics = useSection('basics');
  const connections = useSection('connections');
  const { connections: org, kindOf } = useOrgConnections();
  const id = useId();
  const ticked = connections
    .map((c) => org.find((x) => x.name === c.name && !c.type && !c.connector))
    .filter((c): c is Connection => c !== undefined);
  const repo = basics.repository?.connection;
  const rows = [
    ...(basics.model_connection ? [{ name: basics.model_connection, text: 'Model access, from Basics' }] : []),
    ...(repo && !ticked.some((c) => c.name === repo) && repo !== basics.model_connection
      ? [{ name: repo, text: 'git for the repository, from Basics' }]
      : []),
  ];
  return (
    <section aria-labelledby={id} className={card}>
      <h3 id={id} className="m-0 text-lg font-medium">
        What runs get
      </h3>
      <p className="m-0 text-sm leading-normal text-muted">
        Environment variables, set when a run’s box starts. Values come from the connections; nothing secret is in the
        definition.
      </p>
      <dl className="m-0 flex flex-col">
        {rows.map((r) => (
          <div key={r.name} className="grid grid-cols-[96px_minmax(0,1fr)] gap-3 border-t border-line py-2.5">
            <dt translate="no" className="truncate font-mono text-[13px]">
              {r.name}
            </dt>
            <dd className="m-0 text-sm">{r.text}</dd>
          </div>
        ))}
        {ticked.map((c) => {
          const env = runEnv(kindOf(c), c);
          return (
            <div key={c.name} className="grid grid-cols-[96px_minmax(0,1fr)] gap-3 border-t border-line py-2.5">
              <dt translate="no" className="truncate font-mono text-[13px]">
                {c.name}
              </dt>
              <dd className="m-0 flex flex-wrap gap-1 text-sm">
                {env.length === 0
                  ? kindOf(c)?.modes.includes('mcp')
                    ? 'MCP server in the box'
                    : 'None'
                  : env.map((n) => (
                      <code
                        key={n}
                        translate="no"
                        className="rounded-chip border border-line bg-surface px-1.5 py-0.5 font-mono text-xs"
                      >
                        {n}
                      </code>
                    ))}
              </dd>
            </div>
          );
        })}
        <div className="grid grid-cols-[96px_minmax(0,1fr)] gap-3 border-t border-line py-2.5">
          <dt className="text-[13px]">Knowledge</dt>
          <dd className="m-0 text-sm">
            {(basics.knowledge_sources ?? []).length > 0 ? (
              <>
                <code className="font-mono text-xs">knowledge__search</code> over{' '}
                {(basics.knowledge_sources ?? []).length} sources
              </>
            ) : (
              'No sources'
            )}
          </dd>
        </div>
      </dl>
    </section>
  );
}
