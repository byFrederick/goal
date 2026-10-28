// "Create one" on the New credential page (canvas docs/design/screens/connections/credential-new.jpg):
// a name, a type, the value (write-only, SecretInput), what it is for, the factories and the agents
// that may open it, and its expiry with the rotation reminder. The value leaves the page once, in the
// create request. The mutation is not kept in the cache (gcTime 0), and the URL holds none of it.
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { type FormEvent, type ReactNode, useId, useState } from 'react';
import { toast } from 'sonner';
import { createCredential } from '../../api/credentials';
import { credentialsQuery, keys, versionModelQuery } from '../../api/queries';
import type { CredentialType, Factory, NewCredential } from '../../api/types';
import { FactoryBadge } from '../../app/factory-badge';
import { factoryMark } from '../../shared/marks';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { cn } from '../../ui/cn';
import { Field, Input } from '../../ui/field';
import { agentNamesOf } from '../agents/agents-line';
import { SecretInput } from '../connections/secret-input';
import { refusalText } from '../connections/words';
import { nameProblem, remindOptions, similarName, typeLabels } from './words';

const types = Object.keys(typeLabels) as CredentialType[];

const selectClass =
  'h-11 w-full min-w-0 rounded-control border border-rule bg-card px-3 text-base text-ink md:h-10 md:text-body';

/** A native select with its label and help line wired for screen readers; `Field` wires only its own inputs. */
function SelectField({
  label,
  help,
  children,
}: {
  label: string;
  help?: string;
  children: (control: { id: string; describedBy: string | undefined }) => ReactNode;
}) {
  const id = useId();
  const helpId = `${id}-help`;
  return (
    <div className="flex min-w-0 flex-col gap-2">
      <label htmlFor={id} className="text-sm font-medium text-ink">
        {label}
      </label>
      {children({ id, describedBy: help ? helpId : undefined })}
      {help ? (
        <p id={helpId} className="m-0 text-secondary text-muted">
          {help}
        </p>
      ) : null}
    </div>
  );
}

