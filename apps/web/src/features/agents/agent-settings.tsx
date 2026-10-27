// The Settings aside of the Overview: what the version that runs sets for the agent. A value the agent
// sets reads "Own value"; one it leaves to the factory reads "Default", with the factory's value beside
// it. Read only: the Definition editor changes these.
import { Link } from '@tanstack/react-router';
import type { ReactNode } from 'react';
import { useRunningModel } from '../../shared/definition/agent-cards';
import { toolPolicy } from '../../shared/definition/tool-policy';
import { useFactoryParams } from '../../shared/factory';
import { formatUsd } from '../../shared/format';
import { SectionCard } from '../../ui/card';
import { Chip } from '../../ui/chip';

const none = '—';

function Setting({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-[110px_minmax(0,1fr)] gap-x-3 border-t border-line py-3">
      <dt className="text-secondary text-muted">{label}</dt>
      <dd className="m-0 flex min-w-0 flex-col gap-2">{children}</dd>
    </div>
  );
}

/** "Own value" when the agent sets it, "Default" when the factory does. */
function Origin({ own }: { own: boolean }) {
  return own ? (
    <span className="rounded-chip bg-run-tint px-2 text-meta font-control text-run-ink">Own value</span>
  ) : (
    <span className="rounded-chip border border-line px-2 text-meta text-muted">Default</span>
  );
}

export function AgentSettings({ agent }: { agent: string }) {
  const params = useFactoryParams();
  const running = useRunningModel();
  const model = running.data;
  const found = model?.agents.agents?.find((a) => a.name === agent);
  const defaults = model?.review.budget;
  const policy = found && !found.custom_tools ? toolPolicy(found) : undefined;
  const modelShown = found?.model ?? model?.agents.default_model;

  return (
    <SectionCard title="Settings" description="Unset values come from factory defaults" className="gap-2">
      {found && model ? (
        <dl className="m-0">
          <Setting label="Model">
            <span className="flex flex-wrap items-center gap-2">
              {modelShown ? <Chip tone="run">{modelShown}</Chip> : <span className="text-body text-muted">{none}</span>}
              <Origin own={found.model !== undefined} />
            </span>
          </Setting>
          <Setting label="Turns per run">
            <span className="flex flex-wrap items-center gap-2">
              <b className="font-numeral">{found.max_turns ?? defaults?.max_turns_per_task ?? none}</b>
              <Origin own={found.max_turns !== undefined} />
              {found.max_turns !== undefined && defaults?.max_turns_per_task !== undefined ? (
                <span className="text-meta text-muted">Default {defaults.max_turns_per_task}</span>
              ) : null}
            </span>
          </Setting>
          <Setting label="Per item">
            <span className="flex flex-wrap items-center gap-2">
              <b className="font-numeral">
                {found.per_item_usd !== undefined
                  ? formatUsd(found.per_item_usd)
                  : defaults?.per_item_usd !== undefined
                    ? formatUsd(defaults.per_item_usd)
                    : none}
              </b>
              <Origin own={found.per_item_usd !== undefined} />
              {found.per_item_usd !== undefined && defaults?.per_item_usd !== undefined ? (
                <span className="text-meta text-muted">Default {formatUsd(defaults.per_item_usd)}</span>
              ) : null}
            </span>
          </Setting>
          <Setting label="Skills">
            {found.skills?.length ? (
              <span className="flex flex-wrap gap-2">
                {found.skills.map((s) => (
                  <Chip key={s}>{s}</Chip>
                ))}
              </span>
            ) : (
              <span className="text-body text-muted">None</span>
            )}
          </Setting>
          <Setting label="Connections">
            {found.connections?.length ? (
              <span className="flex flex-wrap gap-2">
                {found.connections.map((c) => (
                  <Chip key={c}>{c}</Chip>
                ))}
              </span>
            ) : (
              <span className="text-body text-muted">None</span>
            )}
          </Setting>
          <Setting label="Tools">
            <span className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
              {policy ? (
                <span>
                  <b className="font-control text-ok-ink">{policy.allowed.length} allowed</b>,{' '}
                  <b className="font-control text-rework-ink">{policy.denied.length} denied</b>
                </span>
              ) : null}
              <Link
                to="/o/$org/f/$factory/agents/$agent"
                params={{ ...params, agent }}
                search={{ view: 'tools' }}
                className="font-control text-run-ink hover:text-run"
              >
                Show the lists
              </Link>
            </span>
          </Setting>
        </dl>
      ) : running.isPending ? (
        <p aria-busy="true" className="m-0">
          <span className="sr-only">Loading settings</span>
        </p>
      ) : (
        <p className="m-0 text-body text-muted">
          {running.isError
            ? 'The version that runs can’t be read right now.'
            : `The version that runs has no agent named ${agent}.`}
        </p>
      )}
    </SectionCard>
  );
}
