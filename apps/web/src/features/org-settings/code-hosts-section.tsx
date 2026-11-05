// Organization settings, Code hosts (ADR 0041, decisions 1 and 10): the hosts repository tokens and knowledge
// secrets may go to. github.com and gitlab.com are built in; an owner (org:code_hosts) adds a GitHub
// Enterprise or self-managed GitLab host with its kind. Everyone else sees the list read only. It saves on its
// own: the PUT replaces the added list, so every save is built from the list the server last answered, and a
// host joins the list only once the PUT answers. A new row rises in and a removed one collapses once the
// PUT answers (MOTION.md, A new item appears in a list or column; A rule leaves a list after Revoke).
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { type FormEvent, useRef, useState } from 'react';
import { toast } from 'sonner';
import { ApiError } from '../../api/client';
import { setCodeHosts } from '../../api/code-hosts';
import { codeHostsQuery, keys } from '../../api/queries';
import type { CodeHost, CodeHostKind, CodeHosts } from '../../api/types';
import { useOrg, useOrgHolds } from '../../shared/org';
import { Button } from '../../ui/button';
import { Chip } from '../../ui/chip';
import { Input } from '../../ui/field';
import { InfoTip } from '../../ui/info-tip';
import { Leaving, useLeaving } from '../../ui/leaving';
import { Select } from '../../ui/select';
import { Skeleton } from '../../ui/skeleton';
import { SettingsRow, SettingsSection } from './card';

const kindNames: Record<CodeHostKind, string> = { github: 'GitHub', gitlab: 'GitLab' };
const kindOptions = [
  { value: 'github', label: 'GitHub' },
  { value: 'gitlab', label: 'GitLab' },
];

/** The plain words for a refusal the API gives with a code; any other detail is shown as goald wrote it. */
function refusal(err: ApiError): string {
  if (err.code === 'code-host-built-in') return 'github.com and gitlab.com are always listed';
  return err.detail ?? 'This is not a host goald can reach';
}

const asInput = (h: CodeHost) => ({ host: h.host, kind: h.kind });

