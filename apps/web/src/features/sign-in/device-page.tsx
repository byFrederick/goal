// Device-Approve (canvas "Device-Approve", "Mobile-Device-Approve"; spec 3 §10.2): the person who
// ran goal login types the code their terminal shows, sees who asks (the client goald checked, the
// address and tool it asked from, whether that is this browser's network, when) and approves or
// denies. The code comes only from what is typed, never from the page's URL, so a link someone
// sends cannot approve anything; approving needs a sign-in from the last 10 minutes, which the
// re-auth dialog asks for.
import { useMutation, useSuspenseQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Check, Terminal, TriangleAlert } from 'lucide-react';
import { type FormEvent, type Ref, useEffect, useRef, useState } from 'react';
import { AuthFailure, authMutations, claimDeviceCode } from '../../api/auth';
import { ApiError, getDeviceRequest } from '../../api/client';
import { meQuery } from '../../api/queries';
import type { DeviceRequest, Me } from '../../api/types';
import { displayName } from '../../shared/names';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { Field, Input } from '../../ui/field';
import { Spinner } from '../../ui/spinner';
import { AuthHeading, AuthPage, LiveMessage } from './auth-page';
import { formatUserCode, isWholeCode, requestTimes } from './device-code';

const invalidCode = 'That code isn’t valid any more. Run goal login again.';
const unreachable = 'Can’t reach Goal. Check your connection and try again.';

/** Why a step failed, in the board's words. */
function failureText(err: unknown): string {
  if (err instanceof ApiError) return err.status === 404 ? invalidCode : unreachable;
  if (err instanceof AuthFailure) {
    if (err.kind === 'refused') return invalidCode;
    if (err.kind === 'reauth-required') return 'Approving needs a recent sign-in. Approve again to confirm it’s you.';
    if (err.kind === 'rate-limited') return 'Too many tries. Wait a minute, then try again.';
  }
  return unreachable;
}

type Step =
  /** invalid: the code the person entered stopped being valid while they looked at it. */
  | { name: 'code'; invalid?: boolean }
  | { name: 'review'; code: string; request: DeviceRequest }
  | { name: 'approved' }
  | { name: 'denied' };

/** The page for a signed-in person: the code, the request, the outcome. */
export function DevicePage() {
  const { data: me } = useSuspenseQuery(meQuery);
  const [step, setStep] = useState<Step>({ name: 'code' });
  const [steps, setSteps] = useState(0);
  const heading = useRef<HTMLHeadingElement>(null);

  // Each step replaces the last in place: its heading takes the focus, so screen readers start
  // there. Not on the first render, where the code field has it.
  useEffect(() => {
    if (steps > 0) heading.current?.focus();
  }, [steps]);

  const go = (next: Step) => {
    setStep(next);
    setSteps((n) => n + 1);
  };

  switch (step.name) {
    case 'code':
      return (
        <CodeStep
          heading={heading}
          invalid={step.invalid ?? false}
          onFound={(code, request) => go({ name: 'review', code, request })}
        />
      );
    case 'review':
      return (
        <ReviewStep
          heading={heading}
          me={me}
          code={step.code}
          request={step.request}
          onApproved={() => go({ name: 'approved' })}
          onDenied={() => go({ name: 'denied' })}
          onInvalid={() => go({ name: 'code', invalid: true })}
        />
      );
    case 'approved':
      return (
        <AuthPage>
          <Callout tone="ok">Approved for {me.user ? displayName(me.user) : 'you'}</Callout>
          <AuthHeading ref={heading}>The CLI is signed in</AuthHeading>
          <p className="m-0 text-body text-ink-soft">You can close this tab.</p>
        </AuthPage>
      );
    case 'denied':
      return (
        <AuthPage>
          <AuthHeading ref={heading}>Denied</AuthHeading>
          <p className="m-0 text-body text-ink-soft">The CLI was not signed in.</p>
        </AuthPage>
      );
  }
}

interface CodeStepProps {
  heading: Ref<HTMLHeadingElement>;
  /** Open with the "not valid any more" message (the code stopped working on step 2). */
  invalid: boolean;
  onFound: (code: string, request: DeviceRequest) => void;
}

