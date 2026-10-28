// Needs you (/o/$org/needs-you, ADR 0016 wave 2): what waits on a person across the organization's
// factories, with the decision in view. The tab, the factory and the selected entry are in the URL.
// Desktop is a split: the list, then the detail. A phone shows the list, or the approve view when the
// URL names an entry. A Decision's Approve and Deny call the same endpoints as the item page; an
// Approval's call the tool-approval endpoint, with Always allow when offered (ADR 0031). J, K, A and D
// work from the keyboard, but not while a field or the Deny dialog has focus.
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
import { ChevronDown, Inbox } from 'lucide-react';
import { type ReactNode, useEffect, useState } from 'react';
import { factoriesQuery, inboxQuery } from '../../api/queries';
import type { InboxEntry } from '../../api/types';
import { usePhone } from '../../app/phone-chrome';
import { useOrg, useOrgHolds } from '../../shared/org';
import { Button } from '../../ui/button';
import { cn } from '../../ui/cn';
import { Count } from '../../ui/count';
import { EmptyState } from '../../ui/empty-state';
import { Menu, MenuContent, MenuRadioGroup, MenuRadioItem, MenuTrigger } from '../../ui/menu';
import { alwaysAllowOffered, type DecidedEntry, decisionOf, useEntryDecision } from './actions';
import { DenyDialog } from './deny-dialog';
import { Detail } from './detail';
import { NeedsList } from './rows';
import { factoriesOf, type NeedsSearch, needsSearch, tabCounts, visibleEntries } from './search';
import { type NeedsTab, tabs, typeLabel, typeOf } from './types';

/** An entry that is a Decision or an Approval and has its item: the kinds Approve and Deny apply to. */
function decidable(entry: InboxEntry | undefined): DecidedEntry['entry'] | undefined {
  const type = entry && typeOf(entry);
  return entry && (type === 'decision' || type === 'approval') && entry.item
    ? (entry as DecidedEntry['entry'])
    : undefined;
}

