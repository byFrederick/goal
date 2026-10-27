// One endpoint: its address with Copy, its signing secret's name with Rotate, the triggers it feeds
// (or the words that it feeds none), the last seven days, and its newest deliveries (ADR 0026). The
// secret is never read here: Rotate asks the server for a new one and hands it to the page's dialog.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Check, Copy, KeyRound, RotateCw } from 'lucide-react';
import { useState } from 'react';
import { ApiError } from '../../api/client';
import { keys } from '../../api/queries';
import { rotateWebhookSecret, type WebhookEndpoint } from '../../api/webhooks';
import { Button } from '../../ui/button';
import { Chip, Dot } from '../../ui/chip';
import { ConfirmDialog } from '../../ui/confirm-dialog';
import { DayBars } from './day-bars';
import { Deliveries, type DeliveriesScope } from './deliveries';
import type { RequestTarget } from './request-dialog';
import type { ShownSecret } from './secret-dialog';
import { addressUrl, feedsNothing, refusedChip, weekTotals } from './words';

/** Why a rotation failed, in the dialog's words. The old secret is only changed when goald answers. */
function rotateFailure(err: Error): string {
  if (!(err instanceof ApiError)) {
    return 'Not rotated: goald is not answering. Check the sender before you change its secret, then try again.';
  }
  return `Not rotated: ${err.detail ?? err.title}. The old secret still works.`;
}

export interface EndpointCardProps {
  scope: DeliveriesScope;
  endpoint: WebhookEndpoint;
  /** The organization's slug, for the trigger page's address. */
  orgSlug: string;
  factoryName: string;
  canManage: boolean;
  onShown: (shown: ShownSecret) => void;
  onView: (target: RequestTarget) => void;
}

export function EndpointCard({ scope, endpoint, orgSlug, factoryName, canManage, onShown, onView }: EndpointCardProps) {
  const queryClient = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const [copied, setCopied] = useState(false);
  const { refused } = weekTotals(endpoint.days);
  const url = addressUrl(endpoint, window.location.origin);
  const headingId = `webhook-${endpoint.id}`;

  const rotate = useMutation({
    // The new secret goes to the page's dialog and nowhere else: the mutation's result is not kept.
    mutationFn: async () => {
      const rotated = await rotateWebhookSecret(scope.org, scope.factory, endpoint.id);
      onShown({ endpointName: rotated.name, secret: rotated.secret, reason: 'rotated' });
    },
    // The confirmation closes only when the secret is shown; a failure stays in it, with the reason.
    onSuccess: () => setConfirming(false),
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.webhooks(scope.org, scope.factory) }),
  });

  const openRotate = () => {
    rotate.reset();
    setConfirming(true);
  };

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(url);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };

  return (
    <section
      aria-labelledby={headingId}
      className="flex min-w-0 flex-col overflow-hidden rounded-card border border-line"
    >
      <header className="flex flex-wrap items-center gap-3 border-b border-line px-4 py-3">
        <h2 id={headingId} className="m-0 min-w-0 text-heading break-words">
          {endpoint.name}
        </h2>
        {refused > 0 ? (
          <Chip tone="rework" className="ml-auto">
            <Dot />
            {refusedChip(refused)}
          </Chip>
        ) : null}
      </header>

      <dl className="m-0 grid grid-cols-[minmax(0,7.5rem)_minmax(0,1fr)] items-center gap-x-3 gap-y-3 px-4 py-3 text-secondary">
        <dt className="text-muted">Address</dt>
        <dd className="m-0 flex min-w-0 flex-wrap items-center gap-2">
          <span className="min-w-0 max-w-full overflow-hidden rounded-control border border-line bg-panel px-3 py-1 font-mono text-secondary text-ellipsis whitespace-nowrap">
            {url}
          </span>
          <Button variant="secondary" size="sm" onClick={() => void copy()}>
            {copied ? (
              <Check size={14} strokeWidth={1.5} aria-hidden="true" />
            ) : (
              <Copy size={14} strokeWidth={1.5} aria-hidden="true" />
            )}
            Copy
          </Button>
          <span role="status" className="text-meta text-muted">
            {copied ? 'Copied.' : ''}
          </span>
        </dd>

        <dt className="text-muted">Signing secret</dt>
        <dd className="m-0 flex min-w-0 flex-wrap items-center gap-2">
          <Chip>
            <KeyRound size={12} strokeWidth={1.5} aria-hidden="true" />
            {endpoint.secret_name}
          </Chip>
          <span className="text-meta text-muted">HMAC SHA-256</span>
          {canManage ? (
            <Button variant="ghost" size="sm" onClick={openRotate}>
              <RotateCw size={14} strokeWidth={1.5} aria-hidden="true" />
              Rotate
            </Button>
          ) : null}
        </dd>

        <dt className="text-muted">Feeds</dt>
        <dd className="m-0 flex min-w-0 flex-wrap items-center gap-2">
          {endpoint.triggers.length > 0 ? (
            endpoint.triggers.map((name) => (
              <Chip key={name} tone="run">
                {name}
              </Chip>
            ))
          ) : (
            <>
              <span className="text-muted">{feedsNothing(endpoint.days)}</span>
              <Button asChild variant="primary" size="sm">
                <a href={`/o/${orgSlug}/f/${factoryName}/intake/new`}>Create a trigger</a>
              </Button>
            </>
          )}
        </dd>

        <dt className="text-muted">Last 7 days</dt>
        <dd className="m-0 min-w-0">
          <DayBars days={endpoint.days} />
        </dd>
      </dl>

      <Deliveries scope={scope} onView={onView} />

      <ConfirmDialog
        open={confirming}
        onOpenChange={(open) => {
          if (!open) setConfirming(false);
        }}
        title={`Rotate the signing secret of ${endpoint.name}?`}
        description="The sender stops being accepted until it has the new secret. The new one is shown once, next."
        confirmLabel="Rotate secret"
        pendingLabel="Rotating…"
        pending={rotate.isPending}
        tone="danger"
        onConfirm={() => rotate.mutate()}
      >
        {rotate.error ? (
          <p role="alert" className="m-0 text-secondary text-rework-ink">
            {rotateFailure(rotate.error)}
          </p>
        ) : null}
      </ConfirmDialog>
    </section>
  );
}
