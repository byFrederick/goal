// The definition's cards on E's Agent page (D §10.10, canvas Agent and Mobile-Agent): the skills
// copied into the agent's container and its tool policy, read from the version that runs, with
// "Edit in Definition" and a note when the open draft changes the agent. Edits go through
// Definition › Agents; these cards only show what runs.
import { type UseQueryResult, useQueries, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Info } from 'lucide-react';
import { type ReactNode, useId } from 'react';
import { compareVersions } from '../../api/definitions';
import {
  builderMetaQuery,
  defKeys,
  factoryQuery,
  versionFileQuery,
  versionModelQuery,
  versionQuery,
} from '../../api/queries';
import type { VersionDetail } from '../../api/types';
import { Button } from '../../ui/button';
import { SectionCard } from '../../ui/card';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { useFactory, useFactoryParams } from '../factory';
import { useOrg } from '../org';
import { type Model, type ModelAgent, type ModelSkill, modelOf } from './model';
import { cleanPath } from './place';
import { skillDescription } from './skill-md';
import { builtinLabels, legacyRules, toolPolicy } from './tool-policy';

/** The model of the version that runs, by the factory of the page. */
export function useRunningModel(): UseQueryResult<Model> {
  const org = useOrg();
  const factory = useFactory();
  return useQuery({ ...versionModelQuery(org.id, factory.id, 'current'), select: (v) => modelOf(v.model) });
}

/** The running version's files (path and sha256), to read single files by their content. */
function useRunningFiles(enabled: boolean): VersionDetail['files'] | undefined {
  const org = useOrg();
  const factory = useFactory();
  return useQuery({ ...versionQuery(org.id, factory.id, 'current'), enabled }).data?.files;
}

/** Header action: the agent in Definition › Agents. */
export function EditInDefinition({ agent }: { agent: string }) {
  const params = useFactoryParams();
  return (
    <Button asChild variant="secondary" className="h-11 md:h-10">
      <Link to="/o/$org/f/$factory/definition/$section" params={{ ...params, section: 'agents' }} search={{ agent }}>
        Edit in Definition
      </Link>
    </Button>
  );
}

/** "The draft changes this agent", with a link to Review, while the open draft does. */
export function DraftChangesNote({ agent, className }: { agent: string; className?: string }) {
  const org = useOrg();
  const factory = useFactory();
  const params = useFactoryParams();
  // The factory is re-read on arrival (this tab's own saves send no invalidation); the draft's
  // comparison at one revision never changes, so it is keyed by the revision.
  const draft = useQuery({ ...factoryQuery(org.id, factory.id), staleTime: 0 }).data?.draft;
  const compare = useQuery({
    queryKey: [...defKeys.compare(org.id, factory.id, 'base', 'draft'), draft?.revision ?? 0],
    queryFn: ({ signal }) => compareVersions(org.id, factory.id, 'base', 'draft', signal),
    enabled: Boolean(draft),
    staleTime: Number.POSITIVE_INFINITY,
  });
  const changes = draft
    ? (compare.data?.changes.filter((c) => c.target.kind === 'agent' && c.target.id === agent) ?? [])
    : [];
  if (changes.length === 0) return null;
  return (
    <p
      className={cn(
        'm-0 flex flex-wrap items-center gap-x-2 gap-y-1 rounded-control border border-line bg-card px-3.5 py-2.5 text-sm md:text-[15px]',
        className,
      )}
    >
      <Info size={16} strokeWidth={1.75} aria-hidden="true" className="shrink-0 text-run" />
      <span>The draft changes this agent. These cards show the version that runs.</span>
      <Link
        to="/o/$org/f/$factory/definition/$section"
        params={{ ...params, section: 'review' }}
        className="inline-flex min-h-11 items-center text-ink underline underline-offset-3 transition-colors duration-200 hover:text-run md:min-h-6"
      >
        Review the draft
      </Link>
    </p>
  );
}

