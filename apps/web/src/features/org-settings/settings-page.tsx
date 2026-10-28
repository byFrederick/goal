// Organization settings (canvas "Org-Settings", docs/design/screens/organization/org-settings): General
// (name, address, factories), Slots (the caps goald enforces), Sign-in, Data, and the Danger zone for
// owners. One form saves General and Slots with "Save changes", sending only what changed. The
// controls the picture shows that the API does not have yet render disabled ("Not available yet").
// Leaving with unsaved changes asks first. A new address moves the page to it: links with the old one
// stop working (review decision 2). What the caller may do comes from the organization's `can`
// (spec 3 §10.2): without org:manage the form is shown read-only, with who changes it; the Danger
// zone shows only to owners: Transfer ownership (org:transfer, with sign-in on) and Delete (org:delete).
import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query';
import { useBlocker, useNavigate, useRouter } from '@tanstack/react-router';
import { ChevronDown, TriangleAlert } from 'lucide-react';
import { type FormEvent, type ReactNode, Suspense, useRef, useState } from 'react';
import { toast } from 'sonner';
import { ApiError, patchOrgSettings } from '../../api/client';
import { authConfigQuery, keys, settingsQuery } from '../../api/queries';
import type { OrgSettings, OrgSettingsPatch, OrgSummary } from '../../api/types';
import { factoryMark } from '../../shared/marks';
import { useOrg, useOrgHolds } from '../../shared/org';
import { reasons } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { Chip } from '../../ui/chip';
import { ConfirmDialog } from '../../ui/confirm-dialog';
import { Input } from '../../ui/field';
import { NotAvailable } from '../../ui/not-available';
import { ReadOnly } from '../../ui/read-only';
import { Switch } from '../../ui/switch';
import { ViewOnlyBanner } from '../../ui/view-only-banner';
import { describedBy, SettingsRow, SettingsSection } from './card';
import { DangerZone } from './danger-zone';
import { changedFields, type FieldErrors, formOf, type SettingsForm, slotNote, validate } from './form';

const formId = 'org-settings';

export function OrgSettingsPage() {
  return (
    <Suspense fallback={<SettingsLoading />}>
      <SettingsLoaded />
    </Suspense>
  );
}

/** While the settings load: the title, with the page busy and "Loading" for a screen reader. */
function SettingsLoading() {
  return (
    <div aria-busy="true" className="flex flex-col gap-5">
      <h1 className="m-0 text-page-title tracking-display">Organization settings</h1>
      <span className="sr-only">Loading</span>
    </div>
  );
}

function SettingsLoaded() {
  const org = useOrg();
  const { data } = useSuspenseQuery(settingsQuery(org.id));
  return <SettingsScreen key={org.id} org={org} settings={data} />;
}

/** Why a save failed, in the form's words; a taken address is the address field's error instead. */
function saveError(err: Error): string {
  if (!(err instanceof ApiError)) return 'goald is not answering. Your changes are still here; save again once it is.';
  return `Not saved: ${err.detail ?? err.title}`;
}

/** The "On this page" links, in the order of the sections. Danger zone is listed only when it shows. */
const sections = [
  ['general', 'General'],
  ['slots', 'Slots'],
  ['signin', 'Sign-in'],
  ['data', 'Data'],
] as const;

