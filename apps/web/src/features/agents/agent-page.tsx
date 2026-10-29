// The Agent page (build/agent): who the agent is, the tab the URL names (?view=), and how its work went
// over the last 7 days. The statistics come from agentStatsQuery; the definition's facts (description,
// model, skills, prompt) come from the version that runs (shared/definition/agent-cards.tsx).
import { useSuspenseQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { agentStatsQuery } from '../../api/queries';
import type { AgentStats } from '../../api/types';
import { DraftChangesNote, SkillsCard, ToolPolicyCard, useRunningModel } from '../../shared/definition/agent-cards';
import { factoryTitle, useFactory, useFactoryParams } from '../../shared/factory';
import { formatDuration, formatPercent, formatUsd, humanize } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { SectionCard } from '../../ui/card';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { Meter } from '../../ui/hatch';
import { KpiTile } from '../../ui/kpi-tile';
import { PromptFull, PromptPreview } from './agent-prompt';
import { AgentSettings } from './agent-settings';
import { AskJonahButton } from './ask-jonah-button';
import { BakeOffButton } from './bake-off-button';
import { blockedSentence, reasonShare } from './sentences';
import { type AgentView, agentViews } from './views';

const none = '—';

const tabLabels: Record<AgentView, string> = {
  overview: 'Overview',
  prompt: 'Prompt',
  tools: 'Tools and access',
  runs: 'Runs',
};

/** Two letters for the avatar square: the first letters of the first two words of the name. */
export function initialsOf(name: string): string {
  const words = name.split(/[-_\s]+/).filter(Boolean);
  const letters = words.length > 1 ? words.slice(0, 2).map((w) => w.charAt(0)) : [...name.slice(0, 2)];
  return letters.join('').toUpperCase();
}

/** The last 7 days: the measures of the agent's finished tasks, one tile each. */
function AgentMeasures({ stats }: { stats: AgentStats }) {
  const ratio = stats.sent_back.ratio;
  return (
    <section aria-label="Last 7 days" className="grid grid-cols-2 gap-3 md:grid-cols-3">
      <KpiTile label="Tasks finished" value={stats.tasks_finished} />
      <KpiTile
        label="Sent back by review"
        value={
          <span className={cn(stats.sent_back.count > 0 && 'text-rework-ink')}>
            {ratio === null ? none : formatPercent(ratio)}
          </span>
        }
      />
      <KpiTile
        label="Spend per task"
        value={stats.spend_per_task_usd === null ? none : formatUsd(stats.spend_per_task_usd)}
      />
      <KpiTile
        label="Median turns"
        value={stats.median_turns === null ? none : String(Math.round(stats.median_turns))}
      />
      <KpiTile
        label="Time per task, p85"
        value={stats.task_time_p85_minutes === null ? none : formatDuration(stats.task_time_p85_minutes * 60_000)}
      />
    </section>
  );
}

/** "Why review sent work back": each reason with a hatched rust bar against the most frequent one. */
function SendBackReasons({ stats }: { stats: AgentStats }) {
  return (
    <SectionCard
      title="Why review sent work back"
      description={`${stats.sent_back.count} times in the last 7 days`}
      aside={<AskJonahButton />}
    >
      {stats.reasons.length === 0 ? (
        <p className="m-0 text-body text-muted">Nothing was sent back in the last 7 days.</p>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          {stats.reasons.map((r) => (
            <li
              key={r.label}
              className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-2 border-t border-line py-3 md:grid-cols-[minmax(0,1fr)_150px_auto]"
            >
              <span className="min-w-0 text-body">{r.label}</span>
              <span className="text-right text-body text-rework-ink tabular-nums md:col-start-3 md:row-start-1">
                {r.count}×<span className="sr-only"> sent back</span>
              </span>
              <Meter
                value={reasonShare(r, stats.reasons)}
                max={1}
                tone="rework"
                className="col-span-2 h-2.5 border border-line md:col-span-1 md:col-start-2 md:row-start-1"
              />
            </li>
          ))}
        </ul>
      )}
    </SectionCard>
  );
}

/** The tool calls the agent's policy blocked, under its Tool policy card. */
function BlockedCalls({ stats }: { stats: AgentStats }) {
  return <p className="m-0 text-secondary text-muted">{blockedSentence(stats.denied_tool_calls)}</p>;
}

export function AgentPage({ agent, view }: { agent: string; view: AgentView }) {
  const factory = useFactory();
  const org = useOrg();
  const params = useFactoryParams();
  const { data: stats } = useSuspenseQuery(agentStatsQuery(org.id, factory.id, agent));
  const description = useRunningModel().data?.agents.agents?.find((a) => a.name === agent)?.description;
  const agentParams = { ...params, agent };

  return (
    <div className="flex min-w-0 flex-col gap-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <nav aria-label="Breadcrumb">
          <ol className="m-0 flex list-none flex-wrap items-center gap-2 p-0 text-secondary text-muted">
            <li>
              <Link to="/o/$org/f/$factory" params={params} className="hover:text-ink">
                {factoryTitle(factory)}
              </Link>
            </li>
            <li aria-hidden="true">›</li>
            <li>Agents</li>
            <li aria-hidden="true">›</li>
            <li aria-current="page" className="font-control text-ink">
              {agent}
            </li>
          </ol>
        </nav>
        <div className="flex flex-wrap gap-2">
          <BakeOffButton agent={agent} />
          <Button asChild variant="secondary" size="sm">
            <Link
              to="/o/$org/f/$factory/definition/$section"
              params={{ ...params, section: 'agents' }}
              search={{ agent }}
            >
              Open prompt file
            </Link>
          </Button>
        </div>
      </div>

      <header className="flex flex-wrap items-start gap-4">
        <span
          aria-hidden="true"
          className="grid size-12 shrink-0 place-items-center rounded-card bg-run font-numeral text-heading text-white"
        >
          {initialsOf(agent)}
        </span>
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <h1 className="m-0 text-page-title break-words tracking-display">{stats.agent}</h1>
          {description ? <p className="m-0 text-body text-ink-soft">{description}</p> : null}
        </div>
        {stats.stations.length > 0 ? (
          <ul aria-label="Stations" className="m-0 flex list-none flex-wrap gap-2 p-0">
            {stats.stations.map((s) => (
              <li key={s.id}>
                <Chip>{`${humanize(s.id)} station`}</Chip>
              </li>
            ))}
          </ul>
        ) : null}
      </header>

      <nav aria-label="Agent views" className="flex gap-1 overflow-x-auto border-b border-line">
        {agentViews.map((v) => {
          const current = v === view;
          return (
            <Link
              key={v}
              to="/o/$org/f/$factory/agents/$agent"
              params={agentParams}
              search={{ view: v }}
              aria-current={current ? 'page' : undefined}
              className={cn(
                '-mb-px inline-flex h-10 shrink-0 items-center border-b-2 px-3 text-body transition-colors duration-200 hover:text-ink',
                current ? 'border-run font-control text-ink' : 'border-transparent text-ink-soft',
              )}
            >
              {tabLabels[v]}
            </Link>
          );
        })}
      </nav>

      <DraftChangesNote agent={agent} />

      {view === 'overview' ? (
        <div className="grid items-start gap-5 xl:grid-cols-[minmax(0,1fr)_400px]">
          <div className="flex min-w-0 flex-col gap-5">
            <AgentMeasures stats={stats} />
            <SendBackReasons stats={stats} />
            <PromptPreview agent={agent} />
          </div>
          <AgentSettings agent={agent} />
        </div>
      ) : null}

      {view === 'prompt' ? <PromptFull agent={agent} /> : null}

      {view === 'tools' ? (
        <div className="grid items-start gap-5 md:grid-cols-2">
          <SkillsCard agent={agent} />
          <ToolPolicyCard agent={agent}>
            <BlockedCalls stats={stats} />
          </ToolPolicyCard>
        </div>
      ) : null}

      {view === 'runs' ? (
        <div className="flex flex-col gap-5">
          <AgentMeasures stats={stats} />
          <SendBackReasons stats={stats} />
          <p className="m-0 text-secondary text-muted">A list of this agent’s runs is not built yet.</p>
        </div>
      ) : null}
    </div>
  );
}
