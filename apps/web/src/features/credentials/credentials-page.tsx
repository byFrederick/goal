// Credentials (ADR 0017, canvas "Credentials"): the organization's keys and tokens, by name, with
// their type, the factories whose runs may open each, what uses it, when it was last used and when
// it expires. Nobody reads a value back. Filters (the type tab, the search text) live in the URL.
// Owners and admins (credential:manage) get New credential, Replace value and Delete; members read.
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
import { KeyRound, Lock, Search } from 'lucide-react';
import { useDeferredValue, useState } from 'react';
import { credentialsQuery, factoriesQuery } from '../../api/queries';
import type { Credential, CredentialType } from '../../api/types';
import { useOrg, useOrgHolds } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { EmptyState } from '../../ui/empty-state';
import { Field, Input } from '../../ui/field';
import { Menu, MenuContent, MenuItem, MenuTrigger } from '../../ui/menu';
import { NotAvailable } from '../../ui/not-available';
import { descriptionClass, eyebrowClass, PageHeader } from '../../ui/page-header';
import { SegmentedControl } from '../../ui/segmented-control';
import { DeleteCredentialDialog, ReplaceCredentialDialog } from './credential-dialogs';
import { ExpiryText, FactoryChips, RowMenu, TypeTile } from './credential-parts';
import { credentialsSearch } from './search';
import {
  countByType,
  credentialTypes,
  type Expiry,
  expiringText,
  expiringThisWeek,
  expiryOf,
  lastUsedText,
  typeLabels,
  typeTabLabels,
  usedByLabels,
} from './words';