export function CodeHostsSection() {
  const org = useOrg();
  const client = useQueryClient();
  const mayEdit = useOrgHolds('org:code_hosts');
  const hosts = useQuery(codeHostsQuery(org.id));
  const [host, setHost] = useState('');
  const [kind, setKind] = useState<CodeHostKind>('github');
  const [problem, setProblem] = useState<string | null>(null);
  // Hosts added in this visit: only these rise in; the rest are there with the section.
  const [fresh, setFresh] = useState<ReadonlySet<string>>(new Set());

  const save = useMutation({
    mutationFn: (next: { host: string; kind: CodeHostKind }[]) => setCodeHosts(org.id, next),
    onError: (err) => {
      if (err instanceof ApiError && err.status === 400) return;
      toast.error(err instanceof ApiError ? `Not saved: ${err.detail ?? err.title}` : 'Not saved. Try again.');
    },
  });

  // Always built from the answer the server last gave, never from local state.
  const added = (hosts.data?.hosts ?? []).filter((h) => !h.built_in);
  const ready = !hosts.isPending;

  const add = (e: FormEvent) => {
    e.preventDefault();
    const name = host.trim().toLowerCase();
    if (!name || save.isPending) return;
    setProblem(null);
    save.mutate([...added.map(asInput), { host: name, kind }], {
      onSuccess: (next: CodeHosts) => {
        setFresh((f) => new Set(f).add(name));
        client.setQueryData(keys.codeHosts(org.id), next);
        setHost('');
      },
      onError: (err) => {
        if (err instanceof ApiError && err.status === 400) setProblem(refusal(err));
      },
    });
  };

  const remove = (name: string): Promise<CodeHosts> =>
    save.mutateAsync(added.filter((h) => h.host !== name).map(asInput));

  const builtIn = (hosts.data?.hosts ?? []).filter((h) => h.built_in);

  return (
    <SettingsSection
      id="hosts"
      title="Code hosts"
      aside={
        <InfoTip about="Code hosts" side="right">
          Tokens go only to these hosts, which must be reachable from the internet
        </InfoTip>
      }
    >
      <Skeleton
        ready={ready}
        placeholder={
          <div className="flex flex-col">
            {[0, 1].map((i) => (
              <div
                key={i}
                className="flex items-center justify-between border-t border-rule px-5 py-3 first:border-t-0"
              >
                <div className="h-9 w-48 rounded-control bg-panel" />
                <div className="h-6 w-16 rounded-chip bg-panel" />
              </div>
            ))}
          </div>
        }
      >
        <div className="flex min-w-0 flex-col [&>:first-child]:border-t-0">
          {builtIn.map((h) => (
            <HostLine key={h.host} host={h} />
          ))}
          {added.map((h) => (
            <AddedHost key={h.host} host={h} fresh={fresh.has(h.host)} mayEdit={mayEdit} remove={remove} />
          ))}
          {hosts.isError ? (
            <p className="m-0 border-t border-rule px-5 py-3 text-secondary text-rework-ink">
              Could not load the code hosts.
            </p>
          ) : null}
        </div>
      </Skeleton>
      {ready && mayEdit ? (
        <form onSubmit={add} noValidate className="contents">
          <SettingsRow id="code-host" label="Add a host" error={problem ?? undefined}>
            <div className="flex flex-wrap items-center gap-2">
              <Input
                id="code-host"
                name="host"
                value={host}
                placeholder="ghe.example.com"
                autoComplete="off"
                autoCapitalize="none"
                spellCheck={false}
                maxLength={253}
                aria-invalid={problem ? true : undefined}
                aria-describedby={problem ? 'code-host-error' : undefined}
                onChange={(e) => {
                  setHost(e.target.value);
                  setProblem(null);
                }}
                className="min-w-0 flex-1 basis-48 font-mono"
              />
              <Select
                aria-label="Kind"
                value={kind}
                options={kindOptions}
                className="w-32"
                onValueChange={(v) => setKind(v as CodeHostKind)}
              />
              <Button type="submit" variant="action" size="md" disabled={!host.trim() || save.isPending}>
                Add
              </Button>
            </div>
          </SettingsRow>
        </form>
      ) : null}
    </SettingsSection>
  );
}

/** The host's name over its kind, then what goes at the right. Long names truncate inside the column. */
function HostLine({ host, fresh = false, children }: { host: CodeHost; fresh?: boolean; children?: React.ReactNode }) {
  return (
    <div
      className={
        fresh
          ? 'flex min-w-0 animate-row-in items-center justify-between gap-3 border-t border-rule px-5 py-3 motion-reduce:animate-none'
          : 'flex min-w-0 items-center justify-between gap-3 border-t border-rule px-5 py-3'
      }
    >
      <div className="flex min-w-0 flex-col gap-1">
        <b className="truncate font-semibold text-body text-ink" title={host.host}>
          {host.host}
        </b>
        <span className="truncate text-secondary text-muted" title={host.built_in ? undefined : host.api_base}>
          {kindNames[host.kind]}
          {host.built_in ? null : ` · ${host.api_base}`}
        </span>
      </div>
      {host.built_in ? <Chip>Built in</Chip> : children}
    </div>
  );
}

function AddedHost({
  host,
  fresh,
  mayEdit,
  remove,
}: {
  host: CodeHost;
  fresh: boolean;
  mayEdit: boolean;
  remove: (name: string) => Promise<CodeHosts>;
}) {
  const client = useQueryClient();
  const org = useOrg();
  const answer = useRef<CodeHosts | null>(null);
  const { leaving, leave } = useLeaving(() => {
    if (answer.current) client.setQueryData(keys.codeHosts(org.id), answer.current);
  });
  return (
    <Leaving leaving={leaving}>
      <HostLine host={host} fresh={fresh}>
        {mayEdit ? (
          <Button
            type="button"
            size="sm"
            disabled={leaving}
            aria-label={`Remove ${host.host}`}
            onClick={() =>
              remove(host.host).then(
                (next) => {
                  answer.current = next;
                  leave();
                },
                () => undefined,
              )
            }
          >
            Remove
          </Button>
        ) : null}
      </HostLine>
    </Leaving>
  );
}
