// One secret (ADR 0037 decision 14, canvas docs/design/screens/connections/secret.jpg): where it is used,
// its activity from the audit log, Replace value, its settings and Delete. No value is ever shown: the
// Value card draws dots for a write-only field. Delete is disabled, with its reason, while something uses
// the secret. People without secret:manage see none of the controls that change it.
import { useQuery, useSuspenseQuery } from '@tanstack/react-query';
import { Link, useNavigate, useParams } from '@tanstack/react-router';
import { Lock } from 'lucide-react';
import { useId, useState } from 'react';
import { factoriesQuery, secretActivityQuery, secretQuery } from '../../api/queries';
import type { Secret } from '../../api/types';
import { TopBarSlot } from '../../app/top-bar';
import { formatDay } from '../../shared/format';
import { useOrg, useOrgHolds } from '../../shared/org';
import { Button } from '../../ui/button';
import { Chip } from '../../ui/chip';
import { DeleteSecretDialog, ReplaceValueDialog } from './secret-dialogs';
import { SettingsCard } from './secret-settings';
import { activityText, deleteReason, lastUsedText, placesCount, useKindLabels } from './words';

const card = 'flex min-w-0 flex-col rounded-card border border-line bg-card';
const cardHead = 'flex min-h-14 items-center justify-between gap-3 border-b border-line px-4 py-2 md:px-5';

export function SecretPage() {
  const org = useOrg();
  const params = useParams({ from: '/o/$org/_org/secrets/$secret' });
  const { data: secret } = useSuspenseQuery(secretQuery(org.id, params.secret));
  const factories = useQuery(factoriesQuery(org.id)).data ?? [];
  const manage = useOrgHolds('secret:manage');
  const [replacing, setReplacing] = useState<Secret | null>(null);
  const now = Date.now();

  return (
    <>
      <TopBarSlot
        crumb={secret.name}
        actions={
          manage ? (
            <Button variant="white" className="h-11 md:h-8.5" onClick={() => setReplacing(secret)}>
              Replace value
            </Button>
          ) : undefined
        }
      />
      <div className="@container flex min-w-0 flex-col gap-5">
        <header className="flex min-w-0 flex-col gap-2">
          <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
            <h1 className="m-0 flex min-w-0 flex-wrap items-center gap-3 text-page-title">
              <span translate="no" className="min-w-0 break-all font-mono">
                {secret.name}
              </span>
              <Chip tone="quiet" className="font-normal tracking-normal">
                Pasted value
              </Chip>
            </h1>
            <span className="inline-flex items-center gap-2 text-secondary text-muted">
              <span
                aria-hidden="true"
                className={secret.last_used_at ? 'size-1.5 rounded-full bg-ok' : 'size-1.5 rounded-full bg-muted'}
              />
              {secret.last_used_at ? `Used ${lastUsedText(secret, now).toLowerCase()}` : 'Never used'}
            </span>
          </div>
          {secret.description ? <p className="m-0 text-body text-ink-soft">{secret.description}</p> : null}
        </header>

        <div className="grid items-start gap-5 @[900px]:grid-cols-[minmax(0,1fr)_minmax(0,448px)]">
          <div className="flex min-w-0 flex-col gap-5">
            <UsesCard secret={secret} />
            <ActivityCard org={org.id} secret={secret} now={now} />
          </div>
          <div className="flex min-w-0 flex-col gap-5">
            <section aria-labelledby="value-heading" className={card}>
              <div className={cardHead}>
                <h2 id="value-heading" className="m-0 text-heading">
                  Value
                </h2>
              </div>
              <div className="flex flex-col gap-3 px-4 py-4 md:px-5">
                <div className="flex h-11 items-center justify-between gap-3 rounded-control border border-rule bg-card px-3 md:h-10">
                  <span aria-hidden="true" className="font-mono text-body text-muted">
                    ••••••••••••••••••••••
                  </span>
                  <Chip tone="quiet" className="font-normal text-muted">
                    <Lock size={12} strokeWidth={1.75} aria-hidden="true" />
                    Write-only
                  </Chip>
                </div>
                <p className="m-0 text-secondary text-muted">
                  Replaced {formatDay(secret.updated_at)} by {secret.updated_by.name}. Runs that start after a replace
                  use the new value.
                </p>
                {manage ? (
                  <Button variant="white" className="h-11 self-start md:h-9" onClick={() => setReplacing(secret)}>
                    Replace value
                  </Button>
                ) : null}
              </div>
            </section>
            <SettingsCard org={org.id} secret={secret} factories={factories} manage={manage} now={now} />
            {manage ? <DeleteCard org={org.id} secret={secret} /> : null}
          </div>
        </div>
      </div>
      <ReplaceValueDialog org={org.id} secret={replacing} onClose={() => setReplacing(null)} />
    </>
  );
}