function SettingsScreen({ org, settings }: { org: OrgSummary; settings: OrgSettings }) {
  const client = useQueryClient();
  const router = useRouter();
  const navigate = useNavigate();
  const mayManage = useOrgHolds('org:manage');
  const mayDelete = useOrgHolds('org:delete');
  // Ownership goes from one signed-in person to another: nobody owns anything with sign-in off.
  const auth = useSuspenseQuery(authConfigQuery).data;
  const mayTransfer = useOrgHolds('org:transfer') && auth.mode === 'session';

  // The settings as goald last answered them, and the form over them.
  const [saved, setSaved] = useState(settings);
  const [form, setForm] = useState<SettingsForm>(() => formOf(settings));
  const [blurred, setBlurred] = useState<Record<string, boolean>>({});
  const [submitted, setSubmitted] = useState(false);
  const [takenSlug, setTakenSlug] = useState<string | null>(null);

  const patch = changedFields(saved, form);
  const dirty = Object.keys(patch).length > 0;
  const errors: FieldErrors = validate(form);
  if (takenSlug !== null && takenSlug === form.slug.trim()) errors.slug = 'Taken';
  // An error shows once the person left the field or tried to save, not while they first type.
  const shown = (key: keyof FieldErrors) => (submitted || blurred[key] ? errors[key] : undefined);
  const blur = (key: keyof FieldErrors) => () => setBlurred((b) => ({ ...b, [key]: true }));

  // Leaving with unsaved changes asks first; saving a new address and deleting leave on purpose.
  const leaving = useRef(false);
  const blocker = useBlocker({
    shouldBlockFn: () => !leaving.current,
    disabled: !dirty,
    enableBeforeUnload: dirty,
    withResolver: true,
  });

  const save = useMutation({
    mutationFn: (body: OrgSettingsPatch) => patchOrgSettings(org.id, body),
    onSuccess: async (next, body) => {
      client.setQueryData(keys.settings(org.id), next);
      setSaved(next);
      setForm(formOf(next));
      setSubmitted(false);
      setBlurred({});
      toast.success('Saved');
      // The sidebar and the route context read the organization from the list.
      await client.refetchQueries({ queryKey: keys.orgs });
      if (body.slug) {
        leaving.current = true;
        await navigate({ to: '/o/$org/settings', params: { org: next.slug }, replace: true });
        leaving.current = false;
      } else {
        await router.invalidate();
      }
    },
    onError: (err, body) => {
      if (err instanceof ApiError && err.code === 'slug-taken' && body.slug) setTakenSlug(body.slug);
    },
  });

  const set = (key: 'name' | 'slug' | 'max') => (e: { target: { value: string } }) =>
    setForm((f) => ({ ...f, [key]: e.target.value }));
  const setCap = (factory: string) => (e: { target: { value: string } }) =>
    setForm((f) => ({ ...f, factories: { ...f.factories, [factory]: e.target.value } }));

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (!mayManage) return;
    setSubmitted(true);
    if (Object.keys(errors).length > 0) {
      requestAnimationFrame(() => document.querySelector<HTMLElement>(`#${formId} [aria-invalid="true"]`)?.focus());
      return;
    }
    if (dirty && !save.isPending) save.mutate(patch);
  };

  const slug = form.slug.trim();
  const note = slotNote(form);
  const failed = save.error && !(save.error instanceof ApiError && save.error.code === 'slug-taken');
  const factories = saved.slots.factories;
  const inSignIn = (method: 'email_code' | 'google') => auth.methods.includes(method);

  return (
    <>
      <header className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="m-0 text-page-title tracking-display">Organization settings</h1>
        {mayManage ? (
          <Button type="submit" form={formId} variant="primary" disabled={!dirty || save.isPending}>
            {save.isPending ? 'Saving…' : 'Save changes'}
          </Button>
        ) : null}
      </header>

      {mayManage ? null : <ViewOnlyBanner>{reasons.settings(saved.name)}</ViewOnlyBanner>}

      {failed ? (
        <p role="alert" className="m-0 rounded-control border border-rework-tint bg-rework-tint px-4 py-3 text-sm">
          {saveError(save.error)}
        </p>
      ) : null}

      <div className="grid gap-10 xl:grid-cols-[minmax(0,1fr)_200px] xl:items-start">
        <div className="flex min-w-0 flex-col gap-4 xl:max-w-205">
          <ReadOnly when={!mayManage}>
            <form id={formId} onSubmit={onSubmit} noValidate className="flex flex-col gap-4">
              <SettingsSection id="general" title="General">
                <SettingsRow id="oname" label="Name" help="Shown in the sidebar and in emails" error={shown('name')}>
                  <Input
                    id="oname"
                    value={form.name}
                    name="name"
                    onChange={set('name')}
                    onBlur={blur('name')}
                    maxLength={80}
                    autoComplete="organization"
                    aria-invalid={shown('name') ? true : undefined}
                    aria-describedby={describedBy('oname', { help: true, error: shown('name') })}
                  />
                </SettingsRow>
                <SettingsRow id="oslug" label="Address" help="Changing it breaks old links" error={shown('slug')}>
                  <div className="flex h-11 items-stretch overflow-hidden rounded-control border border-rule bg-card md:h-10">
                    <span
                      aria-hidden="true"
                      className="flex min-w-0 items-center border-r border-rule bg-panel px-3 text-secondary text-muted"
                    >
                      {window.location.host}/o/
                    </span>
                    <Input
                      id="oslug"
                      value={form.slug}
                      name="slug"
                      onChange={set('slug')}
                      onBlur={blur('slug')}
                      maxLength={40}
                      autoComplete="off"
                      autoCapitalize="off"
                      spellCheck={false}
                      className="h-full border-0 font-mono hover:border-0 md:text-sm"
                      aria-invalid={shown('slug') ? true : undefined}
                      aria-describedby={describedBy('oslug', { help: true, error: shown('slug') })}
                    />
                  </div>
                  {slug !== saved.slug ? (
                    <p role="status" className="m-0 flex items-center gap-2 text-secondary text-ink">
                      <TriangleAlert size={14} strokeWidth={1.75} aria-hidden="true" className="shrink-0" />
                      Links with the old address stop working.
                    </p>
                  ) : null}
                </SettingsRow>
                <SettingsRow label="Factories" help="Each has its own letter">
                  <div className="flex flex-wrap items-center gap-2">
                    {factories.map((f) => (
                      <Chip key={f.factory_id} tone="plain">
                        <span
                          aria-hidden="true"
                          className="grid size-4 place-items-center rounded-chip border border-rule bg-card text-meta font-semibold"
                        >
                          {factoryMark(f, factories)}
                        </span>
                        {f.name}
                      </Chip>
                    ))}
                  </div>
                </SettingsRow>
              </SettingsSection>

              <SettingsSection id="slots" title="Slots">
                <SettingsRow
                  id="slots-max"
                  label="Slots shared by every factory"
                  help="Blank for no limit."
                  error={shown('max')}
                >
                  <Input
                    id="slots-max"
                    value={form.max}
                    name="max"
                    onChange={set('max')}
                    onBlur={blur('max')}
                    inputMode="numeric"
                    autoComplete="off"
                    className="w-24 md:w-30"
                    aria-invalid={shown('max') ? true : undefined}
                    aria-describedby={describedBy('slots-max', { help: true, error: shown('max') })}
                  />
                </SettingsRow>
                {factories.length > 0 ? (
                  <fieldset className="m-0 flex min-w-0 flex-col border-0 p-0">
                    <legend className="w-full px-5 pt-4 pb-1 text-body font-semibold">
                      Most slots each factory may use
                    </legend>
                    {factories.map((f) => {
                      const key = `factory:${f.factory_id}` as const;
                      const id = `slots-${f.factory_id}`;
                      return (
                        <SettingsRow key={f.factory_id} id={id} label={f.name} error={shown(key)}>
                          <Input
                            id={id}
                            value={form.factories[f.factory_id] ?? ''}
                            name={`slots-${f.name}`}
                            onChange={setCap(f.factory_id)}
                            onBlur={blur(key)}
                            inputMode="numeric"
                            autoComplete="off"
                            className="w-24 md:w-30"
                            aria-invalid={shown(key) ? true : undefined}
                            aria-describedby={describedBy(id, { error: shown(key) })}
                          />
                        </SettingsRow>
                      );
                    })}
                  </fieldset>
                ) : null}
                {note ? <p className="m-0 px-5 pt-1 pb-4 text-secondary text-muted">{note}</p> : null}
              </SettingsSection>
            </form>
          </ReadOnly>

          <SettingsSection id="signin" title="Sign-in">
            <SettingsRow label="Ways to sign in" help={`For members of ${saved.name}`}>
              <div className="flex flex-col">
                <SignInMethod name="GitHub">
                  <NotAvailable label="GitHub sign-in">
                    <Switch checked={false} onCheckedChange={() => undefined} aria-label="GitHub sign-in" />
                  </NotAvailable>
                </SignInMethod>
                <SignInMethod name="Email code">
                  <NotAvailable label="Email code sign-in">
                    <Switch
                      checked={inSignIn('email_code')}
                      onCheckedChange={() => undefined}
                      aria-label="Email code sign-in"
                    />
                  </NotAvailable>
                </SignInMethod>
                <SignInMethod name="Google">
                  <NotAvailable label="Google sign-in">
                    <Switch
                      checked={inSignIn('google')}
                      onCheckedChange={() => undefined}
                      aria-label="Google sign-in"
                    />
                  </NotAvailable>
                </SignInMethod>
                <SignInMethod name="Single sign-on, SAML or OIDC">
                  <NotAvailable label="Single sign-on">
                    <Button type="button" size="sm">
                      Set up
                    </Button>
                  </NotAvailable>
                </SignInMethod>
              </div>
            </SettingsRow>
            <SettingsRow label="Sessions" help="Sign in again after">
              <NotAvailable label="Sessions">
                <Button type="button" size="md">
                  30 days
                  <ChevronDown size={12} strokeWidth={1.75} aria-hidden="true" />
                </Button>
              </NotAvailable>
            </SettingsRow>
          </SettingsSection>

          <SettingsSection id="data" title="Data">
            <SettingsRow label="Definitions and knowledge" help="Versioned files, one folder per factory">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <span className="text-secondary">Goal’s storage, Cloudflare R2</span>
                <NotAvailable label="Use your own bucket">
                  <Button type="button" size="sm">
                    Use your own bucket
                  </Button>
                </NotAvailable>
              </div>
            </SettingsRow>
            <SettingsRow label="Run logs" help="Events and tool calls of each run">
              <div className="flex items-center gap-2">
                <span className="text-secondary text-ink-soft">Keep for</span>
                <NotAvailable label="Keep run logs for">
                  <Button type="button" size="md">
                    90 days
                    <ChevronDown size={12} strokeWidth={1.75} aria-hidden="true" />
                  </Button>
                </NotAvailable>
              </div>
            </SettingsRow>
            <SettingsRow label="Audit log" help="Who changed what, kept for a year">
              <div className="flex flex-wrap gap-2">
                <NotAvailable label="Open audit log">
                  <Button type="button" size="sm">
                    Open audit log
                  </Button>
                </NotAvailable>
                <NotAvailable label="Export audit log">
                  <Button type="button" size="sm">
                    Export
                  </Button>
                </NotAvailable>
              </div>
            </SettingsRow>
          </SettingsSection>

          {mayDelete || mayTransfer ? (
            <DangerZone
              org={org}
              name={saved.name}
              deletes={mayDelete}
              transfers={mayTransfer}
              onLeave={() => {
                leaving.current = true;
              }}
            />
          ) : null}
        </div>

        <nav
          aria-label="On this page"
          className="hidden xl:sticky xl:top-5 xl:flex xl:flex-col xl:gap-1 text-secondary"
        >
          {sections.map(([id, label]) => (
            <a
              key={id}
              href={`#${id}`}
              className="rounded-control px-3 py-1 text-ink-soft hover:bg-hover hover:text-ink"
            >
              {label}
            </a>
          ))}
          {mayDelete || mayTransfer ? (
            <a href="#danger" className="rounded-control px-3 py-1 text-ink-soft hover:bg-hover hover:text-ink">
              Danger zone
            </a>
          ) : null}
        </nav>
      </div>

      <ConfirmDialog
        open={blocker.status === 'blocked'}
        onOpenChange={(open) => {
          if (!open) blocker.reset?.();
        }}
        title="Leave without saving?"
        description={`Your changes to ${saved.name}’s settings are not saved.`}
        confirmLabel="Leave"
        cancelLabel="Stay"
        tone="danger"
        onConfirm={() => blocker.proceed?.()}
      />
    </>
  );
}

/** One way to sign in, in the Sign-in row: its name, and its switch or button at the right. */
function SignInMethod({ name, children }: { name: string; children: ReactNode }) {
  return (
    <div className="flex min-h-11 items-center gap-3 border-t border-rule py-2 first:border-t-0 first:pt-0 md:min-h-10">
      <span className="flex-1 text-body">{name}</span>
      {children}
    </div>
  );
}
