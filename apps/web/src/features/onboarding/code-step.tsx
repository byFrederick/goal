// Step 2, "Connect your code" (docs/design/screens/get-started/github.jpg, ADR 0032). The organization's
// GitHub connection, or a link to add one; the owner whose repositories are listed; the repositories
// to work on, chosen by checkbox. The choice lives in the URL (code-search.ts), so step 3 reads it and a
// linked step shows the same thing. Continue carries it on.
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { Check, Search } from 'lucide-react';
import { useState } from 'react';
import { ApiError } from '../../api/client';
import { connectionRepositoriesQuery, connectionsQuery } from '../../api/queries';
import type { Connection, OrgSummary, Repository, RepositoryList } from '../../api/types';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { Field, NativeSelect } from '../../ui/field';
import { GitHubMark } from '../../ui/github-mark';
import { NotAvailable } from '../../ui/not-available';
import type { OnboardingSearch } from './code-search';
import { codeCard, organizationCard } from './factory-preview';
import { OnboardingLayout } from './onboarding-layout';

const day = 86_400_000;

const plural = (n: number, unit: string) => `${n} ${unit}${n === 1 ? '' : 's'}`;

/** "Updated today", "Updated 3 weeks ago", "Updated 2 months ago": the age of the last push, to the unit that reads. */
function updatedLabel(updatedAt: string, now = Date.now()): string {
  const days = Math.floor((now - Date.parse(updatedAt)) / day);
  if (!(days >= 1)) return 'Updated today';
  if (days < 7) return `Updated ${plural(days, 'day')} ago`;
  if (days < 31) return `Updated ${plural(Math.floor(days / 7), 'week')} ago`;
  if (days < 365) return `Updated ${plural(Math.floor(days / 30), 'month')} ago`;
  return `Updated ${plural(Math.floor(days / 365), 'year')} ago`;
}