export function NeedsYouPage() {
  const org = useOrg();
  const navigate = useNavigate({ from: '/o/$org/needs-you' });
  const search = needsSearch(useSearch({ strict: false }) as Record<string, unknown>);
  const tab = search.type ?? 'all';
  const phone = usePhone();
  const inbox = useQuery(inboxQuery(org.id));
  // The factories carry each one's grants (`can`); an entry's own factory does not, so the question's answers read them here.
  const factories = useQuery(factoriesQuery(org.id));
  const decide = useEntryDecision();
  const [denying, setDenying] = useState(false);
  // Always allow is the one choice an Approval carries. It names the entry it is checked for, so a
  // checked box never carries over to another entry.
  const [alwaysAllowFor, setAlwaysAllowFor] = useState<string | undefined>(undefined);
  const canManageConnections = useOrgHolds('connection:manage');

  const entries = inbox.data?.entries ?? [];
  const visible = visibleEntries(entries, search);
  const selected = visible.find((e) => e.id === search.entry) ?? visible[0];
  const current = decidable(selected);
  const allowAlways = current !== undefined && alwaysAllowOffered(current, canManageConnections);
  const alwaysAllow = current !== undefined && alwaysAllowFor === current.id;
  const setAlwaysAllow = (checked: boolean) => setAlwaysAllowFor(checked && current ? current.id : undefined);
  const approvalCurrent = current !== undefined && typeOf(current) === 'approval';
  const approving = phone && visible.some((e) => e.id === search.entry);
  const counts = tabCounts(entries, search.factory);
  const factoryOf = (entry: InboxEntry) => factories.data?.find((f) => f.id === entry.factory.id);

  const setSearch = (next: NeedsSearch) => navigate({ search: (prev) => ({ ...prev, ...clean(next) }) });

  const approve = () => {
    if (!current) return;
    if (approvalCurrent) {
      decide.mutate({ entry: current, decision: { kind: 'tool_approve', always_allow: allowAlways && alwaysAllow } });
      return;
    }
    const decision = decisionOf(current) === 'merge' ? ({ kind: 'merge' } as const) : ({ kind: 'approve' } as const);
    decide.mutate({ entry: current, decision });
  };

  // Deny asks for the reason; the dialog is the same on the phone's approve view and on the split.
  const deny = current ? (
    <DenyDialog
      ref={current.item.ref}
      open={denying}
      onOpenChange={setDenying}
      pending={decide.isPending}
      onConfirm={(reason) =>
        decide.mutate(
          {
            entry: current,
            decision: approvalCurrent ? { kind: 'tool_deny', reason } : { kind: 'reject', reason },
          },
          { onSuccess: () => setDenying(false) },
        )
      }
    />
  ) : null;

  // J and K move the selection, A approves and D opens the Deny dialog. They do nothing while a field
  // or the Deny dialog has focus, so typing a reason never approves.
  useEffect(() => {
    if (denying) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey) return;
      const target = event.target as HTMLElement | null;
      if (target && (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName))) return;
      const at = visible.findIndex((e) => e.id === selected?.id);
      switch (event.key.toLowerCase()) {
        case 'j': {
          const next = visible[Math.min(at + 1, visible.length - 1)];
          if (next) setSearch({ entry: next.id });
          break;
        }
        case 'k': {
          const next = visible[Math.max(at - 1, 0)];
          if (next) setSearch({ entry: next.id });
          break;
        }
        case 'a':
          if (current && !decide.isPending) approve();
          break;
        case 'd':
          if (current) setDenying(true);
          break;
        default:
          return;
      }
      event.preventDefault();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
    // The handler reads the visible list and the selection; both change with the URL and the data.
  });

  if (inbox.isPending) {
    return (
      <main aria-busy="true" className="flex min-h-0 flex-1 flex-col">
        <p className="sr-only">Loading</p>
      </main>
    );
  }

  const header = (
    <header className="flex flex-wrap items-center gap-3 border-b border-line px-4 py-3 md:px-7 md:py-4">
      <div className="flex items-center gap-2">
        <h1 className="m-0 text-page-title font-semibold text-ink">Needs you</h1>
        {entries.length > 0 ? <Count value={counts.all} label=" waiting on you" tone="you" /> : null}
      </div>
      {entries.length > 0 ? (
        <div className="ml-auto flex flex-wrap items-center gap-2">
          <TabGroup
            counts={counts}
            value={tab}
            onChange={(type) => setSearch({ type: type === 'all' ? undefined : type })}
          />
          <FactoryMenu
            entries={entries}
            value={search.factory}
            onChange={(factory) => setSearch({ factory, entry: undefined })}
          />
        </div>
      ) : null}
    </header>
  );

  if (inbox.isError) {
    return (
      <>
        {header}
        <div className="p-4 md:p-7">
          <div
            role="alert"
            className="flex max-w-xl flex-col items-start gap-3 rounded-card border border-line bg-card p-5"
          >
            <p className="m-0 text-heading font-semibold text-ink">Needs you did not load.</p>
            <p className="m-0 text-secondary text-muted">{inbox.error.message}</p>
            <Button variant="secondary" onClick={() => void inbox.refetch()}>
              Try again
            </Button>
          </div>
        </div>
      </>
    );
  }

  if (entries.length === 0) {
    return (
      <>
        {header}
        <div className="p-4 md:p-7">
          <EmptyState
            icon={<Inbox size={20} strokeWidth={1.5} aria-hidden="true" />}
            title="Nothing waits on you right now."
            actions={
              <Button asChild variant="secondary">
                <Link to="/o/$org" params={{ org: org.slug }}>
                  Go to Home
                </Link>
              </Button>
            }
          />
        </div>
      </>
    );
  }

  if (approving && selected) {
    return (
      <>
        <header className="flex items-center gap-3 border-b border-line px-4 py-3">
          <Link
            to="/o/$org/needs-you"
            params={{ org: org.slug }}
            search={{ type: search.type, factory: search.factory }}
            aria-label="Back to Needs you"
            className="text-run no-underline hover:text-run-ink"
          >
            Back
          </Link>
          <h1 className="m-0 text-heading font-semibold text-ink">{typeLabel[typeOf(selected)]}</h1>
        </header>
        <Detail
          entry={selected}
          factory={factoryOf(selected)}
          phone
          pending={decide.isPending}
          allowAlways={allowAlways}
          alwaysAllow={alwaysAllow}
          onAlwaysAllow={setAlwaysAllow}
          onApprove={approve}
          onDeny={() => setDenying(true)}
        />
        {deny}
      </>
    );
  }

  return (
    <>
      {header}
      <div className="flex min-h-0 flex-1 flex-col md:flex-row">
        <section
          aria-label="Waiting on you"
          className="flex min-h-0 flex-col md:w-90 md:flex-none md:border-r md:border-line"
        >
          {visible.length === 0 ? (
            <p className="m-0 px-4 py-5 text-secondary text-muted">Nothing of this type waits on you.</p>
          ) : (
            <NeedsList
              label="Waiting on you, oldest blocking first"
              entries={visible}
              selected={selected?.id}
              search={{ type: search.type, factory: search.factory }}
              siblings={factoriesOf(entries)}
            />
          )}
          <p className="m-0 hidden px-4 py-3 text-meta text-muted md:block">
            <Kbd>J</Kbd> <Kbd>K</Kbd> to move, <Kbd>A</Kbd> approve, <Kbd>D</Kbd> deny
          </p>
        </section>
        <div className="min-w-0 flex-1">
          {selected ? (
            <Detail
              entry={selected}
              factory={factoryOf(selected)}
              pending={decide.isPending}
              allowAlways={allowAlways}
              alwaysAllow={alwaysAllow}
              onAlwaysAllow={setAlwaysAllow}
              onApprove={approve}
              onDeny={() => setDenying(true)}
            />
          ) : null}
        </div>
      </div>
      {deny}
    </>
  );
}

