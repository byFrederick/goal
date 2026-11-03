// What the template panel and the custom panel share: the environment a tool goes to, the lines it adds,
// the footer with Add to draft (which creates the new secrets, writes the draft's environment file and
// saves it), the Allow, Ask first and Off rows of a server, and a secret slot with its New option.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { useRef, useState } from 'react';
import { keys } from '../../../api/queries';
import { createSecret } from '../../../api/secrets';
import type { TemplateAdds, TemplateSecret } from '../../../api/types';
import { useFactory } from '../../../shared/factory';
import { plural } from '../../../shared/format';
import { useOrg } from '../../../shared/org';
import { Button } from '../../../ui/button';
import { Callout } from '../../../ui/callout';
import { cn } from '../../../ui/cn';
import { Field, Input } from '../../../ui/field';
import { Select } from '../../../ui/select';
import { SuccessLabel, successHoldMs } from '../../../ui/success-label';
import { WriteOnly } from '../../connections/parts';
import { SecretInput } from '../../connections/secret-input';
import { refusalText } from '../../connections/words';
import { useDraftSession } from '../draft/use-draft';
import { addsPreview, addToText, envPath, modeOf, newEnvText, type ToolMode } from '../environments/env-file';
import { groupTools, namesText } from '../environments/mcp-card';
import { ModeControl, useSecretOptions } from '../environments/parts';
import { useEnvironmentAgents, useEnvironmentNames } from '../environments/use-environments';
import type { SlotPick } from './apply-template';

/** "Add to": the environment, and the agents that get it. */
export function EnvironmentPicker({ value, onChange }: { value: string; onChange: (name: string) => void }) {
  const { names } = useEnvironmentNames();
  const agents = useEnvironmentAgents();
  const options = (names.includes(value) ? names : [value, ...names]).map((n) => ({ value: n, label: n }));
  const used = agents?.[value] ?? [];
  return (
    <Field label="Add to" help={used.length > 0 ? used.join(', ') : 'No agent uses it yet.'}>
      <Select value={value} options={options} onValueChange={onChange} className="font-mono" />
    </Field>
  );
}

/** "Adds to environments/<name>.yaml": the lines, each with a plus on the tint of an addition. */
export function Preview({ environment, adds }: { environment: string; adds: TemplateAdds }) {
  const lines = addsPreview(adds);
  return (
    <div className="flex min-w-0 flex-col gap-2">
      <h3 className="m-0 text-body font-control">Adds to {envPath(environment)}</h3>
      {lines.length === 0 ? (
        <p className="m-0 rounded-control border border-line bg-shell px-3 py-2.5 text-secondary text-muted">
          Nothing yet. Fill in the fields above.
        </p>
      ) : (
        <pre className="m-0 max-w-full overflow-x-auto rounded-control border border-line bg-shell py-2 font-mono text-secondary">
          {lines.map((line, i) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: lines repeat, their place is their identity
            <span key={i} className="grid grid-cols-[1.75rem_minmax(0,1fr)] bg-ok-tint text-ok-ink">
              <span aria-hidden="true" className="select-none text-center">
                +
              </span>
              <span className="whitespace-pre pr-3">{line}</span>
            </span>
          ))}
        </pre>
      )}
    </div>
  );
}

/** What Add to draft needs: the lines, the secrets to create first, and the environment. */
export interface Apply {
  environment: string;
  adds: TemplateAdds;
  creates: { name: string; value: string }[];
  /** The tool's name in "gets <tool> when you publish". */
  title: string;
  disabled: boolean;
  /** Why it is disabled, in a sentence. */
  reason?: string | undefined;
}

/** Creates the new secrets for this factory, once each: a name created is not created again by a test and then Add to draft. */
export function useCreateSecrets() {
  const org = useOrg();
  const factory = useFactory();
  const client = useQueryClient();
  const made = useRef(new Set<string>());
  return async (creates: readonly { name: string; value: string }[]) => {
    for (const c of creates) {
      if (made.current.has(c.name)) continue;
      await createSecret(org.id, {
        name: c.name,
        value: c.value,
        description: '',
        scope: { all_factories: false, factories: [factory.id] },
      });
      made.current.add(c.name);
    }
    if (creates.length > 0) await client.invalidateQueries({ queryKey: keys.secrets(org.id) });
  };
}

export function AddFooter({ apply, onAdded }: { apply: Apply; onAdded?: () => void }) {
  const session = useDraftSession();
  const org = useOrg();
  const navigate = useNavigate();
  const factory = useFactory();
  const create = useCreateSecrets();
  const mutation = useMutation({
    mutationFn: async () => {
      await create(apply.creates);
      const path = envPath(apply.environment);
      const text = await session.fileText(path).catch(() => newEnvText(apply.environment));
      session.editFile(path, addToText(text, apply.adds));
      await session.flush();
      const refused = session.getSnapshot().error;
      if (refused) throw refused;
    },
    gcTime: 0,
    onSuccess: () => {
      onAdded?.();
      // The success stays in view for a beat, then the Environments page shows the new lines.
      setTimeout(
        () =>
          void navigate({
            to: '/o/$org/f/$factory/definition/$section',
            params: { org: org.slug, factory: factory.name, section: 'environments' },
            search: { environment: apply.environment },
          }),
        successHoldMs,
      );
    },
  });
  return (
    <div className="flex flex-col gap-3 border-t border-line pt-4">
      {mutation.isError ? (
        <div role="alert">
          <Callout tone="rework">{refusalText(mutation.error)}</Callout>
        </div>
      ) : null}
      <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
        <p className="m-0 order-2 text-secondary text-muted md:order-1">
          {apply.disabled && apply.reason
            ? apply.reason
            : `Agents on ${apply.environment} get ${apply.title} when you publish.`}
        </p>
        <Button
          variant="action"
          className="order-1 h-11 max-md:w-full md:order-2 md:h-10"
          disabled={apply.disabled || mutation.isPending || mutation.isSuccess}
          aria-busy={mutation.isPending || undefined}
          onClick={() => mutation.mutate()}
        >
          <SuccessLabel done={mutation.isSuccess} doneLabel="Added to draft">
            {mutation.isPending ? 'Adding…' : 'Add to draft'}
          </SuccessLabel>
        </Button>
      </div>
    </div>
  );
}

