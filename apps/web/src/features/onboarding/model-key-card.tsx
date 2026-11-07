// The model key (ADR 0037 decision 7), asked once on the Line step and again on the First item step: a Claude
// subscription token or an Anthropic API key. One form for every template and for Start empty. Save keeps the
// key as an organization secret open to this factory only and writes `runtime.modelKey` into the draft's
// factory.yaml through the draft session, which autosaves it. A name that is taken never has its value
// replaced: the card offers the existing secret or a new name, as the Code step does.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { type FormEvent, useId, useMemo, useState } from 'react';
import { ApiError } from '../../api/client';
import { keys } from '../../api/queries';
import { createSecret } from '../../api/secrets';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { SectionCard } from '../../ui/card';
import { Field, Input } from '../../ui/field';
import { Skeleton } from '../../ui/skeleton';
import { modelKeyOf } from '../factory-editor/basics/model-key';
import { useDraftSession } from '../factory-editor/draft/use-draft';
import { useFileText } from '../factory-editor/environments/use-environments';
import { SecretInput } from '../secrets/secret-input';
import { nameProblem } from '../secrets/words';
import { modelKeyName, withModelKey } from './model-key';
import { openSecretTo } from './open-secret';

/** The secret named by the draft's runtime.modelKey, and whether factory.yaml has been read yet. */
export function useModelKeySecret(): { secret: string | undefined; ready: boolean } {
  const { text, ready } = useFileText('factory.yaml');
  return { secret: useMemo(() => modelKeyOf(text), [text]), ready };
}

export function ModelKeyCard({ org, factory }: { org: string; factory: string }) {
  const titleId = useId();
  const session = useDraftSession();
  const client = useQueryClient();
  const { secret: current, ready } = useModelKeySecret();
  const [changing, setChanging] = useState(false);
  const [value, setValue] = useState('');
  const [touched, setTouched] = useState(false);
  const [name, setName] = useState(modelKeyName);
  const [clash, setClash] = useState(false);
  const [newName, setNewName] = useState('');

  /** Writes the reference into the draft's factory.yaml; the session's text is the latest, typing included. */
  const writeReference = (secret: string) => {
    const text = session.getSnapshot().files.get('factory.yaml')?.text;
    if (text === undefined) return;
    const next = withModelKey(text, secret);
    if (next !== text) session.editFile('factory.yaml', next);
  };

  const save = useMutation({
    mutationFn: async (mode: { kind: 'create'; name: string } | { kind: 'existing'; name: string }) => {
      if (mode.kind === 'existing') await openSecretTo(org, mode.name, factory);
      else {
        await createSecret(org, {
          name: mode.name,
          value,
          description: 'The model key set up in onboarding',
          scope: { all_factories: false, factories: [factory] },
        });
      }
      return mode.name;
    },
    onSuccess: (saved) => {
      void client.invalidateQueries({ queryKey: keys.secrets(org) });
      writeReference(saved);
      setName(saved);
      setClash(false);
      setChanging(false);
      setValue('');
      setTouched(false);
    },
    onError: (error) => {
      if (error instanceof ApiError && error.code === 'secret-name-taken') {
        setNewName('');
        setClash(true);
      }
    },
  });

  const emptyError = touched && value === '' ? 'Paste the model key.' : undefined;
  const submit = (event: FormEvent) => {
    event.preventDefault();
    setTouched(true);
    if (save.isPending || value === '' || clash) return;
    save.mutate({ kind: 'create', name });
  };
  const chooseNewName = () => {
    if (nameProblem(newName) || value === '') return;
    setName(newName);
    save.mutate({ kind: 'create', name: newName });
  };

  const showForm = ready && (current === undefined || changing);

  return (
    <SectionCard
      headingId={titleId}
      title="Model key"
      description="Every agent runs on this key. Paste a Claude subscription token or an Anthropic API key."
    >
      <Skeleton
        ready={ready}
        placeholder={
          <div className="flex flex-col gap-3">
            <div className="h-5 w-24 rounded-control bg-hover" />
            <div className="h-11 rounded-control bg-hover" />
            <div className="h-11 w-32 rounded-control bg-hover" />
          </div>
        }
      >
        {current !== undefined && !changing ? (
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p
              key={current}
              className="m-0 text-secondary text-ink-soft motion-safe:animate-text-states-swap motion-reduce:animate-fade-in"
            >{`Saved as the secret ${current}, open to this factory.`}</p>
            <Button type="button" size="sm" variant="secondary" onClick={() => setChanging(true)}>
              Change the key
            </Button>
          </div>
        ) : null}

        {showForm ? (
          <form onSubmit={submit} noValidate className="flex animate-fade-in flex-col gap-3">
            <Field labelledBy={titleId} error={emptyError} help="Goal saves it as a secret, open to this factory only.">
              <SecretInput
                value={value}
                onChange={(e) => setValue(e.target.value)}
                onBlur={() => setTouched(true)}
                placeholder="Paste the key"
              />
            </Field>

            {clash ? (
              <div role="alert" className="flex animate-fade-in flex-col gap-3">
                <Callout tone="rework">
                  {`A secret named ${name} already exists. Goal never replaces a secret’s value. Use it, or pick a new name for this key.`}
                </Callout>
                <div className="flex flex-wrap items-end gap-3">
                  <Button
                    type="button"
                    variant="secondary"
                    busy={save.isPending}
                    busyLabel="Using…"
                    onClick={() => save.mutate({ kind: 'existing', name })}
                  >
                    Use the existing secret
                  </Button>
                  <Field label="New secret name" error={newName !== '' ? nameProblem(newName) : undefined}>
                    <Input
                      value={newName}
                      onChange={(e) => setNewName(e.target.value)}
                      autoComplete="off"
                      autoCapitalize="off"
                      spellCheck={false}
                      translate="no"
                      className="font-mono md:text-sm"
                    />
                  </Field>
                  <Button
                    type="button"
                    variant="secondary"
                    busy={save.isPending}
                    busyLabel="Saving…"
                    disabled={nameProblem(newName) !== undefined || value === ''}
                    onClick={chooseNewName}
                  >
                    Use this name
                  </Button>
                </div>
              </div>
            ) : null}

            {save.isError && !clash ? (
              <div role="alert" className="animate-fade-in">
                <Callout tone="rework">{`Couldn’t save the key. ${save.error.message}`.trim()}</Callout>
              </div>
            ) : null}

            <div className="flex flex-wrap gap-2">
              <Button
                type="submit"
                variant="primary"
                busy={save.isPending}
                busyLabel="Saving…"
                aria-disabled={clash || undefined}
              >
                Save the key
              </Button>
              {changing ? (
                <Button type="button" variant="ghost" onClick={() => setChanging(false)}>
                  Cancel
                </Button>
              ) : null}
            </div>
          </form>
        ) : null}
      </Skeleton>
    </SectionCard>
  );
}