export function CredentialsPage() {
  const org = useOrg();
  const search = credentialsSearch(useSearch({ strict: false }));
  const navigate = useNavigate({ from: '/o/$org/credentials/' });
  const manage = useOrgHolds('credential:manage');
  const list = useQuery(credentialsQuery(org.id));
  const factories = useQuery(factoriesQuery(org.id)).data ?? [];
  const [replacing, setReplacing] = useState<Credential | null>(null);
  const [deleting, setDeleting] = useState<Credential | null>(null);
  const [text, setText] = useState(search.q ?? '');
  const q = useDeferredValue(text.trim().toLowerCase());
  const now = Date.now();

  const all = list.data ?? [];
  const counts = countByType(all);
  const shown = all.filter(
    (c) =>
      (!search.type || c.type === search.type) &&
      (!q || [c.name, c.description].some((s) => s.toLowerCase().includes(q))),
  );
  const expiring = expiringThisWeek(all, now);

  const setSearch = (patch: { type?: CredentialType | undefined; q?: string | undefined }) =>
    void navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true });

  const newCredential = manage ? (
    <Menu>
      <MenuTrigger asChild>
        <Button variant="primary" className="h-11 md:h-10">
          <span aria-hidden="true">+</span> New credential
        </Button>
      </MenuTrigger>
      <MenuContent side="bottom" align="end" className="w-72">
        <MenuItem asChild>
          <Link to="/o/$org/credentials/new" params={{ org: org.slug }}>
            <KeyRound size={16} strokeWidth={1.5} aria-hidden="true" className="mt-1 text-muted" />
            <span className="flex flex-col">
              <span className="font-medium">Create a credential</span>
              <span className="text-meta text-muted">One value, typed</span>
            </span>
          </Link>
        </MenuItem>
        <MenuItem asChild>
          <Link to="/o/$org/credentials/new" params={{ org: org.slug }} search={{ panel: 'import' }}>
            <span className="flex flex-col">
              <span className="font-medium">Import from .env or YAML</span>
              <span className="text-meta text-muted">Many at once, checked before saving</span>
            </span>
          </Link>
        </MenuItem>
      </MenuContent>
    </Menu>
  ) : null;

  return (
    <>
      <PageHeader
        eyebrow={<p className={cn(eyebrowClass, 'md:hidden')}>{org.name}</p>}
        title="Credentials"
        description={
          <p className={cn(descriptionClass, 'max-w-3xl')}>
            Keys and tokens that connections and agents use. Nobody can read a value back.
          </p>
        }
        actions={newCredential}
      />

      <div className="flex flex-wrap items-center gap-3 rounded-card bg-ok-tint px-4 py-3 text-body text-ok-ink">
        <Lock size={18} strokeWidth={1.5} aria-hidden="true" className="flex-none" />
        <p className="m-0 min-w-0 flex-1">
          Values are encrypted before they are stored and never shown again. Agents get them only inside their run’s
          box.
        </p>
        <NotAvailable label="Encrypt in your browser">
          <button type="button" className="font-semibold whitespace-nowrap text-ok-ink">
            Encrypt in your browser instead
          </button>
        </NotAvailable>
      </div>

      {list.isPending ? (
        <div aria-busy="true" className="rounded-card border border-line bg-card p-5">
          <span role="status" className="sr-only">
            Loading credentials
          </span>
        </div>
      ) : list.isError ? (
        <div
          className="flex max-w-xl flex-col items-start gap-3 rounded-card border border-line bg-card p-5"
          role="alert"
        >
          <Callout tone="rework">Couldn’t load credentials. Check your connection and try again.</Callout>
          <Button variant="white" onClick={() => void list.refetch()}>
            Try again
          </Button>
        </div>
      ) : all.length === 0 ? (
        <EmptyState icon={<KeyRound size={20} strokeWidth={1.5} />} title="No credentials yet" actions={newCredential}>
          {manage
            ? 'Add the keys and tokens your connections and agents use.'
            : `Owners and admins of ${org.name} add credentials.`}
        </EmptyState>
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-2">
            <SegmentedControl
              label="Type"
              value={search.type ?? 'all'}
              onChange={(value) => setSearch({ type: value === 'all' ? undefined : value })}
              options={[
                { value: 'all', label: 'All', count: all.length },
                ...credentialTypes.map((t) => ({ value: t, label: typeTabLabels[t], count: counts[t] })),
              ]}
            />
            {expiring > 0 ? (
              <Chip tone="rework" className="sm:ml-auto">
                {expiringText(expiring)}
              </Chip>
            ) : null}
            <Field label={<span className="sr-only">Search credentials</span>} className="w-full sm:w-56">
              <span className="relative flex">
                <Search
                  size={16}
                  strokeWidth={1.5}
                  aria-hidden="true"
                  className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-muted"
                />
                <Input
                  type="search"
                  value={text}
                  placeholder="Search credentials"
                  autoComplete="off"
                  className="pl-9"
                  onChange={(e) => {
                    setText(e.target.value);
                    setSearch({ q: e.target.value.trim() || undefined });
                  }}
                />
              </span>
            </Field>
          </div>

          {shown.length === 0 ? (
            <div className="flex flex-col items-start gap-2 rounded-card border border-line bg-card p-4">
              <p className="m-0 text-body text-muted">No credential matches these filters.</p>
              <Button
                variant="white"
                onClick={() => {
                  setText('');
                  setSearch({ type: undefined, q: undefined });
                }}
              >
                Show every credential
              </Button>
            </div>
          ) : (
            <div className="overflow-x-auto rounded-card border border-line bg-card">
              <table className="w-full border-collapse text-secondary">
                <caption className="sr-only">Credentials of {org.name}</caption>
                <thead>
                  <tr className="bg-panel text-left text-muted [&>th]:px-3 [&>th]:py-2 [&>th]:font-semibold">
                    <th scope="col">Credential</th>
                    <th scope="col">Factories</th>
                    <th scope="col">Used by</th>
                    <th scope="col">Last used</th>
                    <th scope="col">Expiry and rotation</th>
                    <th scope="col">
                      <span className="sr-only">Actions</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {shown.map((c) => {
                    const expiry = expiryOf(c, now);
                    const used = usedByLabels(c);
                    return (
                      <tr
                        key={c.id}
                        className="border-t border-line align-middle hover:bg-panel [&>td]:px-3 [&>td]:py-3"
                      >
                        <td className="whitespace-nowrap">
                          <div className="flex items-center gap-3">
                            <TypeTile type={c.type} alert={isAlert(expiry)} />
                            <span className="flex flex-col">
                              <span className="font-mono text-body font-medium text-ink">{c.name}</span>
                              <span className="text-meta text-muted">{typeLabels[c.type]}</span>
                            </span>
                          </div>
                        </td>
                        <td>
                          <FactoryChips credential={c} factories={factories} />
                        </td>
                        <td className="text-muted">
                          {used.length > 0
                            ? used.map((label) => (
                                <span key={label} className="block">
                                  {label}
                                </span>
                              ))
                            : 'Not used yet'}
                        </td>
                        <td className="whitespace-nowrap text-muted">{lastUsedText(c, now)}</td>
                        <td className="whitespace-nowrap">
                          <ExpiryText expiry={expiry} onReplace={manage ? () => setReplacing(c) : undefined} />
                        </td>
                        <td className="text-right">
                          {manage ? (
                            <RowMenu credential={c} onReplace={() => setReplacing(c)} onDelete={() => setDeleting(c)} />
                          ) : null}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}

      <p className="text-meta text-muted">
        Write a credential into a connection or MCP server setting as <code className="font-mono">{'{{name}}'}</code>.
        Goal fills it in when a run starts.
      </p>

      <ReplaceCredentialDialog org={org.id} credential={replacing} onClose={() => setReplacing(null)} />
      <DeleteCredentialDialog org={org.id} credential={deleting} onClose={() => setDeleting(null)} />
    </>
  );
}

/** The reminder window and past expiry: the tile and the text turn the danger colour. */
function isAlert(expiry: Expiry): boolean {
  return expiry.state === 'reminder' || expiry.state === 'expired';
}