/** A card's body while the running version loads: rows of the card's shape. */
function CardSkeleton({ rows, label }: { rows: number; label: string }) {
  return (
    <div aria-busy="true" className="flex flex-col gap-2">
      <span className="sr-only">{label}</span>
      {Array.from({ length: rows }, (_, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: placeholders without identity.
        <div key={i} className="h-[52px] rounded-control bg-control/60" />
      ))}
    </div>
  );
}

/** The running version could not be read: say so, and offer to try again. */
function CardError({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="flex flex-wrap items-center gap-3">
      <p className="m-0 text-sm text-rework-ink">The version that runs can’t be read right now.</p>
      <Button variant="secondary" className="h-11 md:h-8" size="sm" onClick={onRetry}>
        Try again
      </Button>
    </div>
  );
}

/** The running agent, or what to show instead of the card's content. */
function useRunningAgent(
  agent: string,
  rows: number,
  loading: string,
): { found?: ModelAgent; model?: Model; body?: ReactNode } {
  const running = useRunningModel();
  if (running.isPending) return { body: <CardSkeleton rows={rows} label={loading} /> };
  if (running.isError) return { body: <CardError onRetry={() => void running.refetch()} /> };
  const found = running.data.agents.agents?.find((a) => a.name === agent);
  if (!found) {
    return {
      body: <p className="m-0 text-sm text-muted">The version that runs has no agent named {agent}.</p>,
    };
  }
  return { found, model: running.data };
}

type SkillSource = 'local' | 'skills.sh';

/** "skills.sh" for a skill vendored from the catalog (or named by its catalog source), else local. */
function sourceOf(skill: ModelSkill | undefined): SkillSource | undefined {
  if (!skill) return undefined;
  return skill.origin || skill.source.startsWith('skills.sh:') ? 'skills.sh' : 'local';
}

/** Where a local skill's SKILL.md is in the version's files. */
const skillMdPath = (skill: ModelSkill | undefined) =>
  skill && !skill.source.startsWith('skills.sh:') ? `${cleanPath(skill.source).replace(/\/+$/, '')}/SKILL.md` : '';

/** The skills copied into this agent's container: name, SKILL.md's description and source. */
export function SkillsCard({ agent, className }: { agent: string; className?: string }) {
  const org = useOrg();
  const factory = useFactory();
  const params = useFactoryParams();
  const { found, model, body } = useRunningAgent(agent, 2, 'Loading skills');
  const skills = (found?.skills ?? []).map((name) => ({
    name,
    skill: model?.agents.skills?.find((s) => s.name === name),
  }));
  const files = useRunningFiles(skills.length > 0);
  const texts = useQueries({
    queries: skills.map(({ skill }) => {
      const path = skillMdPath(skill);
      const sha = files?.find((f) => f.path === path)?.sha256 ?? '';
      return { ...versionFileQuery(org.id, factory.id, 'current', path, sha), enabled: sha !== '' };
    }),
  });

  return (
    <SectionCard
      title="Skills"
      description="Copied into this agent’s container only"
      className={cn('gap-3', className)}
    >
      {body ??
        (skills.length > 0 ? (
          <ul className="m-0 flex list-none flex-col p-0 md:gap-2">
            {skills.map(({ name, skill }, i) => {
              const text = texts[i];
              const description = text?.data ? skillDescription(text.data.text) : undefined;
              const source = sourceOf(skill);
              return (
                <li
                  key={name}
                  className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-2.5 border-t border-line py-[11px] md:rounded-control md:border md:border-hairline md:bg-surface md:px-3 md:py-2.5"
                >
                  <span className="min-w-0">
                    <span translate="no" className="block text-[15px] font-medium break-words">
                      {name}
                    </span>
                    {!skill ? (
                      <span className="mt-0.5 block text-[13px] text-rework-ink">Not among the factory’s skills</span>
                    ) : description ? (
                      <span className="mt-0.5 block text-[13px] leading-snug text-muted break-words">
                        {description}
                      </span>
                    ) : text?.isPending && text.fetchStatus !== 'idle' ? (
                      <span
                        aria-hidden="true"
                        className="mt-1.5 block h-3 w-40 max-w-full rounded-chip bg-control/70"
                      />
                    ) : null}
                  </span>
                  {source ? (
                    <span className="font-mono text-[13px] text-muted">
                      <span className="sr-only">Source: </span>
                      {source}
                    </span>
                  ) : null}
                </li>
              );
            })}
          </ul>
        ) : (
          <p className="m-0 text-sm text-muted">No skills. The agent works from its prompt alone.</p>
        ))}
      <Link
        to="/o/$org/f/$factory/definition/$section"
        params={{ ...params, section: 'agents' }}
        search={{ agent, add: 'skill' }}
        className="inline-flex min-h-11 items-center self-start text-[15px] text-ink underline underline-offset-3 transition-colors duration-200 hover:text-run md:min-h-6"
      >
        Add a skill from skills.sh
      </Link>
    </SectionCard>
  );
}

