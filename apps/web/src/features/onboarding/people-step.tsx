// Step 7, "Invite people" (docs/design/screens/get-started/invite.jpg; ADR 0032 decision 2). A person adds addresses,
// each with a role (Builder by default). "Send N invites" sends every row through the invitations the Members page
// uses (usePeopleChanges().invite, built by inviteRequestsOf), then goes to step 8. "Skip for now" goes on without
// sending, and "Continue" does the same when there are no rows. A refused address is named and stays in the list.
// Its four states: loading, error (with Retry), a factory that cannot be found, and ready.
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { X } from 'lucide-react';
import { type FormEvent, useRef, useState } from 'react';
import { factoryQuery, intakeQuery, versionModelQuery } from '../../api/queries';
import type { Factory, FactoryModel, Intake, OrgSummary } from '../../api/types';
import { initialsOf } from '../../shared/names';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { Field, Input, NativeSelect } from '../../ui/field';
import { Avatar } from '../../ui/mark';
import { inviteRequestsOf, maxInvites, parseEmails } from '../members/people';
import { refusal, usePeopleChanges } from '../members/use-people';
import type { OnboardingSearch } from './code-search';
import { OnboardingLayout } from './onboarding-layout';
import { type InviteRole, inviteRoles, roleGrant } from './people-roles';
import { stepPanel } from './step-panel';

interface Row {
  email: string;
  role: InviteRole;
}

export function PeopleStep({ org, search }: { org: OrgSummary; search: OnboardingSearch }) {
  const factoryId = search.factory;
  const factory = useQuery({ ...factoryQuery(org.id, factoryId ?? ''), enabled: factoryId !== undefined });

  if (!factoryId) {
    return <NoFactory org={org} search={search} message="Choose a template first, then invite people." />;
  }
  if (factory.isError) {
    return <NoFactory org={org} search={search} message="This factory can’t be read. Choose a template again." />;
  }
  if (factory.isPending) return <Loading org={org} search={search} factory={factoryId} />;
  return <Invite org={org} search={search} factoryId={factoryId} factory={factory.data} />;
}

/** Back to step 6 while the factory exists; back to step 3 (which makes one) when it does not. */
function Back({ org, search, factory }: { org: OrgSummary; search: OnboardingSearch; factory?: string }) {
  return (
    <Button asChild size="lg" variant="secondary">
      {factory ? (
        <Link to="/o/$org/get-started/$step" params={{ org: org.slug, step: 'intake' }} search={{ factory }}>
          Back
        </Link>
      ) : (
        <Link
          to="/o/$org/get-started/$step"
          params={{ org: org.slug, step: 'factory' }}
          search={{ connection: search.connection, owner: search.owner, repos: search.repos }}
        >
          Back
        </Link>
      )}
    </Button>
  );
}

/** The step without a factory, or for a factory that cannot be read: a link back to step 3. */
function NoFactory({ org, search, message }: { org: OrgSummary; search: OnboardingSearch; message: string }) {
  return (
    <OnboardingLayout step="people" org={org} back={<Back org={org} search={search} />}>
      <div className="flex flex-col gap-2">
        <h1 className="m-0 font-numeral text-kpi tracking-display">Invite people</h1>
      </div>
      <Callout tone="rework">{message}</Callout>
    </OnboardingLayout>
  );
}

function Loading({ org, search, factory }: { org: OrgSummary; search: OnboardingSearch; factory: string }) {
  return (
    <OnboardingLayout step="people" org={org} back={<Back org={org} search={search} factory={factory} />}>
      <div aria-busy="true">
        <span role="status" className="sr-only">
          Loading
        </span>
      </div>
    </OnboardingLayout>
  );
}

/** Reads the published model and the intake (for the panel); the step shows loading until both are there. */
function Invite({
  org,
  search,
  factoryId,
  factory,
}: {
  org: OrgSummary;
  search: OnboardingSearch;
  factoryId: string;
  factory: Factory;
}) {
  const model = useQuery(versionModelQuery(org.id, factoryId, 'current'));
  const intake = useQuery(intakeQuery(org.id, factoryId));

  if (model.isPending || intake.isPending) return <Loading org={org} search={search} factory={factoryId} />;
  if (model.isError || intake.isError) {
    const retry = () => {
      void model.refetch();
      void intake.refetch();
    };
    return (
      <OnboardingLayout step="people" org={org} back={<Back org={org} search={search} factory={factoryId} />}>
        <div className="flex flex-col gap-2">
          <h1 className="m-0 font-numeral text-kpi tracking-display">Invite people</h1>
        </div>
        <Callout tone="rework">
          <span>The factory can’t be read.</span>
          <Button type="button" size="sm" variant="secondary" className="mt-2" onClick={retry}>
            Retry
          </Button>
        </Callout>
      </OnboardingLayout>
    );
  }
  return (
    <Ready
      org={org}
      search={search}
      factoryId={factoryId}
      factory={factory}
      model={model.data.model}
      intake={intake.data}
    />
  );
}