/** `search` is the URL's choice (code-search.ts): the route passes it, so the step reads it the same way in a test. */
export function CodeStep({ org, search }: { org: OrgSummary; search: OnboardingSearch }) {
  const navigate = useNavigate();
  const [text, setText] = useState('');
  /** This browser's host, the start of the address the organization gets (as step 1 shows it). */
  const host = window.location.host;

  const connections = useQuery(connectionsQuery(org.id));
  const githubs = (connections.data ?? []).filter((c) => c.kind === 'github');
  const connection: Connection | undefined = githubs.find((c) => c.name === search.connection) ?? githubs[0];

  const repos = useQuery({
    ...connectionRepositoriesQuery(org.id, connection?.name ?? '', search.owner),
    enabled: connection !== undefined,
    placeholderData: keepPreviousData,
  });
  const list: RepositoryList | undefined = repos.data;

  // Only the repositories the list shows count: a link's stale name is not chosen.
  const chosen = (list?.repositories ?? [])
    .map((r) => r.full_name)
    .filter((name) => (search.repos ?? []).includes(name));
  const count = chosen.length;

  const toggle = (name: string) => {
    const next = chosen.includes(name) ? chosen.filter((r) => r !== name) : [...chosen, name];
    void navigate({
      to: '/o/$org/get-started/$step',
      params: { org: org.slug, step: 'code' },
      search: { ...search, repos: next.length > 0 ? next : undefined },
      replace: true,
    });
  };

  /** A new owner's list has its own choice: the repositories chosen under the last one are dropped. */
  const chooseOwner = (owner: string | undefined) => {
    void navigate({
      to: '/o/$org/get-started/$step',
      params: { org: org.slug, step: 'code' },
      search: { ...search, owner, repos: undefined },
      replace: true,
    });
  };

  const loading = connections.isPending || (connection !== undefined && repos.isPending);
  const loadFailed = connections.isError || (connection !== undefined && repos.isError && !list);

  return (
    <OnboardingLayout
      step="code"
      org={org}
      back={
        <Button asChild size="lg" variant="secondary">
          <Link to="/get-started">Back</Link>
        </Button>
      }
      action={
        <Button
          type="button"
          size="lg"
          variant="primary"
          disabled={count === 0 || !list}
          onClick={() => {
            if (!list || count === 0 || !connection) return;
            void navigate({
              to: '/o/$org/get-started/$step',
              params: { org: org.slug, step: 'factory' },
              search: { connection: connection.name, owner: list.owner, repos: chosen },
            });
          }}
        >
          {`Continue with ${count} ${count === 1 ? 'repository' : 'repositories'}`}
        </Button>
      }
      preview={{ organization: organizationCard(org, host), second: codeCard(chosen[0]) }}
    >
      <div className="flex flex-col gap-2">
        <h1 className="m-0 font-numeral text-kpi tracking-display">Connect your code</h1>
        <p className="m-0 text-heading font-normal text-ink-soft">
          Agents clone the repositories you choose, work on branches and open pull requests.
        </p>
      </div>

      {loading ? (
        <div aria-busy="true">
          <span role="status" className="sr-only">
            Loading
          </span>
        </div>
      ) : (
        <div className="flex flex-col gap-6">
          {connections.isError ? null : <GitHubCard org={org} connection={connection} login={list?.login} />}

          {loadFailed ? (
            <div role="alert" className="flex flex-col items-start gap-3">
              <Callout tone="rework">
                {`Couldn’t load the repositories. ${(connections.error ?? repos.error)?.message ?? ''}`.trim()}
              </Callout>
              {repos.error instanceof ApiError && repos.error.code === 'owner-not-found' ? (
                <Button variant="secondary" onClick={() => chooseOwner(undefined)}>
                  Use your account
                </Button>
              ) : (
                <Button
                  variant="secondary"
                  onClick={() => void (connections.isError ? connections.refetch() : repos.refetch())}
                >
                  Retry
                </Button>
              )}
            </div>
          ) : connection === undefined ? null : list ? (
            <>
              <Field label="Owner">
                <NativeSelect value={list.owner} onChange={(e) => chooseOwner(e.target.value)}>
                  {list.owners.map((o) => (
                    <option key={o.login} value={o.login}>
                      {o.kind === 'user' && o.login === list.login ? `${o.login}, your account` : o.login}
                    </option>
                  ))}
                </NativeSelect>
              </Field>

              <section aria-labelledby="repositories-label" className="flex flex-col gap-2">
                <p id="repositories-label" className="m-0 text-sm font-medium text-ink">
                  Repositories
                </p>
                <div className="overflow-hidden rounded-card border border-line bg-card">
                  {list.repositories.length === 0 ? (
                    <p className="m-0 px-4 py-6 text-body text-muted">
                      {`${list.owner} has no repositories the Goal app can reach.`}
                    </p>
                  ) : (
                    <RepositoryRows
                      repositories={list.repositories}
                      chosen={chosen}
                      text={text}
                      onText={setText}
                      onToggle={toggle}
                    />
                  )}
                </div>
                <div className="text-secondary text-muted">
                  Missing one?{' '}
                  <a
                    href="https://github.com/settings/installations"
                    target="_blank"
                    rel="noreferrer"
                    className="font-control no-underline"
                  >
                    Give the Goal app access on GitHub
                  </a>
                  . <GitLabComing />
                </div>
              </section>

              <div className="grid grid-cols-1 gap-4 text-secondary sm:grid-cols-3">
                <Access title="Reads" tone="text-ok-ink" text="Code, issues, checks" />
                <Access
                  title="Writes"
                  tone="text-run-ink"
                  text="Branches and pull requests; comments only when you approve"
                />
                <Access title="Never" tone="text-rework-ink" text="Force pushes, deletes or settings" />
              </div>
            </>
          ) : null}
        </div>
      )}
    </OnboardingLayout>
  );
}

