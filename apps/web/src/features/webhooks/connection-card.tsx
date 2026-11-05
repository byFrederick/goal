// Connection: the address a sender posts to (with Copy), the secret that signs it by name (never its value;
// Rotate is on the secret's page), the sender, and its status: for GitHub, whether its ping arrived.
import { Link } from '@tanstack/react-router';
import type { Webhook } from '../../api/webhooks';
import { useOrgHolds } from '../../shared/org';
import { CopyButton } from '../../ui/copy-button';
import { InfoTip } from '../../ui/info-tip';
import { Card, StatusChip } from './parts';
import { addressUrl, pingText, senderLabel, statusOf } from './words';

const row = 'grid grid-cols-[88px_minmax(0,1fr)] items-start gap-x-4 gap-y-1 px-4 py-2.5 text-secondary';

export function ConnectionCard({ webhook, orgSlug }: { webhook: Webhook; orgSlug: string }) {
  const canRotate = useOrgHolds('secret:manage');
  const url = addressUrl(webhook, window.location.origin);
  const ping = pingText(webhook);
  return (
    <Card title="Connection" aside={<StatusChip status={statusOf(webhook)} />}>
      <dl className="m-0 flex flex-col py-1">
        <div className={row}>
          <dt className="text-muted">Address</dt>
          <dd className="m-0 flex min-w-0 items-center gap-2">
            <code className="min-w-0 flex-1 break-all font-mono text-ink">{url}</code>
            <CopyButton text={url} aria-label="Copy the address" />
          </dd>
        </div>
        <div className={row}>
          <dt className="text-muted">Secret</dt>
          <dd className="m-0 flex min-w-0 flex-wrap items-center gap-x-1">
            <Link
              to="/o/$org/secrets/$secret"
              params={{ org: orgSlug, secret: webhook.secret }}
              className="break-all font-mono text-ink underline-offset-2 hover:underline"
            >
              {webhook.secret}
            </Link>
            <span className="text-muted">in Secrets</span>
            <InfoTip about="the secret">
              Checked on every delivery and never shown again. Replace its value in Secrets, then give the sender the
              new one.
            </InfoTip>
            {canRotate ? (
              <Link
                to="/o/$org/secrets/$secret"
                params={{ org: orgSlug, secret: webhook.secret }}
                className="text-link underline-offset-2 hover:underline"
              >
                Rotate
              </Link>
            ) : null}
          </dd>
        </div>
        <div className={row}>
          <dt className="text-muted">Sender</dt>
          <dd className="m-0 flex min-w-0 flex-wrap items-center gap-x-1">
            {webhook.sender === 'github' ? 'GitHub organization webhook' : senderLabel(webhook.sender)}
            {webhook.sender === 'github' ? (
              <InfoTip about="organization webhooks">
                Covers every repository in the organization, including ones created later. A personal account adds the
                same address to each repository instead.
              </InfoTip>
            ) : null}
          </dd>
        </div>
        {webhook.token ? (
          <div className={row}>
            <dt className="text-muted">Token</dt>
            <dd className="m-0 flex min-w-0 flex-col font-mono text-ink">
              <span>Header {webhook.token.header || 'Authorization'}</span>
              {webhook.token.event_path ? <span>Event {webhook.token.event_path}</span> : null}
              {webhook.token.id_path ? <span>Delivery id {webhook.token.id_path}</span> : null}
            </dd>
          </div>
        ) : null}
        {ping ? (
          <div className={row}>
            <dt className="text-muted">Status</dt>
            <dd className={webhook.pinged_at ? 'm-0 text-ok-ink' : 'm-0 text-muted'}>{ping}</dd>
          </div>
        ) : null}
      </dl>
    </Card>
  );
}
