// Environments (ADR 0037 decision 14, canvas build/definition-environments): a tab per
// environments/<name>.yaml with the agents that use it, then the box, the command-line tools, the
// variables, the files, the MCP servers and the checks. Every edit writes the environment file of the
// draft (use-environments.ts), so the YAML view shows the same file. The tab is `?environment=`.
import { Link, useNavigate } from '@tanstack/react-router';
import { Plus } from 'lucide-react';
import { useId, useState } from 'react';
import { useFactory, useFactoryParams } from '../../../shared/factory';
import { plural } from '../../../shared/format';
import { useOrg } from '../../../shared/org';
import { Button } from '../../../ui/button';
import { Callout } from '../../../ui/callout';
import { cn } from '../../../ui/cn';
import { EmptyState } from '../../../ui/empty-state';
import { Skeleton } from '../../../ui/skeleton';
import { TabNav, tabNavUnderlineItem } from '../../../ui/tab-nav';
import { useDraftSession, useViewOnly } from '../draft/use-draft';
import { bareSection, SectionShell } from '../frame/section-shell';
import type { SectionViewProps } from '../section-views';
import { BoxCard, FilesCard, ToolsCard, VariablesCard } from './box-cards';
import { defaultEnvironment, envPath, newEnvText } from './env-file';
import { ChecksCard, InlineProblems, useFixes } from './environment-checks';
import { McpCard } from './mcp-card';
import { NewSecretSheetFor } from './new-secret';
import { CommitInput, useEnvironmentTest } from './parts';
import { useEnvironment, useEnvironmentAgents, useEnvironmentNames } from './use-environments';

const slug = /^[a-z][a-z0-9-]{1,38}[a-z0-9]$/;

export function EnvironmentsSection({ search }: SectionViewProps) {
  const org = useOrg();
  const params = useFactoryParams();
  const viewOnly = useViewOnly();
  const { names, ready, error, refetch } = useEnvironmentNames();
  const agents = useEnvironmentAgents();
  const current =
    search.environment && names.includes(search.environment) ? search.environment : (names[0] ?? defaultEnvironment);

  return (
    <SectionShell
      section="environments"
      className={bareSection}
      description={
        <>
          What each agent’s box installs and what it can reach. Keys come from{' '}
          <Link to="/o/$org/secrets" params={{ org: org.slug }} className="text-ink underline underline-offset-2">
            Secrets
          </Link>
          .
        </>
      }
      aside={
        viewOnly ? null : (
          <Button asChild variant="white">
            <Link
              to="/o/$org/f/$factory/definition/add-tool"
              params={{ org: org.slug, factory: params.factory }}
              search={{ environment: current }}
            >
              <Plus size={16} strokeWidth={1.75} aria-hidden="true" />
              Add a tool
            </Link>
          </Button>
        )
      }
    >
      {error ? (
        <div role="alert" className="flex flex-col items-start gap-3">
          <Callout tone="rework">The environments did not load.</Callout>
          <Button variant="white" onClick={() => void refetch()}>
            Try again
          </Button>
        </div>
      ) : (
        <Skeleton ready={ready} placeholder={<EnvironmentsPlaceholder />}>
          <Tabs names={names} current={current} agents={agents ?? {}} viewOnly={viewOnly} />
          {names.length === 0 ? (
            <EmptyState title="No environments yet">
              Create one to say what an agent’s box installs and can reach.
            </EmptyState>
          ) : (
            <EnvironmentView key={current} name={current} agents={agents?.[current] ?? []} />
          )}
        </Skeleton>
      )}
    </SectionShell>
  );
}

function EnvironmentsPlaceholder() {
  const block = 'rounded-control bg-hover';
  return (
    <div className="flex flex-col gap-4">
      <div className={cn(block, 'h-10 w-72 max-w-full')} />
      <div className={cn(block, 'h-56')} />
      <div className={cn(block, 'h-24')} />
      <div className={cn(block, 'h-40')} />
    </div>
  );
}

