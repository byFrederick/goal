// Send a test: a curl sample for the first endpoint's address, the fields its trigger can read, and a
// sample delivery that is not built yet (ADR 0016, decision 4). The browser never holds a secret, so it
// cannot sign a request to /hooks/{id}; the sample is for the sender's side.
import { useQuery } from '@tanstack/react-query';
import { webhookDeliveryQuery } from '../../api/queries';
import type { WebhookEndpoint } from '../../api/webhooks';
import { Button } from '../../ui/button';
import { CopyButton } from '../../ui/copy-button';
import { NotAvailable } from '../../ui/not-available';
import type { DeliveriesScope } from './deliveries';
import { addressUrl, signatureHeader } from './words';

/** The newest delivery the endpoint accepted (with or without an item), whose body the fields come from. */
export function TestPanel({
  scope,
  endpoint,
  newestAccepted,
}: {
  scope: DeliveriesScope;
  endpoint: WebhookEndpoint;
  newestAccepted: string | undefined;
}) {
  const fields = useFields(scope, newestAccepted);
  const url = addressUrl(endpoint, window.location.origin);
  const curl = curlSample(url, signatureHeader(endpoint.kind));
  return (
    <aside aria-labelledby="send-a-test" className="flex min-w-0 flex-col gap-3 rounded-card border border-line p-5">
      <h2 id="send-a-test" className="m-0 text-heading">
        Send a test
      </h2>
      <p className="m-0 text-secondary text-ink-soft">Post JSON to the address with the signature header.</p>
      <div className="relative">
        <pre className="m-0 overflow-x-auto rounded-control border border-line bg-panel p-4 font-mono text-meta leading-relaxed text-ink">
          {curl}
        </pre>
        <CopyButton text={curl} className="absolute top-2 right-2" />
      </div>
      <div className="flex flex-col gap-2">
        <span className="text-secondary font-medium">What the trigger can read</span>
        <ul className="m-0 flex list-none flex-wrap gap-2 p-0">
          {fields.map((field) => (
            <li key={field}>
              <code className="inline-flex h-5.5 items-center rounded-chip border border-line bg-panel px-2 font-mono text-xs text-ink">
                {field}
              </code>
            </li>
          ))}
        </ul>
        <span className="text-meta text-muted">Fields reach the agent as untrusted data, never as instructions.</span>
      </div>
      <NotAvailable label="Send a sample delivery">
        <Button variant="secondary" className="self-start">
          Send a sample delivery
        </Button>
      </NotAvailable>
    </aside>
  );
}

/**
 * The top-level keys of the newest accepted delivery's JSON body, or `event` alone when there is no such
 * delivery or its body is not a JSON object: the field the generic kind reads.
 */
function useFields(scope: DeliveriesScope, delivery: string | undefined): string[] {
  const request = useQuery({
    ...webhookDeliveryQuery(scope.org, scope.factory, scope.endpoint, delivery ?? ''),
    enabled: delivery !== undefined,
  });
  const body = request.data;
  if (body?.body_encoding !== 'text') return ['event'];
  try {
    const parsed: unknown = JSON.parse(body.body);
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return ['event'];
    const keys = Object.keys(parsed);
    return keys.length > 0 ? keys : ['event'];
  } catch {
    return ['event'];
  }
}

/** The sample request a sender posts: the address, the signature header and a JSON body. */
function curlSample(url: string, header: string): string {
  return [
    'curl -X POST \\',
    `  ${url} \\`,
    '  -H "Content-Type: application/json" \\',
    `  -H "${header}: sha256=…" \\`,
    "  -d '{",
    '    "event": "deploy.failed",',
    '    "service": "goal-api",',
    '    "commit": "a41f9c2",',
    '    "log_url": "https://dash.example.com/…"',
    "  }'",
  ].join('\n');
}
