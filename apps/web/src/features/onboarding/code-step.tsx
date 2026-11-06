// Step 3, "Connect your code" (docs/design/screens/get-started/repository.jpg, ADR 0041 decision 9). It comes
// after the factory, so it knows the template's `code` (none, read or write) and can open the token to this
// one factory. One form for every case: No code asks for nothing; Read only and Read and write ask which
// repositories (a whole organization, or only some) and a token. Continue saves the token as a secret open
// to the factory, tests every URL on the factory's route, and a passing answer lets the next Continue write
// the `runtime.repositories` entries into the draft's factory.yaml. A name that is taken never has its value
// replaced: the step offers the existing secret or a new name.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { Plus, X } from 'lucide-react';
import { type FormEvent, useState } from 'react';
import { ApiError } from '../../api/client';
import { getDraftFile, openDraft, putDraftFile } from '../../api/definitions';
import { defKeys, keys, templatesQuery } from '../../api/queries';
import { testFactoryRepository } from '../../api/repositories';
import { createSecret, getSecret, patchSecret } from '../../api/secrets';
import type { FactoryRepositoryTest, OrgSummary, TemplateCode } from '../../api/types';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { cn } from '../../ui/cn';
import { Field, Input } from '../../ui/field';
import { InfoTip } from '../../ui/info-tip';
import { RadioCards, type RadioOption } from '../../ui/radio-cards';
import { SegmentedControl } from '../../ui/segmented-control';
import { Skeleton } from '../../ui/skeleton';
import { Spinner } from '../../ui/spinner';
import { touchLink } from '../../ui/touch-target';
import { nameProblem } from '../secrets/words';
import {
  type CodeAccess,
  ownerUrl,
  repositoryEntries,
  repositoryLabel,
  suggestedSecret,
  withRepositories,
} from './code-repositories';
import { type OnboardingSearch, repositoryUrl } from './code-search';
import { codeCard, organizationCard } from './factory-preview';
import { OnboardingLayout } from './onboarding-layout';

type Scope = 'organization' | 'some';

/** What the template's line needs, as the access it starts on. */
const startAccess: Record<TemplateCode, CodeAccess> = { none: 'none', read: 'read', write: 'write' };

/** The reason a choice is disabled for a template, or undefined when it can be chosen. */
export function disabledReason(code: TemplateCode, choice: CodeAccess): string | undefined {
  if (code === 'write' && choice !== 'write') return 'This line merges pull requests, so it needs Read and write.';
  if (code === 'read' && choice === 'none') return 'This line reads code, so it needs at least Read only.';
  return undefined;
}

/** One answer, in words: what Goal could do with the token on this URL, or why it could not. */
export function resultText(label: string, r: FactoryRepositoryTest): string {
  if (r.ok) return `${label}: ${r.summary}`;
  const reason = !r.tested ? '' : !r.read.ok ? r.read.reason : r.push.reason;
  return `${label}: ${r.summary}${reason && !r.summary.includes(reason) ? ` ${reason}` : ''}`.trim();
}

/** Makes the secret open to the factory, keeping its value and every other factory it was open to. */
async function openSecretTo(org: string, name: string, factory: string): Promise<void> {
  const secret = await getSecret(org, name);
  if (secret.scope.all_factories || secret.scope.factories.includes(factory)) return;
  await patchSecret(org, name, { scope: { all_factories: false, factories: [...secret.scope.factories, factory] } });
}

/** `search` is the URL's choice (code-search.ts): the route passes it, so the step reads it the same way in a test. */
export function CodeStep({ org, search }: { org: OrgSummary; search: OnboardingSearch }) {
  const templates = useQuery(templatesQuery(org.id));
  const template = templates.data?.find((t) => t.name === search.template);
  const code: TemplateCode = template?.code ?? 'none';
  // The page's frame stays real while the template loads: only the form's blocks are the placeholder, so
  // nothing focusable sits inside the hidden layer.
  if (templates.isPending) {
    return (
      <OnboardingLayout step="code" org={org} preview={{ organization: organizationCard(org, window.location.host) }}>
        <Skeleton ready={false} placeholder={<CodePlaceholder />}>
          {null}
        </Skeleton>
      </OnboardingLayout>
    );
  }
  return search.factory ? (
    <CodeForm key={code} org={org} search={search} factory={search.factory} code={code} />
  ) : (
    <NoFactory org={org} search={search} />
  );
}

/** The form's blocks as muted shapes, while the template is read. */
function CodePlaceholder() {
  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-2">
        <div className="h-8 w-2/3 rounded-control bg-hover" />
        <div className="h-5 w-3/5 rounded-control bg-hover" />
      </div>
      <div className="grid gap-3 @[480px]:grid-cols-3">
        <div className="h-28 rounded-card bg-hover" />
        <div className="h-28 rounded-card bg-hover" />
        <div className="h-28 rounded-card bg-hover" />
      </div>
      <div className="h-11 rounded-control bg-hover" />
      <div className="h-11 rounded-control bg-hover" />
    </div>
  );
}

