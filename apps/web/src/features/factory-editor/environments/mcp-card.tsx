// MCP servers: each with its endpoint, the secrets it reads, what Test found, and per tool Allow, Ask
// first and Off (written as `tools:` of the server in the environment file). The tools a test found are
// grouped as Read, Change and Delete by their names; before a test, the file's own rules show.
import { Link } from '@tanstack/react-router';
import { Loader2 } from 'lucide-react';
import { useMemo, useState } from 'react';
import type { ServerTest } from '../../../api/types';
import { useFactoryParams } from '../../../shared/factory';
import { plural } from '../../../shared/format';
import { useOrg } from '../../../shared/org';
import { Button } from '../../../ui/button';
import { cn } from '../../../ui/cn';
import { Disclosure } from '../../../ui/disclosure';
import { type EnvSpec, type McpServer, modeOf, removeServer, secretsIn, setToolModes, type ToolMode } from './env-file';
import { Card, Initials, ModeControl, rowPad, secretMark } from './parts';
import { RuleList, RulesPlaceholder, useServerRules } from './standing-rules';

export type ToolGroup = 'Read' | 'Change' | 'Delete';

const reads = /^(get|list|search|find|read|fetch|describe|query|view|show)([_-]|$)/;
const deletes = /^(delete|remove|archive|destroy|drop)([_-]|$)/;

/** A tool's group: what it only reads, what it changes, what it deletes. */
export function groupOf(tool: string): ToolGroup {
  if (reads.test(tool)) return 'Read';
  if (deletes.test(tool)) return 'Delete';
  return 'Change';
}

/** The tools of a test answer by group, in the order Read, Change, Delete; empty groups left out. */
export function groupTools(tools: readonly string[]): { group: ToolGroup; tools: string[] }[] {
  return (['Read', 'Change', 'Delete'] as const)
    .map((group) => ({ group, tools: tools.filter((t) => groupOf(t) === group) }))
    .filter((g) => g.tools.length > 0);
}

/** "list_issues, get_issue and 9 more": the first names of a group. */
export function namesText(tools: readonly string[], shown = 3): string {
  const names = tools.slice(0, shown).join(', ');
  return tools.length > shown ? `${names} and ${tools.length - shown} more` : names;
}

/** The mode every tool of a group has, or undefined when they differ. */
function groupMode(server: McpServer, tools: readonly string[]): ToolMode | undefined {
  const modes = new Set(tools.map((t) => modeOf(server, t)));
  return modes.size === 1 ? [...modes][0] : undefined;
}

/** "12 allowed, 1 ask first, 2 off". */
export function countsText(server: McpServer, tools: readonly string[]): string {
  const n = { allow: 0, ask: 0, off: 0 };
  for (const t of tools) n[modeOf(server, t)]++;
  return [n.allow && `${n.allow} allowed`, n.ask && `${n.ask} ask first`, n.off && `${n.off} off`]
    .filter(Boolean)
    .join(', ');
}

