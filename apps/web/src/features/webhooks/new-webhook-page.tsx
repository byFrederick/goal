// New webhook (ADR 0040 decision 20, canvas docs/design/screens/organization/webhook-new.jpg): a sender, a
// name, a secret (generated and shown once, or an existing one by name) and the factories to open it to. A
// new webhook is open to no factory until someone opens it, so Open to here is a convenience: the webhook is
// created first, then opened, and a failure to open it leaves the webhook and says so. The generated secret
// goes to this page's state only, for the dialog; it never enters the query cache.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { type FormEvent, useRef, useState } from 'react';
import { toast } from 'sonner';
import { ApiError } from '../../api/client';
import { factoriesQuery, keys, secretsQuery } from '../../api/queries';
import { createWebhook, type NewWebhook, patchWebhook, type WebhookSender } from '../../api/webhooks';
import { TopBarSlot } from '../../app/top-bar';
import { factoryTitle } from '../../shared/factory';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Checkbox } from '../../ui/checkbox';
import { Field, Input } from '../../ui/field';
import { RadioCards } from '../../ui/radio-cards';
import { SegmentedControl } from '../../ui/segmented-control';
import { Select } from '../../ui/select';
import { parseRepositories } from './edit-webhook-dialog';
import { Card } from './parts';
import { SecretDialog, type ShownSecret } from './secret-dialog';
import { issuesOwnSecret, nameProblem, senderLabel, senders, setupSteps } from './words';

type SecretChoice = 'generate' | 'existing';

