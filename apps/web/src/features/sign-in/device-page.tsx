// Device-Approve (canvas "Device-Approve", "Mobile-Device-Approve"; spec 3 §10.2): the person who
// ran goal login types the code their terminal shows, sees who asks (the client goald checked, the
// address and tool it asked from, whether that is this browser's network, when) and approves or
// denies. The code comes only from what is typed, never from the page's URL, so a link someone
// sends cannot approve anything; approving needs a sign-in from the last 10 minutes, which the
// re-auth dialog asks for.
import { useMutation, useSuspenseQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Check, TriangleAlert } from 'lucide-react';
import { type FormEvent, type Ref, useEffect, useRef, useState } from 'react';
import { AuthFailure, authMutations, claimDeviceCode } from '../../api/auth';
import { ApiError, getDeviceRequest } from '../../api/client';
import { meQuery } from '../../api/queries';
import type { DeviceRequest, Me } from '../../api/types';
import { displayName, hasName } from '../../shared/names';
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
          <p className="m-0 text-[15px] leading-normal text-ink-soft">You can close this tab.</p>
        </AuthPage>
      );
    case 'denied':
      return (
        <AuthPage>
          <AuthHeading ref={heading}>Denied</AuthHeading>
          <p className="m-0 text-[15px] leading-normal text-ink-soft">The CLI was not signed in.</p>
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
      <AuthHeading ref={heading}>Sign in to the Goal CLI</AuthHeading>
      <p className="m-0 text-[15px] leading-normal text-ink-soft">Enter the code shown in your terminal.</p>
      <form onSubmit={onSubmit} noValidate className="flex flex-col gap-3.5">
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
            className="h-14 text-center font-mono text-2xl tracking-[0.12em] md:h-14 md:text-2xl"
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
          className="h-12 w-full justify-center aria-busy:cursor-progress"
        >
          {find.isPending ? (
            <>
              <Spinner />
              Checking…
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

/** Step 2: who asks, from where and when; approve or deny. */
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

  return (
    <AuthPage wide>
      <AuthHeading ref={heading}>Sign in to the Goal CLI</AuthHeading>
      <p className="m-0 text-base leading-normal text-ink sm:text-[17px]">
        {request.client_name} wants to sign in as{' '}
        <strong className="font-medium">{me.user ? displayName(me.user) : 'you'}</strong>
        {me.user && hasName(me.user) ? ` (${me.user.email})` : null}
      </p>
      <p className="m-0 flex flex-wrap items-center gap-2.5 text-sm text-ink-soft">
        Code you entered
        <span className="inline-flex h-8 items-center rounded-chip border border-line bg-surface px-2.5 font-mono text-[15px] tracking-[0.06em] text-ink">
          {code}
        </span>
      </p>
      <dl className="m-0 grid grid-cols-1 gap-x-4 gap-y-0.5 rounded-control border border-line bg-surface px-3.5 py-3 text-sm leading-snug sm:grid-cols-[140px_minmax(0,1fr)] sm:gap-y-2.5 sm:px-4 sm:py-3.5">
        {request.ip ? (
          <>
            <dt className="text-muted">Requested from</dt>
            <dd className="m-0 mb-2 flex min-w-0 flex-wrap items-center gap-x-2.5 gap-y-1.5 sm:mb-0">
              <span className="font-mono">{request.ip}</span>
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
        {request.user_agent ? (
          <>
            <dt className="text-muted">Browser or tool</dt>
            <dd className="m-0 mb-2 min-w-0 font-mono break-words sm:mb-0">{request.user_agent}</dd>
          </>
        ) : null}
        <dt className="text-muted">Requested</dt>
        <dd className="m-0 min-w-0">{requestTimes(request.requested_at, request.expires_at, now)}</dd>
      </dl>
      <p
        role="note"
        className="m-0 flex items-start gap-2.5 rounded-control border border-rework-line bg-rework-tint px-3 py-2.5 text-sm leading-snug text-rework-ink"
      >
        <TriangleAlert size={16} strokeWidth={1.6} aria-hidden="true" className="mt-0.5 shrink-0" />
        <span>
          Only approve if you just ran <span className="font-mono text-[13px]">goal login</span> yourself. Never approve
          a code someone sent you.
        </span>
      </p>
      <LiveMessage>
        {failed && !(failed instanceof AuthFailure && failed.kind === 'refused') ? (
          <Callout tone="rework">{failureText(failed)}</Callout>
        ) : null}
      </LiveMessage>
      <div className="flex gap-2">
        <Button
          variant="primary"
          size="lg"
          aria-busy={approve.isPending || undefined}
          disabled={busy}
          onClick={() => approve.mutate(code)}
          className="h-12 flex-1 justify-center aria-busy:cursor-progress"
        >
          {approve.isPending ? <Spinner /> : null}
          Approve
        </Button>
        <Button
          variant="white"
          size="lg"
          aria-busy={deny.isPending || undefined}
          disabled={busy}
          onClick={() => deny.mutate(code)}
          className="h-12 flex-1 justify-center aria-busy:cursor-progress"
        >
          {deny.isPending ? <Spinner tone="dark" /> : null}
          Deny
        </Button>
      </div>
    </AuthPage>
  );
}

/** Signed out (board state "Signed out"): sign in first, then this page asks for the code again. */
export function SignInToApprove() {
  return (
    <AuthPage>
      <AuthHeading>Sign in to approve the Goal CLI</AuthHeading>
      <p className="m-0 text-[15px] leading-normal text-ink-soft">You’ll come back to this page after signing in.</p>
      <Link
        to="/sign-in"
        search={{ next: '/device' }}
        className="inline-flex h-12 w-full items-center justify-center rounded-control bg-ink px-4 text-[15px] font-medium text-white no-underline transition-colors duration-200 hover:bg-ink/90"
      >
        Sign in
      </Link>
    </AuthPage>
  );
}