/** Where it's used: one row per use, with its kind. */
function UsesCard({ secret }: { secret: Secret }) {
  const org = useOrg();
  return (
    <section aria-labelledby="uses-heading" className={card}>
      <div className={cardHead}>
        <h2 id="uses-heading" className="m-0 text-heading">
          Where it’s used
        </h2>
        <span className="text-secondary text-muted">{placesCount(secret.uses.length)}</span>
      </div>
      {secret.uses.length === 0 ? (
        <p className="m-0 px-4 py-4 text-secondary text-muted md:px-5">Nothing uses it yet.</p>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          {secret.uses.map((u) => (
            <li
              key={`${u.kind}-${u.name}`}
              className="flex min-h-14 flex-wrap items-center justify-between gap-x-4 gap-y-1 border-t border-line px-4 py-3 first:border-t-0 md:px-5"
            >
              <span className="min-w-0 break-words text-body font-medium">
                {u.kind === 'knowledge_source' ? (
                  <Link to="/o/$org/knowledge/$source" params={{ org: org.slug, source: u.name }}>
                    {u.name}
                  </Link>
                ) : (
                  u.name
                )}
              </span>
              <span className="text-secondary text-muted">{useKindLabels[u.kind]}</span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

/** Activity from the audit log, filtered to this secret. Without audit:read it says so instead of failing. */
function ActivityCard({ org, secret, now }: { org: string; secret: Secret; now: number }) {
  const auditor = useOrgHolds('audit:read');
  const activity = useQuery({ ...secretActivityQuery(org, secret.id), enabled: auditor });
  const entries = activity.data?.entries ?? [];
  const unavailable = !auditor || activity.isError;
  return (
    <section aria-labelledby="activity-heading" className={card}>
      <div className={cardHead}>
        <h2 id="activity-heading" className="m-0 text-heading">
          Activity
        </h2>
      </div>
      {unavailable ? (
        <p className="m-0 px-4 py-4 text-secondary text-muted md:px-5">
          Not available. Reading the audit log needs a role that can see it.
        </p>
      ) : activity.isPending ? (
        <p role="status" className="m-0 px-4 py-4 text-secondary text-muted md:px-5">
          Loading
        </p>
      ) : entries.length === 0 ? (
        <p className="m-0 px-4 py-4 text-secondary text-muted md:px-5">Nothing yet.</p>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          {entries.map((e) => (
            <li
              key={e.id}
              className="flex min-h-11 items-center justify-between gap-3 border-t border-line px-4 py-2 first:border-t-0 md:px-5"
            >
              <span className="min-w-0 break-words text-secondary">{activityText(e)}</span>
              <span className="shrink-0 text-secondary text-muted">
                {Date.parse(e.at) > now - 86_400_000 ? lastUsedText({ last_used_at: e.at }, now) : formatDay(e.at)}
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

/** Delete: disabled with its reason while something uses the secret; otherwise it asks first. */
function DeleteCard({ org, secret }: { org: string; secret: Secret }) {
  const navigate = useNavigate();
  const slug = useOrg().slug;
  const reasonId = useId();
  const [asking, setAsking] = useState<Secret | null>(null);
  const inUse = secret.uses.length > 0;
  return (
    <section aria-labelledby="delete-heading" className={card}>
      <div className={cardHead}>
        <h2 id="delete-heading" className="m-0 text-heading">
          Delete
        </h2>
      </div>
      <div className="flex flex-col items-start gap-3 px-4 py-4 md:px-5">
        <p id={reasonId} className="m-0 text-body">
          {inUse ? deleteReason(secret.uses.length) : 'Runs that start afterwards can’t get it.'}
        </p>
        <Button
          variant="rework"
          className="h-11 aria-disabled:cursor-not-allowed aria-disabled:opacity-55 md:h-9"
          aria-disabled={inUse || undefined}
          aria-describedby={reasonId}
          onClick={() => {
            if (!inUse) setAsking(secret);
          }}
        >
          Delete secret
        </Button>
      </div>
      <DeleteSecretDialog
        org={org}
        secret={asking}
        onClose={() => setAsking(null)}
        onDeleted={() => void navigate({ to: '/o/$org/secrets', params: { org: slug } })}
      />
    </section>
  );
}