/** The Code step without a factory (the address was opened by hand): a link back to the step that makes one. */
function NoFactory({ org, search }: { org: OrgSummary; search: OnboardingSearch }) {
  return (
    <OnboardingLayout
      step="code"
      org={org}
      back={
        <Button asChild size="lg" variant="secondary">
          <Link to="/o/$org/get-started/$step" params={{ org: org.slug, step: 'factory' }} search={{ ...search }}>
            Back
          </Link>
        </Button>
      }
    >
      <div className="flex flex-col gap-2">
        <h1 className="m-0 text-kpi tracking-display">Connect your code</h1>
        <p className="m-0 text-heading font-normal text-ink-soft">Choose a template first, then connect its code.</p>
      </div>
    </OnboardingLayout>
  );
}

interface Answer {
  /** The inputs the test answered for. */
  key: string;
  secret: string;
  urls: string[];
  results: FactoryRepositoryTest[];
}

function CodeForm({
  org,
  search,
  factory,
  code,
}: {
  org: OrgSummary;
  search: OnboardingSearch;
  factory: string;
  code: TemplateCode;
}) {
  const navigate = useNavigate();
  const client = useQueryClient();
  const [access, setAccess] = useState<CodeAccess>(startAccess[code]);
  const [scope, setScope] = useState<Scope>('some');
  const [owner, setOwner] = useState('');
  const [addresses, setAddresses] = useState<string[]>([search.url && !search.url.endsWith('/*') ? search.url : '']);
  const [token, setToken] = useState('');
  const [touched, setTouched] = useState(false);
  const [secretName, setSecretName] = useState<string>();
  const [clash, setClash] = useState(false);
  const [newName, setNewName] = useState('');
  /** The secret and token the last save made, so a retest with the same token does not create it twice. */
  const [saved, setSaved] = useState<{ name: string; token: string }>();
  const [answer, setAnswer] = useState<Answer>();
  const host = window.location.host;

  const wantsCode = access !== 'none';
  const ownerPatternUrl = ownerUrl(owner);
  const urls =
    scope === 'organization' ? (ownerPatternUrl ? [ownerPatternUrl] : []) : addresses.map((a) => repositoryUrl(a));
  const validUrls = urls.filter((u): u is string => u !== undefined);
  const urlsValid = scope === 'organization' ? ownerPatternUrl !== undefined : validUrls.length === addresses.length;
  const name = secretName ?? search.secret ?? suggestedSecret(validUrls[0]);
  const hasToken = token !== '' || search.secret !== undefined || saved !== undefined;
  const key = `${access}\n${validUrls.join('\n')}\n${token}\n${name}`;
  const passed = answer?.key === key && answer.results.every((r) => r.ok);
  const failed = answer?.key === key && !passed;

  const run = useMutation({
    mutationFn: async (mode: 'create' | 'existing'): Promise<Answer> => {
      const using = name;
      const usable = saved?.name === using && saved.token === token;
      if (mode === 'existing' || token === '' || usable) {
        await openSecretTo(org.id, using, factory);
      } else {
        await createSecret(org.id, {
          name: using,
          value: token,
          description: 'The code token set up in onboarding',
          scope: { all_factories: false, factories: [factory] },
        });
        setSaved({ name: using, token });
      }
      void client.invalidateQueries({ queryKey: keys.secrets(org.id) });
      const results = await Promise.all(
        validUrls.map((url) =>
          testFactoryRepository(org.id, factory, { url, secret: using, access: access === 'write' ? 'write' : 'read' }),
        ),
      );
      return { key, secret: using, urls: validUrls, results };
    },
    onSuccess: (result) => {
      setClash(false);
      setAnswer(result);
    },
    onError: (error) => {
      if (error instanceof ApiError && error.code === 'secret-name-taken') {
        setNewName('');
        setClash(true);
      }
    },
  });

  const proceed = (entry: { url: string | undefined; secret: string | undefined }) =>
    void navigate({
      to: '/o/$org/get-started/$step',
      params: { org: org.slug, step: 'line' },
      search: { ...entry, template: search.template, factory },
    });

  const write = useMutation({
    mutationFn: async () => {
      await openDraft(org.id, factory);
      const file = await getDraftFile(org.id, factory, 'factory.yaml');
      const entries = answer
        ? repositoryEntries(answer.urls, answer.secret, access === 'write' ? 'write' : 'read')
        : [];
      const text = withRepositories(file.text, entries);
      if (text !== file.text) await putDraftFile(org.id, factory, 'factory.yaml', text, { sha256: file.sha256 });
    },
    onSuccess: async () => {
      // The file was written directly: the draft the next steps read, and its revision, are stale now.
      await client.invalidateQueries({ queryKey: defKeys.draft(org.id, factory) });
      proceed({ url: answer?.urls[0], secret: answer?.secret });
    },
  });

  const submit = (event: FormEvent) => {
    event.preventDefault();
    setTouched(true);
    if (run.isPending || write.isPending) return;
    if (!wantsCode) {
      // Earlier visits may have written entries: No code takes them out again.
      if (search.url) write.mutate();
      else proceed({ url: undefined, secret: undefined });
      return;
    }
    if (passed) return write.mutate();
    if (!urlsValid || validUrls.length === 0 || !hasToken || failed || clash) return;
    run.mutate('create');
  };

  const chooseExisting = () => run.mutate('existing');
  const chooseNewName = () => {
    const problem = nameProblem(newName);
    if (problem) return;
    setSecretName(newName);
    setClash(false);
    run.mutate('create');
  };

  const busy = run.isPending || write.isPending;
  const urlError = (address: string) =>
    touched && repositoryUrl(address) === undefined
      ? 'Enter the repository’s https address, such as https://github.com/acme/app.'
      : undefined;
  const ownerError =
    touched && ownerPatternUrl === undefined
      ? 'Enter the organization’s https address, such as https://github.com/acme.'
      : undefined;

  const choices: RadioOption<CodeAccess>[] = [
    { value: 'none', label: 'No code', description: 'Agents work without a repository' },
    { value: 'read', label: 'Read only', description: 'Agents read code and push nothing' },
    { value: 'write', label: 'Read and write', description: 'Branches and pull requests' },
  ].map((o) => {
    const reason = disabledReason(code, o.value as CodeAccess);
    return {
      ...o,
      value: o.value as CodeAccess,
      disabled: reason !== undefined,
      badge: reason ? (
        <InfoTip about={`${o.label} being unavailable`} className="max-md:size-11">
          {reason}
        </InfoTip>
      ) : undefined,
    };
  });

  const preview = codeCard(
    wantsCode && validUrls[0] ? repositoryLabel(validUrls[0]) : undefined,
    passed ? answer?.secret : undefined,
  );

  return (
    <OnboardingLayout
      step="code"
      org={org}
      back={
        <Button asChild size="lg" variant="secondary">
          <Link
            to="/o/$org/get-started/$step"
            params={{ org: org.slug, step: 'factory' }}
            search={{ factory, template: search.template, url: search.url, secret: search.secret }}
          >
            Back
          </Link>
        </Button>
      }
      action={
        <Button
          type="submit"
          form="code-form"
          size="lg"
          variant="primary"
          disabled={
            wantsCode && (failed || clash || busy || (!passed && (!urlsValid || validUrls.length === 0 || !hasToken)))
          }
        >
          {run.isPending ? (
            <>
              <Spinner />
              Testing the token
            </>
          ) : (
            'Continue'
          )}
        </Button>
      }
      preview={{ organization: organizationCard(org, host), second: preview }}
    >
      <div className="flex flex-col gap-2">
        <h1 className="m-0 text-kpi tracking-display">Connect your code</h1>
        <p className="m-0 text-heading font-normal text-ink-soft">
          How much of your code agents may touch. A token is all Goal needs.
        </p>
      </div>

      <form id="code-form" onSubmit={submit} noValidate className="flex flex-col gap-6">
        <RadioCards
          legend="Code"
          name="code-access"
          value={access}
          onChange={(v) => {
            setAccess(v);
            setClash(false);
          }}
          options={choices}
          gridClassName="@[480px]:grid-cols-3"
        />

        {wantsCode ? (
          <div className="flex animate-fade-in flex-col gap-6">
            <div className="flex flex-col gap-2">
              <span className="flex items-center gap-1 text-sm font-medium text-ink">
                Repositories
                <InfoTip about="repositories" className="max-md:size-11">
                  A whole organization covers every repository in it, including new ones. Each item clones the one it’s
                  about. Read and write access works either way.
                </InfoTip>
              </span>
              <SegmentedControl
                label="Repositories"
                value={scope}
                onChange={setScope}
                options={[
                  { value: 'organization', label: 'A whole organization' },
                  { value: 'some', label: 'Only some' },
                ]}
              />
            </div>

            {scope === 'organization' ? (
              <Field label="Organization" error={ownerError} help="GitHub or GitLab, in the cloud or on your own host.">
                <Input
                  type="url"
                  inputMode="url"
                  value={owner}
                  onChange={(e) => setOwner(e.target.value)}
                  onBlur={() => setTouched(true)}
                  placeholder="https://github.com/acme"
                  autoComplete="off"
                  autoCapitalize="off"
                  spellCheck={false}
                />
              </Field>
            ) : (
              <div className="flex flex-col gap-3">
                {addresses.map((address, i) => (
                  // The list is edited in place and rows have no id: the position is the row's identity.
                  // biome-ignore lint/suspicious/noArrayIndexKey: rows are positional
                  <div key={i} className="animate-fade-in">
                    <Field
                      label={i === 0 ? 'Repository' : `Repository ${i + 1}`}
                      error={urlError(address)}
                      help={i === 0 ? 'GitHub or GitLab, in the cloud or on your own host.' : undefined}
                      aside={
                        addresses.length > 1 ? (
                          <Button
                            type="button"
                            size="sm"
                            variant="ghost"
                            aria-label={`Remove repository ${i + 1}`}
                            onClick={() => setAddresses(addresses.filter((_, n) => n !== i))}
                          >
                            <X size={14} aria-hidden="true" />
                            Remove
                          </Button>
                        ) : undefined
                      }
                    >
                      <Input
                        type="url"
                        inputMode="url"
                        value={address}
                        onChange={(e) => setAddresses(addresses.map((a, n) => (n === i ? e.target.value : a)))}
                        onBlur={() => setTouched(true)}
                        placeholder="https://github.com/acme/app"
                        autoComplete="off"
                        autoCapitalize="off"
                        spellCheck={false}
                      />
                    </Field>
                  </div>
                ))}
                <div>
                  <Button type="button" size="sm" variant="ghost" onClick={() => setAddresses([...addresses, ''])}>
                    <Plus size={14} aria-hidden="true" />
                    Add another repository
                  </Button>
                </div>
              </div>
            )}

            <Field
              label="Token"
              help={
                <>
                  {access === 'write'
                    ? 'A fine-grained token for these repositories: Contents and Pull requests, read and write. '
                    : 'A fine-grained token for these repositories: Contents, read only. '}
                  <a
                    href="https://github.com/settings/personal-access-tokens/new"
                    target="_blank"
                    rel="noreferrer"
                    className={touchLink}
                  >
                    Create one on GitHub
                  </a>
                </>
              }
            >
              <Input
                type="password"
                value={token}
                onChange={(e) => setToken(e.target.value)}
                placeholder={
                  search.secret
                    ? `Saved as ${search.secret}. Paste a token to save it under a new name.`
                    : 'Paste the token'
                }
                autoComplete="off"
                autoCapitalize="off"
                spellCheck={false}
              />
            </Field>

            {clash ? (
              <div role="alert" className="flex animate-fade-in flex-col gap-3">
                <Callout tone="rework">
                  {`A secret named ${name} already exists. Goal never replaces a secret’s value. Use it, or pick a new name for this token.`}
                </Callout>
                <div className="flex flex-wrap items-end gap-3">
                  <Button type="button" variant="secondary" onClick={chooseExisting} disabled={busy}>
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
                    onClick={chooseNewName}
                    disabled={busy || nameProblem(newName) !== undefined || token === ''}
                  >
                    Use this name
                  </Button>
                </div>
              </div>
            ) : null}

            <div
              role="status"
              aria-live="polite"
              className={cn('flex flex-col gap-2', !answer && !run.isError && !write.isError && 'hidden')}
            >
              {answer && (passed || failed)
                ? answer.results.map((r, i) => {
                    const label = repositoryLabel(answer.urls[i] ?? r.url);
                    return (
                      <div key={r.url} className="flex animate-fade-in flex-col gap-1">
                        <Callout tone={r.ok ? 'ok' : 'rework'}>{resultText(label, r)}</Callout>
                        {r.warning ? <Callout tone="note">{r.warning}</Callout> : null}
                        {r.note ? <Callout tone="note">{r.note}</Callout> : null}
                      </div>
                    );
                  })
                : null}
              {answer && passed ? (
                <p className="m-0 animate-fade-in text-meta text-muted">
                  {`Saved as the secret ${answer.secret}, open to this factory. Change it later in Secrets.`}
                </p>
              ) : null}
              {run.isError && !clash ? (
                <div className="animate-fade-in">
                  <Callout tone="rework">{`Couldn’t save and test the token. ${run.error.message}`.trim()}</Callout>
                </div>
              ) : null}
              {write.isError ? (
                <div className="animate-fade-in">
                  <Callout tone="rework">
                    {`The repositories weren’t saved in the factory. ${write.error.message}`.trim()}
                  </Callout>
                </div>
              ) : null}
            </div>
          </div>
        ) : null}
      </form>
    </OnboardingLayout>
  );
}
