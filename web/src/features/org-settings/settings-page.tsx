// Organization settings (C §10.9, canvas "Org-Settings"): one form for General (name, short
// mark, address) and the slot limits, saved by "Save changes" with only what changed; Model
// access and Vault arrive with credentials (phase 3); the Danger zone deletes the organization.
// Leaving with unsaved changes asks first. A new address moves the page to it: links with the
// old one stop working (review decision 2).
import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query';
import { useBlocker, useNavigate, useRouter } from '@tanstack/react-router';
import { TriangleAlert } from 'lucide-react';
import { type FormEvent, useRef, useState } from 'react';
import { toast } from 'sonner';
import { ApiError, patchOrgSettings } from '../../api/client';
import { keys, settingsQuery } from '../../api/queries';
import type { OrgSettings, OrgSettingsPatch, OrgSummary } from '../../api/types';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { ConfirmDialog } from '../../ui/confirm-dialog';
import { Field, Input } from '../../ui/field';
import { DangerZone } from './danger-zone';
import { changedFields, type FieldErrors, formOf, type SettingsForm, slotNote, validate } from './form';
import { SettingsSection } from './section';

const formId = 'org-settings';

export function OrgSettingsPage() {
  const org = useOrg();
  const { data } = useSuspenseQuery(settingsQuery(org.id));
  return <SettingsScreen key={org.id} org={org} settings={data} />;
}

function subtitle(name: string, factories: number): string {
  if (factories === 0) return `Settings for ${name}.`;
  if (factories === 1) return `Settings for ${name} and its factory.`;
  return `Settings for ${name} and all ${factories} of its factories.`;
}

/** Why a save failed, in the form's words; a taken address is the address field's error instead. */
function saveError(err: Error): string {
  if (!(err instanceof ApiError)) return 'goald is not answering. Your changes are still here; save again once it is.';
  return `Not saved: ${err.detail ?? err.title}`;
}

