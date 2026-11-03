// Step 2, "Connect your code" (docs/design/screens/get-started/repository.jpg, ADR 0037): the repository's
// URL and a write-only token. Continue saves the token as a secret open to every factory (github-token),
// tests that it can clone and push, and shows the host's answer; a passing answer lets Continue go on, a
// failing one keeps it disabled until the URL or the token changes. The URL and the secret's name live in
// the URL (code-search.ts), so step 3 reads them; the token never does.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { type FormEvent, useState } from 'react';
import { ApiError } from '../../api/client';
import { keys } from '../../api/queries';
import { testRepository } from '../../api/repositories';
import { createSecret, replaceSecretValue } from '../../api/secrets';
import type { OrgSummary, RepositoryTestResult } from '../../api/types';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { cn } from '../../ui/cn';
import { Field, Input } from '../../ui/field';
import { Spinner } from '../../ui/spinner';
import { type OnboardingSearch, repoOf, repositoryUrl } from './code-search';
import { codeCard, organizationCard } from './factory-preview';
import { OnboardingLayout } from './onboarding-layout';

/** The secret the token is saved as. */
export const tokenSecret = 'github-token';

/** The check, in words: what Goal could do with the token, or the host's reason it could not. */
export function checkText(repo: string, result: RepositoryTestResult): { ok: boolean; text: string } {
  if (!result.tested)
    return { ok: false, text: 'Goal couldn’t test the token: the host gave no answer in 10 seconds.' };
  if (!result.read.ok) return { ok: false, text: `Goal couldn’t clone ${repo}. ${result.read.reason}` };
  if (!result.push.ok) return { ok: false, text: `Goal cloned ${repo} but can’t push a branch. ${result.push.reason}` };
  return { ok: true, text: `Goal cloned ${repo} and can push a branch.` };
}

/** `search` is the URL's choice (code-search.ts): the route passes it, so the step reads it the same way in a test. */
export function CodeStep({ org, search }: { org: OrgSummary; search: OnboardingSearch }) {
  const navigate = useNavigate();
  const client = useQueryClient();
  const [address, setAddress] = useState(search.url ?? '');
  const [token, setToken] = useState('');
  const [touched, setTouched] = useState(false);
  /** The inputs the last test answered for, and what it answered. */
  const [answer, setAnswer] = useState<{ key: string; ok: boolean; text: string } | undefined>();
  /** This browser's host, the start of the address the organization gets (as step 1 shows it). */
  const host = window.location.host;

  const url = repositoryUrl(address);
  const repo = repoOf(url);
  const secret = search.secret ?? tokenSecret;
  // A token typed now, or the secret an earlier visit saved: one of them is needed to test.
  const hasToken = token !== '' || search.secret !== undefined;
  const key = `${url ?? ''}\n${token}`;
  const passed = answer?.ok === true && answer.key === key;
  const failed = answer?.ok === false && answer.key === key;
  const urlError =
    touched && url === undefined
      ? 'Enter the repository’s https address, such as https://github.com/acme/app.'
      : undefined;

  const check = useMutation({
    mutationFn: async (): Promise<RepositoryTestResult> => {
      if (token !== '') {
        try {
          await createSecret(org.id, {
            name: secret,
            value: token,
            description: 'The repository token set up in onboarding',
            scope: { all_factories: true, factories: [] },
          });
        } catch (error) {
          // The name is taken by an earlier visit or another token: the typed one replaces its value.
          if (!(error instanceof ApiError) || error.code !== 'secret-name-taken') throw error;
          await replaceSecretValue(org.id, secret, token);
        }
        void client.invalidateQueries({ queryKey: keys.secrets(org.id) });
      }
      return testRepository(org.id, { url: url ?? '', secret });
    },
    onSuccess: (result) => {
      const found = checkText(repo ?? 'the repository', result);
      setAnswer({ key, ...found });
    },
  });

  const proceed = () =>
    void navigate({
      to: '/o/$org/get-started/$step',
      params: { org: org.slug, step: 'factory' },
      search: { url, secret },
    });

  const submit = (event: FormEvent) => {
    event.preventDefault();
    setTouched(true);
    if (passed) return proceed();
    if (url === undefined || !repo || !hasToken || failed || check.isPending) return;
    check.mutate();
  };

  const busy = check.isPending;
  return (
    <OnboardingLayout
      step="code"
      org={org}
      back={
        <Button asChild size="lg" variant="secondary">
          <Link to="/get-started">Back</Link>
        </Button>
      }
      action={
        <Button
          type="submit"
          form="repository-form"
          size="lg"
          variant="primary"
          disabled={failed || busy || (!passed && (url === undefined || !hasToken))}
        >
          {busy ? (
            <>
              <Spinner />
              Testing the token
            </>
          ) : (
            'Continue'
          )}
        </Button>
      }
      preview={{ organization: organizationCard(org, host), second: codeCard(repo, passed ? secret : undefined) }}
    >
      <div className="flex flex-col gap-2">
        <h1 className="m-0 text-kpi tracking-display">Connect your code</h1>
        <p className="m-0 text-heading font-normal text-ink-soft">
          Agents clone one repository, work on a branch per item and open pull requests. A token is all Goal needs.
        </p>
      </div>

      <form id="repository-form" onSubmit={submit} noValidate className="flex flex-col gap-6">
        <Field
          label="Repository"
          error={urlError}
          help="GitHub. GitLab and other git hosts over https work the same way."
        >
          <Input
            type="url"
            inputMode="url"
            value={address}
            onChange={(e) => setAddress(e.target.value)}
            onBlur={() => setTouched(true)}
            placeholder="https://github.com/acme/app"
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
          />
        </Field>

        <Field
          label="Token"
          help={
            <>
              A fine-grained token for this repository: Contents and Pull requests, read and write.{' '}
              <a href="https://github.com/settings/personal-access-tokens/new" target="_blank" rel="noreferrer">
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
              search.secret ? `Saved as ${search.secret}. Paste a new token to replace it.` : 'Paste the token'
            }
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
          />
        </Field>

        <div role="status" aria-live="polite" className={cn(!answer && !check.isError && 'hidden')}>
          {answer && (passed || failed) ? (
            <div className="flex animate-fade-in flex-col gap-1">
              <Callout tone={answer.ok ? 'ok' : 'rework'}>{answer.text}</Callout>
              {answer.ok ? (
                <p className="m-0 text-[13px] leading-snug text-muted">
                  Saved as the secret {secret}. Change it later in Secrets.
                </p>
              ) : null}
            </div>
          ) : null}
          {check.isError ? (
            <div className="animate-fade-in">
              <Callout tone="rework">{`Couldn’t save and test the token. ${check.error.message}`.trim()}</Callout>
            </div>
          ) : null}
        </div>

        <dl className="m-0 grid grid-cols-1 gap-4 text-secondary sm:grid-cols-3">
          <Access title="Reads" tone="text-ok-ink" text="Code, issues and checks" />
          <Access title="Writes" tone="text-run-ink" text="Branches and pull requests; anything else asks you first" />
          <Access title="Never" tone="text-rework-ink" text="Force pushes or the default branch" />
        </dl>
      </form>
    </OnboardingLayout>
  );
}

/** One of the three access notes: what the agents read, write, or never do. */
function Access({ title, tone, text }: { title: string; tone: string; text: string }) {
  return (
    <div className="flex flex-col gap-1">
      <dt className={cn('font-control', tone)}>{title}</dt>
      <dd className="m-0 text-ink-soft">{text}</dd>
    </div>
  );
}
