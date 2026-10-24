// One connection before phase 4 (D §10.8): what it is, how it signs in (a platform secret's env:
// name, never a value), its status from B's problems, the agents allowed to use it, Edit in YAML
// at its line, and Remove… (it and every agent's reference to it, one save per section touched).
import { useId, useState } from 'react';
import { toast } from 'sonner';
import type { ModelConnection } from '../../../shared/definition/model';
import { Button } from '../../../ui/button';
import { cn } from '../../../ui/cn';
import { ConfirmDialog } from '../../../ui/confirm-dialog';
import { removeConnection, setConnectionAgents } from '../draft/ops';
import { useDraft, useDraftSession, useModel, useViewOnly } from '../draft/use-draft';
import { useValidation } from '../frame/use-editor';
import { SearchLink, useSetSearch } from '../search-link';
import { agentsWithAccess, connectionKind, connectionStatus, problemsAt, signIn, statusTone } from './describe';

const and = (words: string[]) =>
  words.length < 2 ? (words[0] ?? '') : `${words.slice(0, -1).join(', ')} and ${words.at(-1)}`;

export function ConnectionDetail({ connection, index }: { connection: ModelConnection; index: number }) {
  const session = useDraftSession();
  const viewOnly = useViewOnly();
  const model = useModel();
  const setSearch = useSetSearch();
  const headingId = useId();
  const problems = useValidation()?.problems ?? [];
  const line = useDraft((s) => s.ranges[`/connections/${index}`]?.[0]);
  const [removing, setRemoving] = useState(false);
  const name = connection.name;
  const allowed = agentsWithAccess(model, connection);
  const agents = (model.agents.agents ?? []).map((a) => a.name);
  const status = connectionStatus(connection, index, problems);
  const messages = problemsAt(index, problems).filter((p) => p.severity !== 'info');
  const sign = signIn(connection);

  const toggle = (agent: string, on: boolean) => {
    const next = on ? [...allowed, agent] : allowed.filter((a) => a !== agent);
    // Keep the agents in the order the factory lists them.
    session.edit(
      setConnectionAgents(
        model,
        name,
        agents.filter((a) => next.includes(a)),
      ),
    );
  };

  return (
    <section
      aria-labelledby={headingId}
      className="flex min-w-0 flex-col gap-4 rounded-card border border-line bg-card p-4 md:p-5"
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 flex-[1_1_300px]">
          <h3
            id={headingId}
            translate="no"
            className="m-0 text-[28px] leading-tight font-normal tracking-display break-words"
          >
            {name}
          </h3>
          <p className="m-0 mt-1 text-sm text-muted">
            {connectionKind(connection)}
            {connection.legacy ? ', reached through Goal’s gateway.' : '.'}
          </p>
        </div>
        {viewOnly ? null : (
          <Button variant="white" size="sm" className="h-11 md:h-8" onClick={() => setRemoving(true)}>
            Remove…
          </Button>
        )}
      </div>

      <dl className="m-0 grid grid-cols-1 gap-3.5 @[560px]:grid-cols-2">
        <div className="flex min-w-0 flex-col gap-1.5">
          <dt className="text-sm font-medium">Sign-in</dt>
          <dd className="m-0 text-[15px] leading-normal">
            {sign === 'No sign-in' ? (
              'None needed.'
            ) : (
              <>
                <span translate="no" className="font-mono text-sm">
                  {sign.replace(/^Uses /, '')}
                </span>
                {connection.credential ? null : ', a platform secret of this install.'}
              </>
            )}
            <span className="mt-0.5 block text-[13px] text-muted">Choosing a credential arrives with credentials.</span>
          </dd>
        </div>
        <div className="flex min-w-0 flex-col gap-1.5">
          <dt className="text-sm font-medium">Status</dt>
          <dd className={cn('m-0 text-[15px] leading-normal', statusTone[status.tone])}>
            {status.text}
            {messages.length > 0 ? (
              <ul className="m-0 mt-1 flex list-none flex-col gap-0.5 p-0 text-[13px]">
                {messages.map((p) => (
                  <li key={`${p.code}:${p.pointer}`}>{p.message}</li>
                ))}
              </ul>
            ) : null}
          </dd>
        </div>
      </dl>

      <fieldset className="m-0 flex min-w-0 flex-col gap-2 border-0 p-0">
        <legend className="mb-1 p-0 text-[15px] font-medium">Agents allowed to use {name}</legend>
        {agents.length > 0 ? (
          <div className="flex flex-wrap gap-1.5">
            {agents.map((agent) => (
              <label
                key={agent}
                className="inline-flex h-11 cursor-pointer items-center gap-2 rounded-chip border border-line bg-surface pr-2.5 pl-2 has-[:disabled]:cursor-default text-[13px] whitespace-nowrap transition-colors duration-200 has-[:checked]:border-ink has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-2 has-[:focus-visible]:outline-run md:h-8"
              >
                <input
                  type="checkbox"
                  checked={allowed.includes(agent)}
                  disabled={viewOnly}
                  onChange={(e) => toggle(agent, e.target.checked)}
                  className="m-0 size-4 cursor-pointer accent-ink focus-visible:outline-none disabled:cursor-default"
                />
                <span translate="no">{agent}</span>
              </label>
            ))}
          </div>
        ) : (
          <p className="m-0 text-sm text-muted">This factory has no agents yet.</p>
        )}
        <p className="m-0 text-[13px] leading-snug text-muted">
          {connection.legacy ? 'Saved in each agent’s connections.' : 'Saved in the connection’s agents.'}
        </p>
      </fieldset>

      <SearchLink
        patch={{ mode: 'yaml', line, connection: name }}
        className="inline-flex min-h-11 items-center self-start text-sm text-ink underline underline-offset-3 hover:text-run md:min-h-6"
      >
        Edit {name} in YAML
      </SearchLink>

      <ConfirmDialog
        open={removing}
        onOpenChange={setRemoving}
        tone="danger"
        title={`Remove ${name}?`}
        description={
          allowed.length > 0
            ? `${and(allowed)} ${allowed.length === 1 ? 'loses' : 'lose'} it. Undo puts it back.`
            : 'No agent uses it. Undo puts it back.'
        }
        confirmLabel="Remove connection"
        onConfirm={() => {
          session.edit(removeConnection(model, name));
          setRemoving(false);
          setSearch({ connection: undefined });
          toast.success(`${name} removed`);
        }}
      />
    </section>
  );
}
