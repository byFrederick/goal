// Agents (D §10.7, canvas Define-Agents and New-Agents): the list of the factory's agents with
// where each works, then the chosen one's detail (`?agent=`, else the first). Two columns from
// 760 px of `main`, one below; on phones the list comes first and `?agent=` opens the detail with
// a back link. Definition and the wizard share it (`place`).
import { Bot, ChevronLeft } from 'lucide-react';
import { whereLabel } from '../../../shared/definition/where';
import { cn } from '../../../ui/cn';
import { EmptyState } from '../../../ui/empty-state';
import { useSection } from '../draft/use-draft';
import { SectionShell } from '../frame/section-shell';
import { SearchLink } from '../search-link';
import type { SectionViewProps } from '../section-views';
import { AgentDetail } from './agent-detail';
import { AddAgentButton, CopyAgentButton } from './agent-dialogs';

/** The section's frame without a card of its own: the list and the detail are the cards. */
export const bareShell = 'gap-3.5 rounded-none border-0 bg-transparent p-0 md:p-0';

export function AgentsSection({ place, search }: SectionViewProps) {
  const agents = useSection('agents').agents ?? [];
  const line = useSection('line');
  const chosen = search.agent ? agents.find((a) => a.name === search.agent) : agents[0];

  return (
    <SectionShell section="agents" className={bareShell}>
      <div className="grid items-start gap-4 @[760px]:grid-cols-[264px_minmax(0,1fr)]">
        <nav
          aria-label="Agents"
          className={cn(
            'flex min-w-0 flex-col gap-2.5 rounded-card border border-line bg-card p-3.5',
            search.agent && 'max-md:hidden',
          )}
        >
          <h3 className="m-0 mx-1 text-[15px] font-medium">
            {agents.length === 1 ? '1 agent' : `${agents.length} agents`}
          </h3>
          {agents.length > 0 ? (
            <ul className="m-0 flex list-none flex-col gap-1.5 p-0">
              {agents.map((a) => {
                const current = a.name === chosen?.name;
                return (
                  <li key={a.name}>
                    <SearchLink
                      patch={{ agent: a.name, add: undefined }}
                      aria-current={current ? 'true' : undefined}
                      className={cn(
                        'grid min-h-11 grid-cols-[36px_minmax(0,1fr)] items-center gap-2.5 rounded-control border bg-surface px-3 py-2.5 text-ink no-underline transition-colors duration-200',
                        current
                          ? 'border-ink shadow-[0_0_0_1px_var(--color-ink)]'
                          : 'border-line hover:border-rule-strong',
                      )}
                    >
                      <span
                        aria-hidden="true"
                        className={cn(
                          'flex size-9 items-center justify-center rounded-control',
                          current ? 'bg-ink text-white' : 'bg-control text-ink',
                        )}
                      >
                        <Bot size={18} strokeWidth={1.5} />
                      </span>
                      <span className="min-w-0">
                        <span translate="no" className="block truncate text-[15px] font-medium">
                          {a.name}
                        </span>
                        <span className="block truncate text-[13px] text-muted">{whereLabel(line, a.name)}</span>
                      </span>
                    </SearchLink>
                  </li>
                );
              })}
            </ul>
          ) : null}
          <AddAgentButton />
          {place === 'definition' ? <CopyAgentButton /> : null}
        </nav>

        <div className={cn('flex min-w-0 flex-col gap-3', !search.agent && 'max-md:hidden')}>
          {search.agent ? (
            <SearchLink
              patch={{ agent: undefined, add: undefined }}
              className="inline-flex min-h-11 items-center gap-1 self-start text-[15px] text-ink md:hidden"
            >
              <ChevronLeft size={18} strokeWidth={1.5} aria-hidden="true" />
              All agents
            </SearchLink>
          ) : null}
          {chosen ? (
            <AgentDetail key={chosen.name} agent={chosen} place={place} openPicker={search.add === 'skill'} />
          ) : agents.length === 0 ? (
            <EmptyState title="Add the first agent">
              Agents work at the line’s agent stations. Give one a name, then its role, model and prompt.
            </EmptyState>
          ) : (
            <EmptyState title={`No agent named ${search.agent}`}>
              It may have been renamed or removed. Pick one from the list.
            </EmptyState>
          )}
        </div>
      </div>
    </SectionShell>
  );
}