/** Step 1: the code from the terminal, typed or pasted. */
function CodeStep({ heading, invalid, onFound }: CodeStepProps) {
  const [code, setCode] = useState('');
  const [incomplete, setIncomplete] = useState(false);

  // goald describes the pending request; the identity service then binds the code to this person,
  // so only they can approve or deny it.
  const find = useMutation({
    mutationKey: ['auth', 'device', 'find'],
    mutationFn: async (typed: string) => {
      const request = await getDeviceRequest(typed);
      if (!(await claimDeviceCode(typed))) throw new AuthFailure('refused');
      return request;
    },
    onSuccess: (request, typed) => onFound(typed, request),
  });

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (!isWholeCode(code)) {
      setIncomplete(true);
      return;
    }
    setIncomplete(false);
    if (!find.isPending) find.mutate(code);
  };

  const error = incomplete ? 'Enter the 8-character code shown in your terminal, like ABCD-EFGH.' : undefined;
  const failed = find.isError ? failureText(find.error) : find.isIdle && invalid ? invalidCode : undefined;

  return (
    <AuthPage>
      <div className="flex flex-col gap-2">
        <AuthHeading ref={heading}>Sign in to the Goal CLI</AuthHeading>
        <p className="m-0 text-body text-ink-soft">Enter the code shown in your terminal.</p>
      </div>
      <form onSubmit={onSubmit} noValidate className="flex flex-col gap-3">
        <Field label="Code" error={error}>
          <Input
            name="user_code"
            value={code}
            onChange={(e) => setCode(formatUserCode(e.target.value))}
            placeholder="ABCD-EFGH"
            autoComplete="off"
            autoCapitalize="characters"
            autoCorrect="off"
            spellCheck={false}
            maxLength={9}
            // A failed lookup is described by its message below; a code too short, by the field's.
            {...(failed ? { 'aria-invalid': true, 'aria-describedby': 'device-code-message' } : {})}
            // The page is for the code: it takes focus as the page opens.
            autoFocus
            className="h-14 text-center font-mono text-kpi tracking-[0.12em]"
          />
        </Field>
        <LiveMessage>
          {failed ? (
            <Callout tone="rework" id="device-code-message">
              {failed}
            </Callout>
          ) : null}
        </LiveMessage>
        <Button
          type="submit"
          variant="primary"
          size="lg"
          aria-busy={find.isPending || undefined}
          className="w-full justify-center aria-busy:cursor-progress"
        >
          {find.isPending ? (
            <>
              <Spinner />
              Checking…
              <span className="sr-only">Loading</span>
            </>
          ) : (
            'Continue'
          )}
        </Button>
      </form>
    </AuthPage>
  );
}

interface ReviewStepProps {
  heading: Ref<HTMLHeadingElement>;
  me: Me;
  code: string;
  request: DeviceRequest;
  onApproved: () => void;
  onDenied: () => void;
  onInvalid: () => void;
}

/** The code as the terminal shows it: letters in boxes, split by a dash (consent.html's right card). */
function CodeBoxes({ code }: { code: string }) {
  const [first, second] = code.split('-');
  const box =
    'flex h-14 w-9 items-center justify-center rounded-control border border-line bg-sidebar text-kpi text-ink sm:w-10';
  // The cells are a fixed row of characters, so the position is their identity.
  const cells = (chars: string) =>
    [...chars].map((letter, i) => (
      // biome-ignore lint/suspicious/noArrayIndexKey: a fixed row of characters
      <span key={i} className={box} aria-hidden="true">
        {letter}
      </span>
    ));
  return (
    <div className="flex items-center gap-1 sm:gap-2">
      {cells(first ?? '')}
      <span aria-hidden="true" className="text-kpi text-muted">
        –
      </span>
      {cells(second ?? '')}
      <span className="sr-only">{code}</span>
    </div>
  );
}

