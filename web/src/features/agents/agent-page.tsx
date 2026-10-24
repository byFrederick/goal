// The Agent page (Agent and Mobile-Agent boards): who the agent is and how its work went over the
// last 7 days. E12 builds the frame and the statistics; D12 adds the definition's cards
// (Connections, Skills, Tool policy) and "Edit in Definition" through AgentPage's slots.
import { useSuspenseQuery } from '@tanstack/react-query';
import { Link, useParams } from '@tanstack/react-router';
import type { ReactNode } from 'react';
import { agentStatsQuery } from '../../api/queries';
import type { AgentStats } from '../../api/types';
import { factoryTitle, useFactory, useFactoryParams } from '../../shared/factory';
import { formatDuration, formatPercent, formatUsd } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { SectionCard } from '../../ui/card';
import { cn } from '../../ui/cn';
import { Meter } from '../../ui/hatch';
import { agentSentence, blockedSentence, reasonShare } from './sentences';

const none = '—';

interface Tile {
  label: string;
  /** The phone's shorter label. */
  short: string;
  value: string;
  /** Sent back is rust whenever something was. */
  rework?: boolean;
  /** Phones leave it out (Mobile-Agent shows four tiles). */
  desktopOnly?: boolean;
}

function tilesOf(s: AgentStats): Tile[] {
  return [
    { label: 'Tasks finished', short: 'Tasks finished', value: String(s.tasks_finished) },
    {
      label: 'Sent back by review',
      short: 'Sent back',
      value: s.sent_back.ratio === null ? none : formatPercent(s.sent_back.ratio),
      rework: s.sent_back.count > 0,
    },
    {
      label: 'Median turns',
      short: 'Median turns',
      value: s.median_turns === null ? none : String(Math.round(s.median_turns)),
      desktopOnly: true,
    },
    {
      label: 'Spend per task',
      short: 'Spend per task',
      value: s.spend_per_task_usd === null ? none : formatUsd(s.spend_per_task_usd),
    },
    {
      label: 'Time per task, p85',
      short: 'Time, p85',
      value: s.task_time_p85_minutes === null ? none : formatDuration(s.task_time_p85_minutes * 60_000),
    },
  ];
}

/** The five measures of the last 7 days: one row of cells from 720 px, a 2×2 grid of four on phones. */
export function AgentStatsStrip({ stats }: { stats: AgentStats }) {
  return (
    <dl
      aria-label="Last 7 days"
      className={cn(
        'm-0 grid grid-cols-2 gap-2',
        '@[720px]:flex @[720px]:flex-wrap @[720px]:gap-0 @[720px]:overflow-hidden @[720px]:rounded-card @[720px]:border @[720px]:border-line @[720px]:bg-card',
      )}
    >
      {tilesOf(stats).map((t) => (
        <div
          key={t.label}
          className={cn(
            'flex min-w-0 flex-col gap-1 rounded-card border border-line bg-card p-3',
            '@[720px]:flex-[1_1_150px] @[720px]:rounded-none @[720px]:border-0 @[720px]:border-r @[720px]:px-[18px] @[720px]:py-3.5 @[720px]:last:border-r-0',
            t.desktopOnly && 'hidden @[720px]:flex',
          )}
        >
          <dt className="text-[13px] text-muted">
            <span className="@[720px]:hidden">{t.short}</span>
            <span className="hidden @[720px]:inline">{t.label}</span>
          </dt>
          <dd
            className={cn(
              'm-0 text-[28px] leading-tight font-normal tracking-display tabular-nums @[720px]:text-[30px]',
              t.rework && 'text-rework',
            )}
          >
            {t.value}
          </dd>
        </div>
      ))}
    </dl>
  );
}

/** "Why review sent work back": each reason with a hatched rust bar against the most frequent one. */
export function SendBackReasons({ stats, className }: { stats: AgentStats; className?: string }) {
  return (
    <SectionCard title="Why review sent work back" className={className}>
      {stats.reasons.length === 0 ? (
        <p className="m-0 text-[15px] text-muted">Nothing was sent back in the last 7 days.</p>
      ) : (
        <ul className="m-0 flex list-none flex-col gap-3 p-0">
          {stats.reasons.map((r) => (
            <li key={r.label} className="flex flex-col gap-1.5">
              <div className="flex justify-between gap-2.5 text-[14px] md:text-[15px]">
                <span className="min-w-0">{r.label}</span>
                <span className="shrink-0 text-rework-ink tabular-nums">
                  {r.count}×<span className="sr-only"> sent back</span>
                </span>
              </div>
              <Meter
                value={reasonShare(r, stats.reasons)}
                max={1}
                tone="rework"
                className="h-2 border border-hairline"
              />
            </li>
          ))}
        </ul>
      )}
    </SectionCard>
  );
}

/** The tool calls the agent's policy blocked; D12 shows it under its Tool policy card. */
export function BlockedCalls({ stats, className }: { stats: AgentStats; className?: string }) {
  return <p className={cn('m-0 text-[13px] text-muted', className)}>{blockedSentence(stats.denied_tool_calls)}</p>;
}

/** The page's header: where the agent sits, its name, what it does and where it runs. */
export function AgentHeader({ stats, actions }: { stats: AgentStats; actions?: ReactNode }) {
  const factory = useFactory();
  const params = useFactoryParams();
  return (
    <header className="flex flex-wrap items-end justify-between gap-4">
      <div className="flex min-w-0 flex-col gap-2">
        <nav aria-label="Breadcrumb" className="hidden text-sm text-muted md:block">
          <ol className="m-0 flex list-none gap-1.5 p-0">
            <li>
              <Link to="/o/$org/f/$factory" params={params} className="text-muted hover:text-run">
                {factoryTitle(factory)}
              </Link>
            </li>
            <li aria-hidden="true">/</li>
            <li>Agents</li>
            <li aria-hidden="true">/</li>
            <li aria-current="page">{stats.agent}</li>
          </ol>
        </nav>
        <h1 className="m-0 text-[34px] leading-[1.05] font-normal tracking-display break-words md:text-5xl">
          {stats.agent}
        </h1>
        <p className="m-0 max-w-[64ch] text-[14px] leading-[1.45] text-muted md:text-base">{agentSentence(stats)}</p>
      </div>
      {actions}
    </header>
  );
}

/**
 * The agent page. `actions` sits beside the title (D12: Edit in Definition); `cards` come before
 * the reasons in the grid (D12: Connections, Skills, Tool policy, which then shows BlockedCalls).
 */
export function AgentPage({ actions, cards }: { actions?: ReactNode; cards?: ReactNode } = {}) {
  const factory = useFactory();
  const org = useOrg();
  const { agent } = useParams({ from: '/o/$org/f/$factory/agents/$agent' });
  const { data: stats } = useSuspenseQuery(agentStatsQuery(org.id, factory.id, agent));

  return (
    <>
      <AgentHeader stats={stats} actions={actions} />
      <AgentStatsStrip stats={stats} />
      {cards ? null : <BlockedCalls stats={stats} className="-mt-1" />}
      <div className="grid items-start gap-3 md:gap-4 @[720px]:grid-cols-[repeat(auto-fit,minmax(min(100%,380px),1fr))]">
        {cards}
        <SendBackReasons stats={stats} />
      </div>
    </>
  );
}