/** What a server is reached at: its URL without the scheme, or its command. */
export const endpointText = (s: McpServer) => s.url?.replace(/^https?:\/\//, '') ?? s.command?.join(' ') ?? '';

/** The tool names a server's rules and test answer name, rules without a glob first. */
export function knownTools(server: McpServer, test: ServerTest | undefined): string[] {
  return test?.ok ? test.tools : Object.keys(server.tools);
}

function statusOf(test: ServerTest | undefined, testing: boolean): string {
  if (testing) return 'Testing…';
  if (!test) return 'Not tested';
  if (!test.tested) return 'Runs in the box';
  return test.ok ? `${test.tools.length} found` : `Failed: ${test.error ?? 'no answer'}`;
}

function StatusLine({ test, testing }: { test: ServerTest | undefined; testing: boolean }) {
  const text = statusOf(test, testing);
  // When the status changes in place the new text swaps in (MOTION.md, A status changes in place); the
  // first draw does not animate, so a list of cards does not move when it appears.
  const [seen, setSeen] = useState({ text, generation: 0 });
  let { generation } = seen;
  if (seen.text !== text) {
    generation += 1;
    setSeen({ text, generation });
  }
  const swap = generation > 0 && 'motion-safe:animate-text-states-swap motion-reduce:animate-fade-in';
  if (testing) {
    return (
      <span key={generation} className={cn('inline-flex items-center gap-2 text-secondary text-muted', swap)}>
        <Loader2 size={13} aria-hidden="true" className="animate-spin" />
        {text}
      </span>
    );
  }
  if (!test || !test.tested)
    return (
      <span key={generation} className={cn('text-secondary text-muted', swap)}>
        {text}
      </span>
    );
  return (
    <span
      key={generation}
      role="status"
      className={cn(
        'inline-flex items-center gap-2 text-secondary',
        test.ok ? 'text-ink-soft' : 'text-rework-ink',
        swap,
      )}
    >
      <span aria-hidden="true" className={cn('size-1.5 rounded-full', test.ok ? 'bg-ok' : 'bg-rework')} />
      {text}
    </span>
  );
}

export function McpCard({
  place = 'definition',
  environment,
  spec,
  change,
  readOnly,
  results,
  testing,
  testServer,
}: {
  place?: 'definition' | 'wizard';
  environment: string;
  spec: EnvSpec;
  change: (edit: (text: string) => string) => void;
  readOnly: boolean;
  results: Record<string, ServerTest>;
  /** The server being tested, or '' for all of them. */
  testing: string | undefined;
  testServer: (server: string) => void;
}) {
  const servers = Object.entries(spec.mcp);
  const said = Object.values(results)
    .map(
      (t) =>
        `${t.server}: ${t.ok ? `${t.tools.length} found` : t.tested ? `failed, ${t.error ?? 'no answer'}` : 'runs in the box'}`,
    )
    .join('. ');
  return (
    <Card title="MCP servers" hint="Each tool: Allow, Ask first or Off">
      {/* The answers are read out as they arrive: the rows themselves sit inside buttons. */}
      <p role="status" className="sr-only">
        {said}
      </p>
      {servers.length === 0 ? (
        <p className={cn('m-0 py-4 text-body text-muted', rowPad)}>None. Add one from the catalog or write your own.</p>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          {servers.map(([name, server]) => (
            <li key={name} className="border-b border-line last:border-b-0">
              <ServerRow
                name={name}
                environment={environment}
                server={server}
                test={results[name]}
                testing={testing === name || testing === ''}
                readOnly={readOnly}
                change={change}
                testServer={() => testServer(name)}
              />
            </li>
          ))}
        </ul>
      )}
      {readOnly || place !== 'definition' ? null : (
        <div className={cn('border-t border-line py-1', rowPad)}>
          <AddServerLink environment={environment} />
        </div>
      )}
    </Card>
  );
}

function ServerRow({
  name,
  environment,
  server,
  test,
  testing,
  readOnly,
  change,
  testServer,
}: {
  name: string;
  environment: string;
  server: McpServer;
  test: ServerTest | undefined;
  testing: boolean;
  readOnly: boolean;
  change: (edit: (text: string) => string) => void;
  testServer: () => void;
}) {
  const tools = useMemo(() => knownTools(server, test), [server, test]);
  const groups = groupTools(tools);
  const { rules, loading } = useServerRules(environment, name);
  // A rule's tool sits under its group; one the server no longer lists goes after the last group, never hidden.
  const listed = new Set(tools);
  const orphans = rules.filter((r) => !listed.has(r.tool));
  const secrets = [...new Set([...Object.values(server.headers), ...Object.values(server.env)].flatMap(secretsIn))];
  const counts = countsText(server, tools);
  // The Test button sits in the card's body, so a card that was just tested opens to show it (and its
  // tools); the person can close it again.
  const [open, setOpen] = useState(false);
  const [answered, setAnswered] = useState(Boolean(test));
  if (Boolean(test) !== answered) {
    setAnswered(Boolean(test));
    if (test) setOpen(true);
  }
  return (
    <Disclosure
      open={open}
      onOpenChange={setOpen}
      triggerClassName="items-start py-3 md:items-center"
      summary={
        <span className="flex min-w-0 flex-1 items-start gap-3 md:items-center">
          <Initials name={name} />
          <span className="flex min-w-0 flex-1 flex-col gap-1 md:flex-row md:items-center md:justify-between md:gap-4">
            <span className="flex min-w-0 flex-col gap-1">
              <span className="truncate font-control" title={name}>
                {name}
              </span>
              <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-secondary text-muted">
                <span className="truncate font-mono" title={endpointText(server)}>
                  {endpointText(server)}
                </span>
                {secrets.map((s) => (
                  <span
                    key={s}
                    className="inline-flex items-center gap-1 rounded-chip bg-hover px-2 font-mono text-meta text-ink"
                  >
                    {secretMark}
                    {s}
                  </span>
                ))}
                {server.command ? <span>Runs in the box</span> : secrets.length === 0 ? <span>No key</span> : null}
                {counts ? <span>{counts}</span> : null}
              </span>
            </span>
            <StatusLine test={test} testing={testing} />
          </span>
        </span>
      }
    >
      <div className={cn('flex flex-col gap-3 pb-4 pl-[3.25rem] pr-4 md:pr-5 max-md:pl-4')}>
        {groups.length === 0 ? (
          <>
            <p className="m-0 text-secondary text-muted">
              {test && !test.ok
                ? 'Fix the answer above, then test again to list its tools.'
                : 'Test the server to list its tools and set each one.'}
            </p>
            <RuleList rules={rules} />
          </>
        ) : (
          <ul className="m-0 flex list-none flex-col p-0">
            {groups.map(({ group, tools: list }) => (
              <li key={group} className="flex flex-col gap-2 border-b border-line py-3 last:border-b-0">
                <div className="flex flex-col gap-2 md:flex-row md:items-center md:justify-between md:gap-4">
                  <span className="min-w-0 text-secondary">
                    <span className="font-control">{group}</span>{' '}
                    <span className="text-muted">
                      {plural(list.length, 'tool')}: <span className="font-mono">{namesText(list)}</span>
                    </span>
                  </span>
                  <ModeControl
                    label={`${group} tools of ${name}`}
                    value={groupMode(server, list)}
                    readOnly={readOnly}
                    onChange={(mode) => change((t) => setToolModes(t, name, list, mode))}
                  />
                </div>
                <RuleList rules={rules.filter((r) => list.includes(r.tool))} />
              </li>
            ))}
            {orphans.length > 0 ? (
              <li className="border-b border-line py-3 last:border-b-0">
                <RuleList rules={orphans} />
              </li>
            ) : null}
          </ul>
        )}
        {loading ? <RulesPlaceholder /> : null}
        <div className="flex flex-wrap items-center gap-2">
          <Button variant="white" size="sm" onClick={testServer} disabled={testing} aria-label={`Test ${name}`}>
            {test ? 'Test again' : 'Test'}
          </Button>
          {readOnly ? null : (
            <Button
              variant="danger"
              size="sm"
              onClick={() => change((t) => removeServer(t, name))}
              aria-label={`Remove ${name}`}
            >
              Remove
            </Button>
          )}
        </div>
      </div>
    </Disclosure>
  );
}

/** Definition only: Add a tool has a page of its own under the factory's Definition. */
function AddServerLink({ environment }: { environment: string }) {
  const params = useFactoryParams();
  const org = useOrg();
  return (
    <Link
      to="/o/$org/f/$factory/definition/add-tool"
      params={{ org: org.slug, factory: params.factory }}
      search={{ environment }}
      className="press inline-flex min-h-11 items-center gap-2 text-body text-ink no-underline hover:text-link md:min-h-8"
    >
      <span aria-hidden="true" className="text-lg leading-none">
        +
      </span>
      Add an MCP server
    </Link>
  );
}