/** The GitHub card: connected as the account the credential signs in as, or a link to connect one. */
function GitHubCard({
  org,
  connection,
  login,
}: {
  org: OrgSummary;
  connection: Connection | undefined;
  login: string | undefined;
}) {
  return (
    <section
      aria-label="GitHub"
      className="flex flex-wrap items-center gap-3 rounded-card border border-line bg-card p-4"
    >
      <span
        aria-hidden="true"
        className="flex size-9 shrink-0 items-center justify-center rounded-control bg-ink text-shell"
      >
        <GitHubMark />
      </span>
      <div className="flex min-w-0 flex-1 flex-col">
        <span className="font-control text-body">GitHub</span>
        <span className="text-secondary text-muted">
          {connection === undefined ? 'Not connected yet' : login ? `Connected as ${login}` : connection.name}
        </span>
      </div>
      {connection === undefined ? (
        <Button asChild variant="primary">
          <Link
            to="/o/$org/connections/new"
            params={{ org: org.slug }}
            search={{ kind: 'github', next: `/o/${org.slug}/get-started/code` }}
          >
            Connect GitHub
          </Link>
        </Button>
      ) : (
        <>
          {login ? (
            <Chip tone="ok">
              <Check size={12} strokeWidth={2.25} aria-hidden="true" />
              Connected
            </Chip>
          ) : null}
          <Button asChild variant="secondary">
            <Link to="/o/$org/connections/$connection" params={{ org: org.slug, connection: connection.name }}>
              Switch account
            </Link>
          </Button>
        </>
      )}
    </section>
  );
}

/** The search box and the checkable rows. The selected rows take the selection tint. */
function RepositoryRows({
  repositories,
  chosen,
  text,
  onText,
  onToggle,
}: {
  repositories: Repository[];
  chosen: string[];
  text: string;
  onText: (text: string) => void;
  onToggle: (name: string) => void;
}) {
  const q = text.trim().toLowerCase();
  const shown = repositories.filter((r) => r.full_name.toLowerCase().includes(q));
  const label = `Search ${repositories.length} repositories`;
  return (
    <>
      <label className="flex h-11 items-center gap-2 px-4 text-muted">
        <Search size={16} strokeWidth={1.5} aria-hidden="true" className="shrink-0" />
        <input
          type="search"
          aria-label={label}
          placeholder={label}
          value={text}
          onChange={(e) => onText(e.target.value)}
          autoComplete="off"
          className="min-w-0 flex-1 bg-transparent text-body text-ink outline-none placeholder:text-muted"
        />
      </label>
      {shown.length === 0 ? (
        <p className="m-0 border-t border-line px-4 py-4 text-secondary text-muted">{`No repository matches “${text.trim()}”.`}</p>
      ) : (
        shown.map((r) => {
          const checked = chosen.includes(r.full_name);
          return (
            <label
              key={r.full_name}
              className={cn(
                'flex cursor-pointer items-center gap-3 border-t border-line px-4 py-3 hover:bg-hover',
                checked && 'bg-run-tint hover:bg-run-tint',
              )}
            >
              <input
                type="checkbox"
                checked={checked}
                onChange={() => onToggle(r.full_name)}
                className="size-4 accent-run"
              />
              <span className="flex min-w-0 flex-col">
                <span className="font-control text-body break-all">{r.full_name}</span>
                {r.languages.length > 0 ? (
                  <span className="text-secondary text-muted">{r.languages.join(', ')}</span>
                ) : null}
              </span>
              <span className="text-secondary whitespace-nowrap text-muted">{updatedLabel(r.updated_at)}</span>
            </label>
          );
        })
      )}
    </>
  );
}

/** GitLab is not built: its link is the disabled control, which says so on hover and focus. */
function GitLabComing() {
  return (
    <NotAvailable label="GitLab">
      <button type="button" className="cursor-pointer bg-transparent p-0 text-secondary text-muted">
        GitLab is coming.
      </button>
    </NotAvailable>
  );
}

/** One of the three access notes: what the agents read, write, or never do. */
function Access({ title, tone, text }: { title: string; tone: string; text: string }) {
  return (
    <div className="flex flex-col gap-1">
      <span className={cn('font-control', tone)}>{title}</span>
      <span className="text-ink-soft">{text}</span>
    </div>
  );
}