function Tabs({
  names,
  current,
  agents,
  viewOnly,
}: {
  names: string[];
  current: string;
  agents: Record<string, string[]>;
  viewOnly: boolean;
}) {
  const org = useOrg();
  const params = useFactoryParams();
  const session = useDraftSession();
  const navigate = useNavigate();
  const [creating, setCreating] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const errorId = useId();

  const create = (value: string) => {
    const name = value.trim();
    if (name === '') return setCreating(false);
    if (!slug.test(name)) return setProblem('Lowercase letters, digits and dashes, 3 to 40 characters.');
    if (names.includes(name)) return setProblem(`${name} already exists.`);
    session.editFile(envPath(name), newEnvText(name));
    setCreating(false);
    setProblem(null);
    void navigate({
      to: '/o/$org/f/$factory/definition/$section',
      params: { org: org.slug, factory: params.factory, section: 'environments' },
      search: { environment: name },
    });
  };

  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1 border-b border-line">
      <TabNav label="Environments" variant="underline" className="border-b-0">
        {names.map((name) => (
          <Link
            key={name}
            to="/o/$org/f/$factory/definition/$section"
            params={{ org: org.slug, factory: params.factory, section: 'environments' }}
            search={{ environment: name }}
            aria-current={name === current ? 'page' : undefined}
            className={cn(tabNavUnderlineItem, 'flex-col items-start justify-center gap-0 no-underline')}
          >
            <span className="font-mono">{name}</span>
            <span className="text-xs font-normal text-muted">{plural(agents[name]?.length ?? 0, 'agent')}</span>
          </Link>
        ))}
      </TabNav>
      {viewOnly ? null : creating ? (
        <div className="flex flex-col py-1">
          <CommitInput
            aria-label="New environment name"
            value=""
            autoFocus
            placeholder="deploy"
            invalid={problem !== null}
            describedBy={problem ? errorId : undefined}
            className="w-44"
            onCommit={create}
          />
          {problem ? (
            <p id={errorId} role="alert" className="m-0 mt-1 text-secondary text-rework-ink">
              {problem}
            </p>
          ) : null}
        </div>
      ) : (
        <button
          type="button"
          onClick={() => setCreating(true)}
          className="press inline-flex min-h-11 cursor-pointer items-center gap-2 px-3 text-body text-ink transition-colors duration-(--duration-quick) hover:text-link md:min-h-10"
        >
          <Plus size={14} strokeWidth={1.75} aria-hidden="true" />
          New environment
        </button>
      )}
    </div>
  );
}

function EnvironmentView({ name, agents }: { name: string; agents: string[] }) {
  const viewOnly = useViewOnly();
  const factory = useFactory();
  const { spec, change, ready } = useEnvironment(name);
  const { results, test, run } = useEnvironmentTest(name);
  const secret = NewSecretSheetFor();
  const fixes = useFixes({ spec, change });
  const testing = test.isPending ? (test.variables?.server ?? '') : undefined;
  const props = { spec, change, readOnly: viewOnly, newSecret: secret.open };

  return (
    <Skeleton ready={ready} placeholder={<EnvironmentsPlaceholder />}>
      <div className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center gap-2 text-secondary">
          <span className="text-muted">Used by</span>
          {agents.length === 0 ? (
            <span className="text-muted">no agent yet.</span>
          ) : (
            <ul className="m-0 flex list-none flex-wrap gap-2 p-0">
              {agents.map((a) => (
                <li key={a} className="rounded-chip bg-hover px-3 py-1 text-ink">
                  {a}
                </li>
              ))}
            </ul>
          )}
          {name === defaultEnvironment ? (
            <span className="text-muted">An agent without an environment gets default.</span>
          ) : null}
        </div>
        <BoxCard {...props} />
        <ToolsCard {...props} />
        <VariablesCard
          {...props}
          checks={<InlineProblems environment={name} apply={fixes.apply} readOnly={viewOnly} />}
        />
        <FilesCard {...props} />
        <McpCard
          environment={name}
          spec={spec}
          change={change}
          readOnly={viewOnly}
          results={results}
          testing={testing}
          testServer={(server) => run({ server })}
        />
        {test.isError ? (
          <div role="alert">
            <Callout tone="rework">
              {test.error instanceof Error ? test.error.message : 'The test did not run.'}
            </Callout>
          </div>
        ) : null}
        <ChecksCard
          environment={name}
          spec={spec}
          apply={fixes.apply}
          readOnly={viewOnly}
          results={results}
          factoryName={factory.name}
          testAll={() => run()}
          testing={test.isPending}
        />
      </div>
      {fixes.sheet}
      {secret.sheet}
    </Skeleton>
  );
}