function SettingsScreen({ org, settings }: { org: OrgSummary; settings: OrgSettings }) {
  const client = useQueryClient();
  const router = useRouter();
  const navigate = useNavigate();

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
      // The rail, the sidebar and the route context read the organization from the list.
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

  const set = (key: 'name' | 'mark' | 'slug' | 'max') => (e: { target: { value: string } }) =>
    setForm((f) => ({ ...f, [key]: e.target.value }));
  const setCap = (factory: string) => (e: { target: { value: string } }) =>
    setForm((f) => ({ ...f, factories: { ...f.factories, [factory]: e.target.value } }));

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
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
  const capRow =
    'grid grid-cols-[minmax(0,1fr)_96px] items-center gap-x-4 gap-y-1.5 @[480px]:grid-cols-[minmax(0,1fr)_120px] [&>label]:text-[15px] [&>label]:font-normal';

  return (
    <>
      <header className="flex flex-wrap items-end justify-between gap-4">
        <div className="flex min-w-0 flex-col gap-2">
          <p className="m-0 text-[13px] text-muted md:hidden">{saved.name}</p>
          <h1 className="m-0 text-[34px] leading-[1.05] font-normal tracking-display md:text-5xl">
            Organization settings
          </h1>
          <p className="m-0 text-[15px] text-muted">{subtitle(saved.name, org.factory_count)}</p>
        </div>
        <Button
          type="submit"
          form={formId}
          variant="primary"
          className="h-11 md:h-10"
          disabled={!dirty || save.isPending}
        >
          {save.isPending ? 'Saving…' : 'Save changes'}
        </Button>
      </header>

      {failed ? (
        <p
          role="alert"
          className="m-0 max-w-[980px] rounded-control border border-rework-line bg-rework-tint px-4 py-3 text-sm"
        >
          {saveError(save.error)}
        </p>
      ) : null}

      <div className="flex max-w-[980px] flex-col">
        <form id={formId} onSubmit={onSubmit} noValidate className="flex flex-col">
          <SettingsSection
            title="General"
            lede="The name and short mark people see in the rail, and the address in links."
          >
            <div className="grid grid-cols-[repeat(auto-fit,minmax(min(240px,100%),1fr))] gap-3.5">
              <Field label="Name" error={shown('name')}>
                <Input
                  value={form.name}
                  name="name"
                  onChange={set('name')}
                  onBlur={blur('name')}
                  maxLength={80}
                  autoComplete="organization"
                />
              </Field>
              <Field label="Short mark" help="Two letters, shown at the top of the rail" error={shown('mark')}>
                <Input
                  value={form.mark}
                  name="mark"
                  onChange={set('mark')}
                  onBlur={blur('mark')}
                  maxLength={2}
                  autoComplete="off"
                  spellCheck={false}
                />
              </Field>
              <Field
                label="Address"
                error={shown('slug')}
                help={
                  <>
                    <span className="break-all">
                      {window.location.host}/o/{slug || '…'}
                    </span>
                    <span role="status" className="block">
                      {slug !== saved.slug ? (
                        <span className="mt-1 flex items-center gap-1.5 text-ink">
                          <TriangleAlert size={14} strokeWidth={1.75} aria-hidden="true" className="shrink-0" />
                          Links with the old address stop working.
                        </span>
                      ) : null}
                    </span>
                  </>
                }
              >
                <Input
                  value={form.slug}
                  name="slug"
                  onChange={set('slug')}
                  onBlur={blur('slug')}
                  maxLength={40}
                  autoComplete="off"
                  autoCapitalize="off"
                  spellCheck={false}
                  className="font-mono md:text-sm"
                />
              </Field>
            </div>
          </SettingsSection>

          <SettingsSection
            title="Model access"
            lede={`How ${saved.name}’s agents reach Claude. Slots are how many agents can run at once across all factories.`}
          >
            <p className="m-0 rounded-control border border-line bg-surface px-4 py-3.5 text-sm leading-normal text-muted">
              Model access is a platform setting of this install. Choosing a subscription or API key arrives with
              credentials.
            </p>
            <div className="flex flex-col gap-3 rounded-card border border-line bg-card p-4">
              <Field
                label="Slots shared by every factory"
                help="Blank for no limit."
                error={shown('max')}
                className={capRow}
              >
                <Input
                  value={form.max}
                  name="max"
                  onChange={set('max')}
                  onBlur={blur('max')}
                  inputMode="numeric"
                  autoComplete="off"
                />
              </Field>
              {saved.slots.factories.length > 0 ? (
                <fieldset className="m-0 flex min-w-0 flex-col gap-2.5 border-0 border-t border-line p-0 pt-3">
                  <legend className="float-left mb-1 w-full p-0 text-[15px] font-medium">
                    Most slots each factory may use
                  </legend>
                  {saved.slots.factories.map((f) => {
                    const key = `factory:${f.factory_id}` as const;
                    return (
                      <Field key={f.factory_id} label={f.name} error={shown(key)} className={capRow}>
                        <Input
                          value={form.factories[f.factory_id] ?? ''}
                          name={`slots-${f.name}`}
                          onChange={setCap(f.factory_id)}
                          onBlur={blur(key)}
                          inputMode="numeric"
                          autoComplete="off"
                        />
                      </Field>
                    );
                  })}
                </fieldset>
              ) : null}
              {note ? <p className="m-0 text-[13px] leading-snug text-muted">{note}</p> : null}
            </div>
          </SettingsSection>

          <SettingsSection
            title="Vault"
            lede={`Where ${saved.name}’s credentials are kept. Agents never see them; Goal adds them when a tool runs.`}
          >
            <p className="m-0 rounded-control border border-dashed border-rule px-4 py-3.5 text-sm text-muted">
              Arrives with credentials
            </p>
          </SettingsSection>
        </form>

        <DangerZone
          org={org}
          name={saved.name}
          onLeave={() => {
            leaving.current = true;
          }}
        />
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