export function NewWebhookPage() {
  const org = useOrg();
  const navigate = useNavigate();
  const client = useQueryClient();
  const factories = useQuery(factoriesQuery(org.id)).data ?? [];
  const secrets = useQuery(secretsQuery(org.id)).data ?? [];
  const nameRef = useRef<HTMLInputElement>(null);

  const [sender, setSender] = useState<WebhookSender>('github');
  const [name, setName] = useState('');
  const [choice, setChoice] = useState<SecretChoice>('generate');
  const [secret, setSecret] = useState('');
  const [header, setHeader] = useState('');
  const [eventPath, setEventPath] = useState('');
  const [idPath, setIdPath] = useState('');
  const [opened, setOpened] = useState<Record<string, { open: boolean; repositories: string }>>({});
  const [attempted, setAttempted] = useState(false);
  const [shown, setShown] = useState<ShownSecret | null>(null);

  const go = (webhook: string) => navigate({ to: '/o/$org/webhooks/$webhook', params: { org: org.slug, webhook } });

  const create = useMutation({
    // The generated value goes to the page's state, never into the mutation's result, which the cache keeps.
    mutationFn: async (body: NewWebhook): Promise<void> => {
      const { secret_value, ...created } = await createWebhook(org.id, body);
      const open = factories
        .filter((f) => opened[f.id]?.open)
        .map((f) => {
          const repositories = parseRepositories(opened[f.id]?.repositories ?? '');
          return { factory: f.id, repositories: repositories.length === 0 ? null : repositories };
        });
      let openFailed = false;
      if (open.length > 0) {
        try {
          await patchWebhook(org.id, created.name, { open });
        } catch {
          openFailed = true;
        }
      }
      await client.invalidateQueries({ queryKey: keys.webhooks(org.id) });
      if (openFailed)
        toast.error(`${created.name} is created, but opening it failed`, { description: 'Open it from its page.' });
      if (secret_value) setShown({ webhook: created.name, sender: created.sender, secret: secret_value });
      else await go(created.name);
    },
    gcTime: 0,
  });

  const problem = attempted ? nameProblem(name) : undefined;
  const err = create.error instanceof ApiError ? create.error : undefined;
  const nameError =
    err?.code === 'webhook-name-taken'
      ? 'Another webhook of the organization has this name.'
      : err?.code === 'secret-name-taken'
        ? `A secret named webhook-${name} exists. Pick another name, or use that secret.`
        : err?.code === 'webhook-invalid' && !err.detail?.includes('token')
          ? (err.detail ?? 'This name or sender is not accepted.')
          : problem;
  const secretError =
    err?.code === 'secret-not-found'
      ? 'This organization has no such secret.'
      : attempted && choice === 'existing' && secret === ''
        ? 'Pick the secret that signs it.'
        : undefined;
  const failed = create.isError && !nameError && !secretError;

  const submit = (event: FormEvent) => {
    event.preventDefault();
    setAttempted(true);
    if (nameProblem(name)) return nameRef.current?.focus();
    if (choice === 'existing' && secret === '') return;
    if (create.isPending) return;
    create.mutate({
      name,
      sender,
      ...(choice === 'existing' ? { secret } : {}),
      ...(sender === 'token'
        ? { token: { header: header.trim(), event_path: eventPath.trim(), id_path: idPath.trim() } }
        : {}),
    });
  };

  const pick = (value: WebhookSender) => {
    create.reset();
    setSender(value);
    // PagerDuty and Sentry issue their own secret, so the one to use must exist first.
    setChoice(issuesOwnSecret.includes(value) ? 'existing' : 'generate');
  };

  const steps = setupSteps(sender);
  const generated = choice === 'generate';

  return (
    <>
      <TopBarSlot crumb="New webhook" />
      <div className="flex min-w-0 flex-col gap-5">
        <h1 className="m-0 text-page-title tracking-display">New webhook</h1>
        <form
          onSubmit={submit}
          noValidate
          className="grid min-w-0 items-start gap-5 lg:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)]"
        >
          <div className="flex min-w-0 flex-col gap-5">
            <Card
              title="Sender"
              tip={{
                about: 'senders',
                text: 'How the sender signs its deliveries. Each is an entry in Goal’s catalog of senders.',
              }}
            >
              <div className="p-4">
                <RadioCards<WebhookSender>
                  legend="Sender"
                  showLegend={false}
                  name="sender"
                  value={sender}
                  onChange={pick}
                  gridClassName="grid-cols-1 sm:grid-cols-2"
                  options={senders.map((s) => ({ value: s.value, label: s.label, description: s.blurb }))}
                />
                {sender === 'token' ? (
                  <div className="mt-4 grid gap-3 sm:grid-cols-3">
                    <Field label="Header" help="Empty: Authorization, as Bearer <value>.">
                      <Input
                        value={header}
                        onChange={(e) => setHeader(e.target.value)}
                        maxLength={100}
                        className="font-mono"
                        placeholder="X-Alert-Token"
                      />
                    </Field>
                    <Field label="Event path" help="Where the event name sits in the body.">
                      <Input
                        value={eventPath}
                        onChange={(e) => setEventPath(e.target.value)}
                        maxLength={200}
                        className="font-mono"
                        placeholder="event.type"
                      />
                    </Field>
                    <Field label="Delivery id path" help="Where a delivery id sits in the body.">
                      <Input
                        value={idPath}
                        onChange={(e) => setIdPath(e.target.value)}
                        maxLength={200}
                        className="font-mono"
                        placeholder="id"
                      />
                    </Field>
                  </div>
                ) : null}
              </div>
            </Card>

            <Card title="Name">
              <div className="p-4">
                <Field
                  label="Name"
                  error={nameError}
                  help={
                    <>
                      Triggers name it: <span className="font-mono">{name || 'name'}.check_run</span>
                    </>
                  }
                >
                  <Input
                    ref={nameRef}
                    value={name}
                    onChange={(e) => {
                      create.reset();
                      setName(e.target.value.toLowerCase());
                    }}
                    onBlur={() => setAttempted((a) => a || name !== '')}
                    autoComplete="off"
                    spellCheck={false}
                    className="font-mono"
                    placeholder={`${sender}-acme`}
                  />
                </Field>
              </div>
            </Card>

            <Card title="Secret">
              <div className="flex flex-col gap-3 p-4">
                <SegmentedControl<SecretChoice>
                  label="Secret"
                  value={choice}
                  onChange={(value) => {
                    create.reset();
                    setChoice(value);
                  }}
                  options={[
                    { value: 'generate', label: 'Generate one' },
                    { value: 'existing', label: 'Use a secret' },
                  ]}
                />
                {generated ? (
                  <p className="m-0 text-secondary text-muted">
                    Saved as <span className="font-mono">webhook-{name || 'name'}</span> and shown once, for{' '}
                    {senderLabel(sender)}.
                  </p>
                ) : (
                  <Field
                    label="Secret"
                    error={secretError}
                    help={
                      issuesOwnSecret.includes(sender)
                        ? `${senderLabel(sender)} gives you the secret: save it in Secrets first.`
                        : undefined
                    }
                  >
                    <Select
                      value={secret}
                      onValueChange={(value) => {
                        create.reset();
                        setSecret(value);
                      }}
                      placeholder="Pick a secret"
                      options={secrets.map((s) => ({ value: s.name, label: s.name }))}
                    />
                  </Field>
                )}
              </div>
            </Card>

            <Card
              title="Open to"
              tip={{
                about: 'openings',
                text: 'A new webhook is open to no factory. It carries every repository’s events, so limit an opening to the repositories a team owns.',
              }}
            >
              <div className="flex flex-col gap-3 p-4">
                {factories.length === 0 ? <p className="m-0 text-secondary text-muted">No factory yet.</p> : null}
                {factories.map((f) => {
                  const row = opened[f.id] ?? { open: false, repositories: '' };
                  return (
                    <div
                      key={f.id}
                      className="grid grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)] items-center gap-3 max-sm:grid-cols-1"
                    >
                      <Checkbox
                        label={factoryTitle(f)}
                        checked={row.open}
                        onCheckedChange={(open) => setOpened((o) => ({ ...o, [f.id]: { ...row, open } }))}
                      />
                      {row.open ? (
                        <Input
                          aria-label={`Repositories for ${factoryTitle(f)}`}
                          placeholder="All repositories"
                          value={row.repositories}
                          autoComplete="off"
                          spellCheck={false}
                          className="font-mono"
                          onChange={(e) =>
                            setOpened((o) => ({ ...o, [f.id]: { ...row, repositories: e.target.value } }))
                          }
                        />
                      ) : null}
                    </div>
                  );
                })}
              </div>
            </Card>

            {failed ? (
              <div role="alert">
                <Callout tone="rework">
                  {err?.code === 'secrets-unavailable'
                    ? 'Secrets are not available until goald has a master key. Nothing was created.'
                    : 'Not created. Check your connection and try again.'}
                </Callout>
              </div>
            ) : null}
            <div className="flex flex-wrap justify-end gap-2">
              <Button asChild variant="secondary">
                <Link to="/o/$org/webhooks" params={{ org: org.slug }}>
                  Cancel
                </Link>
              </Button>
              <Button type="submit" variant="primary" disabled={create.isPending}>
                {create.isPending ? 'Creating…' : 'Create webhook'}
              </Button>
            </div>
          </div>

          <Card title={`Then, in ${steps.where}`} className="min-w-0">
            <ol className="m-0 flex list-decimal flex-col gap-2 py-3 ps-9 pe-4 text-secondary text-ink-soft marker:text-muted">
              <li>The address and the secret to paste.</li>
              <li>The events your triggers use.</li>
              {sender === 'github' ? <li>Goal shows GitHub’s ping when it arrives.</li> : null}
            </ol>
          </Card>
        </form>
      </div>
      <SecretDialog
        shown={shown}
        onDone={() => {
          const webhook = shown?.webhook;
          setShown(null);
          if (webhook) void go(webhook);
        }}
      />
    </>
  );
}
