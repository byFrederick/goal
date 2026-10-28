// Step 1, "Name your organization" (docs/design/screens/get-started/org.jpg). It creates the organization
// with the signed-in person as its owner (ADR 0032 decision 1), keeps the uses they picked in this
// browser (org-uses.ts), and goes to step 2. The address follows the name until it is edited.
import { Link, useNavigate } from '@tanstack/react-router';
import { Check } from 'lucide-react';
import { type FormEvent, useRef, useState } from 'react';
import type { OrgSummary } from '../../api/types';
import { isSlugTaken, useCreateOrg } from '../../shared/create-org';
import { checkSlug, orgNameMax, orgSlugMax, slugFromName } from '../../shared/org-rules';
import { Button } from '../../ui/button';
import { cn } from '../../ui/cn';
import { Field, Input } from '../../ui/field';
import { OnboardingLayout, PreviewCard } from './onboarding-layout';
import { type OrgUse, orgUses, setOrgUses } from './org-uses';

const useLabels: Record<OrgUse, string> = {
  build: 'Build software',
  operations: 'Run operations',
  support: 'Answer support',
  other: 'Something else',
};

export function OrgStep({ home }: { home: OrgSummary | undefined }) {
  const navigate = useNavigate();
  const nameRef = useRef<HTMLInputElement>(null);
  const slugRef = useRef<HTMLInputElement>(null);
  /** This browser's host, the start of the address the organization gets (goal.example.com/o/goal). */
  const host = window.location.host;

  const [name, setName] = useState('');
  const [slug, setSlug] = useState('');
  /** The address follows the name until the person edits it. */
  const [slugEdited, setSlugEdited] = useState(false);
  const [uses, setUses] = useState<OrgUse[]>(['build']);
  const [submitted, setSubmitted] = useState(false);
  /** The address goald said is taken, while the field still holds it. */
  const [takenSlug, setTakenSlug] = useState<string | null>(null);

  const trimmedName = name.trim();
  const nameError = submitted && !trimmedName ? 'Give the organization a name.' : undefined;
  // The address is checked as it is typed, so a rule it breaks shows before Continue.
  const slugRule = slug === '' ? (submitted ? 'Give the organization an address.' : null) : checkSlug(slug);
  const slugError = slugRule ?? (takenSlug === slug ? `${slug} is taken. Choose another address.` : undefined);

  const create = useCreateOrg((org) => {
    setOrgUses(org.id, uses);
    navigate({ to: '/o/$org/get-started/$step', params: { org: org.slug, step: 'code' } });
  });
  const failed = create.error && !isSlugTaken(create.error);

  const submit = () => {
    setSubmitted(true);
    if (create.isPending) return;
    if (!trimmedName) return nameRef.current?.focus();
    if (checkSlug(slug)) return slugRef.current?.focus();
    create.mutate(
      { name: trimmedName, slug },
      {
        onError: (err) => {
          if (isSlugTaken(err)) {
            setTakenSlug(slug);
            slugRef.current?.focus();
          }
        },
      },
    );
  };

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    submit();
  };

  // Kept in the order of the chips, so the saved list reads the same however the person clicked.
  const toggle = (use: OrgUse) => {
    setUses((current) => orgUses.filter((u) => (u === use ? !current.includes(u) : current.includes(u))));
  };

  return (
    <OnboardingLayout
      step="organization"
      org={home}
      preview={{
        organization: trimmedName ? (
          <PreviewCard active label="Organization" value={trimmedName} detail={`${host}/o/${slug}`} />
        ) : undefined,
      }}
      back={
        home ? (
          <Button asChild size="lg" variant="secondary">
            <Link to="/o/$org" params={{ org: home.slug }}>
              Back
            </Link>
          </Button>
        ) : undefined
      }
      action={
        <Button
          type="submit"
          form="org-step-form"
          size="lg"
          variant="primary"
          aria-busy={create.isPending || undefined}
        >
          Continue
        </Button>
      }
    >
      <div className="flex flex-col gap-2">
        <h1 className="m-0 font-numeral text-kpi tracking-display">Name your organization</h1>
        <p className="m-0 text-heading font-normal text-ink-soft">
          An organization holds your factories, the people who run them and what they connect to.
        </p>
      </div>
      <form id="org-step-form" onSubmit={onSubmit} noValidate className="flex flex-col gap-6">
        <Field label="Organization name" error={nameError}>
          <Input
            ref={nameRef}
            value={name}
            maxLength={orgNameMax}
            autoComplete="organization"
            onChange={(e) => {
              setName(e.target.value);
              if (!slugEdited) setSlug(slugFromName(e.target.value));
            }}
          />
        </Field>
        <Field label="Address" error={slugError}>
          <div className="flex min-w-0 items-center rounded-control border border-rule bg-card focus-within:border-muted">
            <span className="shrink-0 pl-3 text-body text-muted">{host}/o/</span>
            <Input
              ref={slugRef}
              value={slug}
              maxLength={orgSlugMax}
              autoComplete="off"
              autoCapitalize="none"
              spellCheck={false}
              className="border-0 bg-transparent pl-1 hover:border-0"
              onChange={(e) => {
                setSlug(e.target.value.trim());
                setSlugEdited(true);
              }}
            />
          </div>
        </Field>
        <fieldset className="m-0 flex min-w-0 flex-col gap-2 border-0 p-0">
          <legend className="mb-2 p-0 text-sm font-medium text-ink">What will your factories do?</legend>
          <div className="flex flex-wrap gap-2">
            {orgUses.map((use) => {
              const on = uses.includes(use);
              return (
                <button
                  key={use}
                  type="button"
                  aria-pressed={on}
                  onClick={() => toggle(use)}
                  className={cn(
                    'inline-flex h-10 items-center gap-2 rounded-cell border px-3 font-control text-body transition-colors duration-200',
                    on ? 'border-run bg-run-tint text-run-ink' : 'border-rule bg-card text-ink-soft hover:bg-panel',
                  )}
                >
                  {on ? <Check className="size-3" strokeWidth={2.25} aria-hidden="true" /> : null}
                  {useLabels[use]}
                </button>
              );
            })}
          </div>
          <p className="m-0 text-secondary text-muted">Pick any. Goal suggests templates from this.</p>
        </fieldset>
        {failed ? (
          <div className="flex flex-wrap items-center gap-3">
            <p role="alert" className="m-0 text-secondary text-rework-ink">
              Could not create the organization: {create.error.message}
            </p>
            <Button variant="secondary" onClick={submit}>
              Retry
            </Button>
          </div>
        ) : null}
      </form>
    </OnboardingLayout>
  );
}
