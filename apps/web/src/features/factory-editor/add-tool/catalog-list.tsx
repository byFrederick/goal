// The catalog of Add a tool: a search, then the templates as MCP servers, command-line tools and APIs, and
// Custom under "Your own". The row of a template the environment already has says "In default".
import { Link } from '@tanstack/react-router';
import { Search } from 'lucide-react';
import { useState } from 'react';
import type { ToolTemplate } from '../../../api/types';
import { useFactoryParams } from '../../../shared/factory';
import { useOrg } from '../../../shared/org';
import { cn } from '../../../ui/cn';
import type { EnvSpec } from '../environments/env-file';
import { Initials } from '../environments/parts';
import { groupTemplates, inEnvironment, kindTitles } from './apply-template';

const heading = 'm-0 px-3 pb-1 pt-4 text-secondary font-control text-muted';

function Row({
  mark,
  title,
  description,
  current,
  note,
  search,
}: {
  mark: string;
  title: string;
  description: string;
  current: boolean;
  note?: string | undefined;
  search: { environment: string; template: string; type?: 'mcp' };
}) {
  const org = useOrg();
  const params = useFactoryParams();
  return (
    <li>
      <Link
        to="/o/$org/f/$factory/definition/add-tool"
        params={{ org: org.slug, factory: params.factory }}
        search={search}
        aria-current={current ? 'true' : undefined}
        className={cn(
          'press flex min-h-11 items-center gap-3 rounded-control px-3 py-2 text-ink no-underline transition-colors duration-(--duration-quick) hover:bg-hover',
          current && 'bg-hover',
        )}
      >
        <Initials name={mark} />
        <span className="flex min-w-0 flex-1 flex-col leading-tight">
          <span className="truncate font-control" title={title}>
            {title}
          </span>
          <span className="truncate text-secondary text-muted" title={description}>
            {description}
          </span>
        </span>
        {note ? (
          <span className="flex shrink-0 items-center gap-2 text-secondary text-ink-soft">
            <span aria-hidden="true" className="size-1.5 rounded-full bg-ok" />
            {note}
          </span>
        ) : null}
      </Link>
    </li>
  );
}

export function CatalogList({
  templates,
  selected,
  environment,
  spec,
  className,
}: {
  templates: readonly ToolTemplate[];
  /** The template's name, or 'custom'. */
  selected: string;
  environment: string;
  spec: EnvSpec;
  className?: string;
}) {
  const [query, setQuery] = useState('');
  const groups = groupTemplates(templates, query);
  const showCustom = query.trim() === '' || 'custom'.includes(query.trim().toLowerCase());
  return (
    <nav aria-label="Catalog" className={cn('flex min-w-0 flex-col gap-1 p-3', className)}>
      <label className="relative block">
        <span className="sr-only">Search MCP servers, CLIs and APIs</span>
        <Search
          size={15}
          strokeWidth={1.75}
          aria-hidden="true"
          className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted"
        />
        <input
          type="search"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search MCP servers, CLIs and API"
          className="h-11 w-full min-w-0 rounded-control border border-rule bg-card pl-9 pr-3 text-base text-ink hover:border-muted md:h-9 md:text-body"
        />
      </label>
      {groups.map(({ kind, templates: list }) => (
        <section key={kind} aria-label={kindTitles[kind]}>
          <h3 className={heading}>{kindTitles[kind]}</h3>
          <ul className="m-0 flex list-none flex-col p-0">
            {list.map((t) => (
              <Row
                key={t.name}
                mark={t.title.slice(0, 2)}
                title={t.title}
                description={t.description}
                current={selected === t.name}
                note={inEnvironment(t, spec) ? `In ${environment}` : undefined}
                search={{ environment, template: t.name }}
              />
            ))}
          </ul>
        </section>
      ))}
      {groups.length === 0 && !showCustom ? (
        <p className="m-0 px-3 py-4 text-body text-muted">Nothing matches “{query}”.</p>
      ) : null}
      {showCustom ? (
        <section aria-label="Your own">
          <h3 className={heading}>Your own</h3>
          <ul className="m-0 flex list-none flex-col p-0">
            <Row
              mark="<>"
              title="Custom"
              description="An MCP server, a CLI or an API"
              current={selected === 'custom'}
              search={{ environment, template: 'custom' }}
            />
          </ul>
        </section>
      ) : null}
    </nav>
  );
}