function Ready({
  org,
  search,
  factoryId,
  factory,
  model,
  intake,
}: {
  org: OrgSummary;
  search: OnboardingSearch;
  factoryId: string;
  factory: Factory;
  model: FactoryModel;
  intake: Intake;
}) {
  const navigate = useNavigate();
  const { invite } = usePeopleChanges(org);
  const emailRef = useRef<HTMLInputElement>(null);
  const [text, setText] = useState('');
  const [picked, setPicked] = useState<InviteRole>('builder');
  const [rows, setRows] = useState<Row[]>([]);
  const [error, setError] = useState<string | undefined>();
  const [refusals, setRefusals] = useState<string[]>([]);

  const roleLabel = (role: InviteRole) => inviteRoles.find((r) => r.value === role)?.label ?? role;
  // Only owners invite owners, as the Members page offers it.
  const roles = inviteRoles.filter((r) => r.value !== 'owner' || org.role === 'owner');

  const goToFirstItem = () => {
    void navigate({
      to: '/o/$org/get-started/$step',
      params: { org: org.slug, step: 'first-item' },
      search: { factory: factoryId },
    });
  };

  const onAdd = (e: FormEvent) => {
    e.preventDefault();
    const { emails, invalid } = parseEmails(text);
    const fresh = emails.filter((email) => !rows.some((r) => r.email === email));
    const problem =
      invalid.length > 0
        ? `Not an email address: ${invalid.join(', ')}.`
        : emails.length === 0
          ? 'Enter an email address.'
          : rows.length + fresh.length > maxInvites
            ? `At most ${maxInvites} addresses at once.`
            : undefined;
    setError(problem);
    if (problem) {
      emailRef.current?.focus();
      return;
    }
    setRows((list) => [...list, ...fresh.map((email) => ({ email, role: picked }))]);
    setText('');
    emailRef.current?.focus();
  };

  const send = async () => {
    if (rows.length === 0) {
      goToFirstItem();
      return;
    }
    setRefusals([]);
    // One request per row, each with its own role: the same requests the Members page sends.
    const requests = rows.flatMap((r) => {
      const { role, grant } = roleGrant(r.role);
      return inviteRequestsOf({
        orgId: org.id,
        emails: [r.email],
        role,
        grants: grant ? { [factoryId]: grant } : {},
      });
    });
    try {
      const refused = await invite.mutateAsync(requests);
      if (refused.length === 0) {
        goToFirstItem();
        return;
      }
      // Keep the rows that did not go, and say why each did not.
      const failed = new Set(refused.map((f) => f.email));
      setRows((list) => list.filter((r) => failed.has(r.email)));
      setRefusals(refused.map((f) => `${f.email}: ${refusal(f.err, org.name)}`));
    } catch (err) {
      setRefusals([refusal(err, org.name)]);
    }
  };

  const count = rows.length;
  const label = count === 0 ? 'Continue' : count === 1 ? 'Send 1 invite' : `Send ${count} invites`;

  return (
    <OnboardingLayout
      step="people"
      org={org}
      back={<Back org={org} search={search} factory={factoryId} />}
      action={
        <div className="flex items-center gap-3">
          <Button asChild size="lg" variant="ghost">
            <Link
              to="/o/$org/get-started/$step"
              params={{ org: org.slug, step: 'first-item' }}
              search={{ factory: factoryId }}
            >
              Skip for now
            </Link>
          </Button>
          <Button
            type="button"
            size="lg"
            variant="primary"
            disabled={invite.isPending}
            aria-busy={invite.isPending || undefined}
            onClick={() => void send()}
          >
            {label}
          </Button>
        </div>
      }
      preview={stepPanel({ org, search, factory, model, intake })}
    >
      <div className="flex flex-col gap-2">
        <h1 className="m-0 font-numeral text-kpi tracking-display">Invite people</h1>
        <p className="m-0 text-heading font-normal text-ink-soft">
          People answer what needs them, approve writes and shape the factory with you.
        </p>
      </div>

      <form onSubmit={onAdd} noValidate className="flex flex-col gap-4">
        <div className="flex flex-wrap items-end gap-3">
          <Field label="Email" error={error} className="min-w-0 flex-1 basis-56">
            <Input
              ref={emailRef}
              type="email"
              value={text}
              onChange={(e) => setText(e.target.value)}
              placeholder="name@company.com"
              autoComplete="off"
              autoCapitalize="off"
              spellCheck={false}
            />
          </Field>
          <Field label="Role" className="w-40">
            <NativeSelect value={picked} onChange={(e) => setPicked(e.target.value as InviteRole)}>
              {roles.map((r) => (
                <option key={r.value} value={r.value}>
                  {r.label}
                </option>
              ))}
            </NativeSelect>
          </Field>
          <Button type="submit" size="lg" variant="secondary">
            Add
          </Button>
        </div>

        {rows.length > 0 ? (
          <ul aria-label="People to invite" className="m-0 flex list-none flex-col gap-2 p-0">
            {rows.map((r) => (
              <li key={r.email} className="flex items-center gap-3 rounded-card border border-line bg-card px-3 py-2">
                <Avatar initials={initialsOf({ name: '', email: r.email })} size="phone" />
                <span className="min-w-0 flex-1 break-all text-body text-ink">{r.email}</span>
                <Chip>{roleLabel(r.role)}</Chip>
                <Button
                  type="button"
                  size="icon"
                  variant="ghost"
                  aria-label={`Remove ${r.email}`}
                  onClick={() => setRows((list) => list.filter((x) => x.email !== r.email))}
                >
                  <X className="size-4" />
                </Button>
              </li>
            ))}
          </ul>
        ) : null}
      </form>

      <dl className="m-0 grid gap-x-6 gap-y-3 sm:grid-cols-2">
        {inviteRoles
          .filter((r) => r.value !== 'owner' || org.role === 'owner')
          .map((r) => (
            <div key={r.value} className="flex flex-col gap-1">
              <dt className="text-body font-semibold text-ink">{r.label}</dt>
              <dd className="m-0 text-secondary text-ink-soft">{r.description}</dd>
            </div>
          ))}
      </dl>

      {refusals.length > 0 ? (
        <Callout tone="rework">
          <span className="flex flex-col gap-1">
            {refusals.map((r) => (
              <span key={r}>Not sent to {r}</span>
            ))}
          </span>
        </Callout>
      ) : null}
    </OnboardingLayout>
  );
}