/** The URL keeps no empty params: an undefined value drops the key. */
function clean(next: NeedsSearch) {
  return Object.fromEntries(Object.entries(next).map(([k, v]) => [k, v === '' ? undefined : v]));
}

function Kbd({ children }: { children: ReactNode }) {
  return <kbd className="font-control text-meta text-ink-soft">{children}</kbd>;
}

/** The tabs: All, Approvals, Decisions, Questions, Alerts, each with its count. */
function TabGroup({
  counts,
  value,
  onChange,
}: {
  counts: Record<NeedsTab, number>;
  value: NeedsTab;
  onChange: (tab: NeedsTab) => void;
}) {
  return (
    <fieldset className="m-0 inline-flex flex-wrap gap-1 rounded-control border border-line bg-panel p-1">
      <legend className="sr-only">Show</legend>
      {tabs.map((tab) => {
        const pressed = value === tab.value;
        return (
          <button
            key={tab.value}
            type="button"
            aria-pressed={pressed}
            onClick={() => onChange(tab.value)}
            className={cn(
              'inline-flex h-11 items-center gap-2 rounded-chip px-3 font-control text-secondary whitespace-nowrap text-ink-soft hover:text-ink md:h-7',
              pressed && 'bg-card text-ink shadow-raised',
            )}
          >
            {tab.label}
            <span className="text-meta text-muted tabular-nums">{counts[tab.value]}</span>
          </button>
        );
      })}
    </fieldset>
  );
}

/** The All factories menu: a choice of one factory, or all of them. */
function FactoryMenu({
  entries,
  value,
  onChange,
}: {
  entries: readonly InboxEntry[];
  value: string | undefined;
  onChange: (factory: string | undefined) => void;
}) {
  const factories = factoriesOf(entries);
  const chosen = factories.find((f) => f.name === value);
  return (
    <Menu>
      <MenuTrigger asChild>
        <Button variant="secondary">
          {chosen ? chosen.title : 'All factories'}
          <ChevronDown size={14} strokeWidth={1.5} aria-hidden="true" />
        </Button>
      </MenuTrigger>
      <MenuContent align="end">
        <MenuRadioGroup value={value ?? 'all'} onValueChange={(v) => onChange(v === 'all' ? undefined : v)}>
          <MenuRadioItem value="all">All factories</MenuRadioItem>
          {factories.map((f) => (
            <MenuRadioItem key={f.name} value={f.name}>
              {f.title}
            </MenuRadioItem>
          ))}
        </MenuRadioGroup>
      </MenuContent>
    </Menu>
  );
}