/** Step 2: what is asked, from where and when; approve or deny. */
function ReviewStep({ heading, me, code, request, onApproved, onDenied, onInvalid }: ReviewStepProps) {
  const [now] = useState(() => Date.now());
  const onError = (err: unknown) => {
    // The code was used, expired or denied meanwhile: back to typing one, which says so.
    if (err instanceof AuthFailure && err.kind === 'refused') onInvalid();
  };
  const approve = useMutation({ ...authMutations.approveDevice, onSuccess: onApproved, onError });
  const deny = useMutation({ ...authMutations.denyDevice, onSuccess: onDenied, onError });
  const busy = approve.isPending || deny.isPending;
  const failed = approve.error ?? deny.error;
  const who = me.user ? displayName(me.user) : 'you';

  return (
    <AuthPage wide>
      <span className="inline-grid size-12 flex-none place-items-center rounded-cell bg-andon text-white">
        <Terminal size={20} strokeWidth={1.8} aria-hidden="true" />
      </span>
      <div className="flex flex-col gap-2">
        <AuthHeading ref={heading}>Sign in the goal CLI?</AuthHeading>
        <p className="m-0 text-body text-ink-soft">Check that this code matches the one in your terminal.</p>
      </div>
      <CodeBoxes code={code} />
      <dl className="m-0 grid grid-cols-1 gap-x-4 gap-y-1 rounded-control bg-panel px-4 py-3 text-secondary leading-snug sm:grid-cols-[120px_minmax(0,1fr)] sm:gap-y-3">
        <dt className="text-muted">Device</dt>
        <dd className="m-0 mb-2 min-w-0 break-words text-ink sm:mb-0">
          {[request.client_name, request.user_agent].filter(Boolean).join(', ')}
        </dd>
        <dt className="text-muted">Asked</dt>
        <dd className="m-0 mb-2 min-w-0 text-ink sm:mb-0">
          {requestTimes(request.requested_at, request.expires_at, now)}
        </dd>
        <dt className="text-muted">Gets</dt>
        <dd className="m-0 min-w-0 text-ink">Full access as {who}, in Goal</dd>
        {request.ip ? (
          <>
            <dt className="text-muted">Requested from</dt>
            <dd className="m-0 mt-2 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-2 sm:mt-0">
              <span className="font-mono text-ink">{request.ip}</span>
              {request.same_ip ? (
                <Chip tone="ok">
                  <Check size={14} strokeWidth={1.8} aria-hidden="true" />
                  Same network as this browser
                </Chip>
              ) : (
                <Chip tone="rework">
                  <TriangleAlert size={14} strokeWidth={1.6} aria-hidden="true" />A different network
                </Chip>
              )}
            </dd>
          </>
        ) : null}
      </dl>
      <LiveMessage>
        {failed && !(failed instanceof AuthFailure && failed.kind === 'refused') ? (
          <Callout tone="rework">{failureText(failed)}</Callout>
        ) : null}
      </LiveMessage>
      <div className="flex gap-2">
        <Button
          variant="white"
          size="lg"
          aria-busy={deny.isPending || undefined}
          disabled={busy}
          onClick={() => deny.mutate(code)}
          className="flex-1 justify-center text-rework-ink aria-busy:cursor-progress"
        >
          {deny.isPending ? <Spinner tone="dark" /> : null}
          Deny
        </Button>
        <Button
          variant="primary"
          size="lg"
          aria-busy={approve.isPending || undefined}
          disabled={busy}
          onClick={() => approve.mutate(code)}
          className="flex-1 justify-center aria-busy:cursor-progress"
        >
          {approve.isPending ? <Spinner /> : null}
          Approve
        </Button>
        {busy ? <span className="sr-only">Loading</span> : null}
      </div>
      <p className="m-0 text-meta text-muted">
        Didn’t run <span className="font-mono">goal login</span> just now? Deny it. Nothing else happens.
      </p>
    </AuthPage>
  );
}

/** Signed out (board state "Signed out"): sign in first, then this page asks for the code again. */
export function SignInToApprove() {
  return (
    <AuthPage>
      <AuthHeading>Sign in to approve the Goal CLI</AuthHeading>
      <p className="m-0 text-body text-ink-soft">You’ll come back to this page after signing in.</p>
      <Link
        to="/sign-in"
        search={{ next: '/device' }}
        className="inline-flex h-11 w-full items-center justify-center rounded-control bg-run px-4 font-control text-white no-underline transition-colors duration-200 hover:bg-run/90"
      >
        Sign in
      </Link>
    </AuthPage>
  );
}