/** One of a server's tools or groups with Allow, Ask first and Off: the tools a test found grouped Read, Change, Delete, else the template's own rules. */
export function ServerModes({
  server,
  tools,
  found,
  onChange,
}: {
  server: string;
  tools: Record<string, ToolMode>;
  found: readonly string[] | undefined;
  onChange: (next: Record<string, ToolMode>) => void;
}) {
  const view = { headers: {}, env: {}, tools };
  const groups = found ? groupTools(found) : [];
  const rows = groups.length > 0 ? null : Object.keys(tools);
  const setGroup = (list: readonly string[], mode: ToolMode) => {
    const next = { ...tools };
    for (const tool of list) if (modeOf({ ...view, tools: next }, tool) !== mode || tool in next) next[tool] = mode;
    onChange(next);
  };
  const row =
    'flex flex-col gap-2 border-b border-line px-3 py-2.5 last:border-b-0 md:flex-row md:items-center md:justify-between md:gap-4';
  return (
    <div className="flex flex-col gap-2">
      <h3 className="m-0 text-body font-control">What agents may do</h3>
      <ul className="m-0 flex list-none flex-col rounded-control border border-line bg-card p-0">
        {groups.map(({ group, tools: list }) => {
          const modes = new Set(list.map((t) => modeOf(view, t)));
          return (
            <li key={group} className={row}>
              <span className="min-w-0 text-secondary">
                <span className="font-control">{group}</span>{' '}
                <span className="text-muted">
                  {plural(list.length, 'tool')}: <span className="font-mono">{namesText(list, 2)}</span>
                </span>
              </span>
              <ModeControl
                label={`${group} tools of ${server}`}
                value={modes.size === 1 ? [...modes][0] : undefined}
                onChange={(m) => setGroup(list, m)}
              />
            </li>
          );
        })}
        {rows?.map((pattern) => (
          <li key={pattern} className={row}>
            <span className="min-w-0 font-mono text-secondary">{pattern}</span>
            <ModeControl
              label={`${pattern} of ${server}`}
              value={tools[pattern]}
              onChange={(m) => onChange({ ...tools, [pattern]: m })}
            />
          </li>
        ))}
        {rows?.length === 0 ? (
          <li className="px-3 py-2.5 text-secondary text-muted">
            Test the server to see its tools. Until then every tool is allowed.
          </li>
        ) : null}
      </ul>
    </div>
  );
}

const NEW = '__new__';

/** A secret slot: one of the organization's secrets, or a new one named and pasted here (saved to Secrets when the tool is tested or added). */
export function SlotSecret({
  slot,
  label,
  pick,
  onChange,
  factoryName,
}: {
  slot: TemplateSecret;
  label: string;
  pick: SlotPick;
  onChange: (pick: SlotPick) => void;
  factoryName: string;
}) {
  const { options } = useSecretOptions();
  const [typed, setTyped] = useState(slot.suggest);
  const isNew = pick.kind === 'new';
  return (
    <div className="flex flex-col gap-2">
      <Field label={label}>
        <Select
          value={isNew ? NEW : pick.name}
          options={[...options, { value: NEW, label: `${typed || slot.suggest} (new)` }]}
          className="font-mono"
          onValueChange={(v) =>
            onChange(v === NEW ? { kind: 'new', name: typed, value: '' } : { kind: 'existing', name: v })
          }
        />
      </Field>
      {pick.kind === 'new' ? (
        <>
          <Field label="Name of the new secret">
            <Input
              value={pick.name}
              className="font-mono"
              autoComplete="off"
              spellCheck={false}
              onChange={(e) => {
                setTyped(e.target.value);
                onChange({ ...pick, name: e.target.value });
              }}
            />
          </Field>
          <Field
            label={`${slot.title}`}
            aside={<WriteOnly />}
            help={<>Saved to Secrets and opened to {factoryName}. The file holds only its name.</>}
          >
            <SecretInput
              value={pick.value}
              placeholder={`Paste ${slot.title.toLowerCase()}`}
              onChange={(e) => onChange({ ...pick, value: e.target.value })}
            />
          </Field>
        </>
      ) : null}
    </div>
  );
}

/** A panel's header: the tool's mark, name and one line. */
export function PanelHeader({ mark, title, description }: { mark: string; title: string; description: string }) {
  return (
    <header className="flex items-start gap-3.5">
      <span
        aria-hidden="true"
        className={cn(
          'flex size-11 shrink-0 items-center justify-center rounded-control border border-line bg-card text-body font-control text-ink',
        )}
      >
        {mark}
      </span>
      <div className="min-w-0">
        <h2 className="m-0 text-xl font-medium tracking-[-0.01em]">{title}</h2>
        <p className="m-0 mt-1 text-body text-muted">{description}</p>
      </div>
    </header>
  );
}