function Rules({ label, rules, tone }: { label: string; rules: string[]; tone: 'plain' | 'rework' }) {
  const id = useId();
  return (
    <div className="flex flex-col gap-2">
      <p id={id} className="m-0 text-sm">
        {label}
      </p>
      {rules.length > 0 ? (
        <ul aria-labelledby={id} className="m-0 flex list-none flex-wrap gap-1.5 p-0">
          {rules.map((rule) => (
            <li key={rule} className="min-w-0">
              <Chip
                tone={tone}
                className="h-auto min-h-[26px] max-w-full px-2 py-1 font-mono text-xs font-normal whitespace-normal [overflow-wrap:anywhere]"
              >
                <span translate="no">{rule}</span>
              </Chip>
            </li>
          ))}
        </ul>
      ) : (
        <p className="m-0 text-[13px] text-muted">None</p>
      )}
    </div>
  );
}

/** An agent's legacy rules, from the running factory.yaml (read only for such an agent). */
function useLegacyRules(agent: ModelAgent | undefined) {
  const org = useOrg();
  const factory = useFactory();
  const client = useQueryClient();
  const custom = agent?.custom_tools === true;
  const sha = useRunningFiles(custom)?.find((f) => f.path === 'factory.yaml')?.sha256 ?? '';
  const file = versionFileQuery(org.id, factory.id, 'current', 'factory.yaml', sha);
  return useQuery({
    queryKey: [...defKeys.versionFile(org.id, sha), 'tools', agent?.name ?? ''],
    queryFn: async () => (await legacyRules((await client.fetchQuery(file)).text, agent?.name ?? '')) ?? null,
    enabled: custom && sha !== '',
    staleTime: Number.POSITIVE_INFINITY,
  });
}

/**
 * What the agent may call, as goald hands it to the worker: "Allowed" and "Denied before the call
 * runs", in Claude Code's permission syntax. `children` follows them (E's "Blocked this week").
 */
export function ToolPolicyCard({
  agent,
  className,
  children,
}: {
  agent: string;
  className?: string;
  children?: ReactNode;
}) {
  const org = useOrg();
  const { found, body } = useRunningAgent(agent, 2, 'Loading the tool policy');
  const legacy = useLegacyRules(found);
  const approvals = useQuery(builderMetaQuery(org.id)).data?.capabilities.tool_approvals ?? false;
  const policy = found ? toolPolicy(found, legacy.data ?? undefined) : undefined;
  const asked = approvals ? [] : (policy?.asked ?? []);

  return (
    <SectionCard title="Tool policy" className={cn('gap-3', className)}>
      {body ??
        (found?.custom_tools && legacy.isError ? (
          <CardError onRetry={() => void legacy.refetch()} />
        ) : found?.custom_tools && legacy.isPending ? (
          <CardSkeleton rows={2} label="Loading the tool policy" />
        ) : policy ? (
          <>
            <Rules label="Allowed" rules={policy.allowed} tone="plain" />
            <Rules label="Denied before the call runs" rules={policy.denied} tone="rework" />
            {asked.length > 0 ? (
              <p className="m-0 text-[13px] leading-snug text-amber-ink">
                {asked.map((k) => builtinLabels[k]).join(', ')}: Ask me is asked as Off until tool approvals arrive.
              </p>
            ) : null}
          </>
        ) : null)}
      {children}
    </SectionCard>
  );
}