/** The form. `org` is the organization's id; `slug` is its URL name, for the way back to the list. */
export function CreateCredentialCard({ org, slug, factories }: { org: string; slug: string; factories: Factory[] }) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const existing = useQuery(credentialsQuery(org)).data ?? [];
  const [name, setName] = useState('');
  const [type, setType] = useState<CredentialType>('api_key');
  const [value, setValue] = useState('');
  const [description, setDescription] = useState('');
  const [all, setAll] = useState(false);
  const [chosen, setChosen] = useState<string[]>([]);
  // Per factory id, the agents that get the credential in their run's box.
  const [agents, setAgents] = useState<Record<string, string[]>>({});
  const [expires, setExpires] = useState('');
  const [remind, setRemind] = useState(14);
  const [attempted, setAttempted] = useState(false);

  // One model query per factory, for the agent names it defines; a factory with no current version
  // has none, and its group says so.
  const models = useQueries({ queries: factories.map((f) => versionModelQuery(org, f.id, 'current')) });
  const modelOf = new Map(factories.map((f, i) => [f.id, models[i]] as const));

  const create = useMutation({
    mutationFn: (credential: NewCredential) => createCredential(org, credential),
    gcTime: 0,
    onSuccess: async (saved) => {
      await client.invalidateQueries({ queryKey: keys.credentials(org) });
      toast.success(`${saved.name} created`, { description: 'Nobody can read its value back.' });
      void navigate({ to: '/o/$org/credentials', params: { org: slug } });
    },
  });

  const trimmed = name.trim();
  const nameError = attempted ? nameProblem(trimmed) : undefined;
  const scopeError =
    attempted && !all && chosen.length === 0 ? 'Choose the factories that can use it, or all factories.' : undefined;
  const valueError = attempted && value === '' ? 'Enter the value.' : undefined;
  const similar = trimmed
    ? similarName(
        trimmed,
        existing.map((c) => c.name),
      )
    : undefined;
  const scopeFactories = all ? factories : factories.filter((f) => chosen.includes(f.id));

  const toggleFactory = (id: string) =>
    setChosen((list) => (list.includes(id) ? list.filter((x) => x !== id) : [...list, id]));

  const toggleAgent = (factory: string, agent: string) =>
    setAgents((prev) => {
      const list = prev[factory] ?? [];
      return { ...prev, [factory]: list.includes(agent) ? list.filter((a) => a !== agent) : [...list, agent] };
    });

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setAttempted(true);
    if (nameProblem(trimmed) || value === '' || (!all && chosen.length === 0)) return;
    const scopedAgents = Object.fromEntries(
      scopeFactories.flatMap((f) => (agents[f.id]?.length ? [[f.id, agents[f.id] ?? []]] : [])),
    );
    create.mutate({
      name: trimmed,
      type,
      value,
      description: description.trim(),
      scope: { all_factories: all, factories: all ? [] : chosen },
      agents: scopedAgents,
      ...(expires
        ? { expires_at: new Date(`${expires}T00:00:00Z`).toISOString(), remind_days: remind }
        : { remind_days: 0 }),
    });
  };

  return (
    <form
      onSubmit={submit}
      aria-labelledby="create-heading"
      className="flex min-w-0 flex-col gap-5 rounded-card border border-line bg-card p-6"
    >
      <h2 id="create-heading" className="m-0 text-heading">
        Create one
      </h2>

      <div className="grid gap-4 @[560px]:grid-cols-2">
        <Field
          label="Name"
          error={nameError}
          help={
            <>
              Used as <span className="font-mono">{`{{${trimmed || 'name'}}}`}</span> in settings
            </>
          }
        >
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            onBlur={() => setAttempted((a) => a || name !== '')}
            className="font-mono"
            autoComplete="off"
            spellCheck={false}
            required
          />
        </Field>
        <SelectField label="Type" help="The type decides how runs receive it">
          {(control) => (
            <select
              id={control.id}
              aria-describedby={control.describedBy}
              value={type}
              onChange={(e) => setType(e.target.value as CredentialType)}
              className={selectClass}
            >
              {types.map((t) => (
                <option key={t} value={t}>
                  {typeLabels[t]}
                </option>
              ))}
            </select>
          )}
        </SelectField>
      </div>

      <Field
        label="Value"
        error={valueError}
        help="Shown only while you type. After saving, nobody can read it back, you included."
      >
        <SecretInput value={value} onChange={(e) => setValue(e.target.value)} required />
      </Field>

      <Field
        label={
          <>
            What it is for <span className="font-normal text-muted">optional</span>
          </>
        }
      >
        <Input value={description} onChange={(e) => setDescription(e.target.value)} maxLength={500} />
      </Field>

      <fieldset className="flex min-w-0 flex-col gap-2 border-0 p-0">
        <legend className="mb-2 p-0 text-sm font-medium text-ink">Factories that can use it</legend>
        <div className="flex flex-wrap items-center gap-2">
          {factories.map((f) => {
            const pressed = all || chosen.includes(f.id);
            return (
              <button
                key={f.id}
                type="button"
                aria-pressed={pressed}
                disabled={all}
                onClick={() => toggleFactory(f.id)}
                className="inline-flex h-11 items-center gap-2 rounded-control border border-rule bg-card px-3 text-body font-control text-ink transition-colors duration-200 hover:border-muted aria-pressed:border-run aria-pressed:bg-run-tint aria-pressed:text-run-ink disabled:opacity-60 md:h-9"
              >
                <FactoryBadge mark={factoryMark(f, factories)} className="size-5" />
                {f.title}
              </button>
            );
          })}
          <label className="ml-2 inline-flex min-h-11 items-center gap-2 text-body text-ink md:min-h-9">
            <input
              type="checkbox"
              checked={all}
              onChange={(e) => setAll(e.target.checked)}
              className="size-4 accent-run"
            />
            All factories, including new ones
          </label>
        </div>
        {factories.length === 0 && !all ? (
          <p className="m-0 text-secondary text-muted">No factories yet. Choose all factories, including new ones.</p>
        ) : null}
        {scopeError ? <p className="m-0 text-secondary text-rework-ink">{scopeError}</p> : null}
      </fieldset>

      <fieldset className="flex min-w-0 flex-col gap-2 border-0 p-0">
        <legend className="mb-2 p-0 text-sm font-medium text-ink">Agents that can use it</legend>
        {scopeFactories.length > 0 ? (
          <div className="overflow-hidden rounded-card border border-line">
            {scopeFactories.map((f) => {
              const model = modelOf.get(f.id);
              const names = model?.data ? agentNamesOf(model.data.model) : [];
              return (
                <section
                  key={f.id}
                  aria-labelledby={`agents-${f.id}`}
                  className="border-t border-line first:border-t-0"
                >
                  <h3
                    id={`agents-${f.id}`}
                    className="m-0 flex items-center gap-2 bg-panel px-3 py-2 text-meta font-control text-ink-soft"
                  >
                    <FactoryBadge mark={factoryMark(f, factories)} className="size-4" />
                    {f.title}
                  </h3>
                  {model?.isPending ? (
                    <p role="status" aria-busy="true" className="m-0 px-3 py-2 text-secondary text-muted">
                      Loading agents
                    </p>
                  ) : names.length === 0 ? (
                    <p className="m-0 px-3 py-2 text-secondary text-muted">No agents defined yet.</p>
                  ) : (
                    <div className="flex flex-wrap gap-x-5 gap-y-2 px-3 py-2">
                      {names.map((agent) => (
                        <label
                          key={agent}
                          className="inline-flex min-h-11 items-center gap-2 text-body text-ink md:min-h-8"
                        >
                          <input
                            type="checkbox"
                            checked={(agents[f.id] ?? []).includes(agent)}
                            onChange={() => toggleAgent(f.id, agent)}
                            className="size-4 accent-run"
                          />
                          {agent}
                        </label>
                      ))}
                    </div>
                  )}
                </section>
              );
            })}
          </div>
        ) : (
          <p className="m-0 rounded-card border border-line px-3 py-2 text-secondary text-muted">
            Choose factories first. Their agents show here.
          </p>
        )}
        <p className="m-0 text-secondary text-muted">
          Only these agents get it in their run&apos;s box. Connections that name it get it too.
        </p>
      </fieldset>

      <div className="grid gap-4 @[560px]:grid-cols-2">
        <Field label="Expires">
          <Input type="date" value={expires} onChange={(e) => setExpires(e.target.value)} />
        </Field>
        <SelectField label="Remind me to rotate it">
          {(control) => (
            <select
              id={control.id}
              aria-describedby={control.describedBy}
              value={remind}
              disabled={!expires}
              onChange={(e) => setRemind(Number(e.target.value))}
              className={cn(selectClass, !expires && 'opacity-60')}
            >
              {remindOptions.map((o) => (
                <option key={o.days} value={o.days}>
                  {o.label}
                </option>
              ))}
            </select>
          )}
        </SelectField>
      </div>

      {create.isError ? (
        <div role="alert">
          <Callout tone="rework">{refusalText(create.error)}</Callout>
        </div>
      ) : null}

      <div className="flex flex-wrap items-center gap-3 border-t border-line pt-4">
        {similar ? (
          <p className="m-0 text-secondary text-muted">
            Replaces nothing. The old {similar} keeps working until you remove it.
          </p>
        ) : null}
        <Button type="submit" variant="primary" className="ml-auto" disabled={create.isPending}>
          {create.isPending ? 'Creating…' : 'Create credential'}
        </Button>
      </div>
    </form>
  );
}
