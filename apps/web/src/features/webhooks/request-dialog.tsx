// View request: one delivery as it was received, its headers and its body (ADR 0026). The signature
// headers are never here. A body that is not UTF-8 text is labelled as base64.
import { useQuery } from '@tanstack/react-query';
import { ApiError } from '../../api/client';
import { webhookDeliveryQuery } from '../../api/queries';
import { Callout } from '../../ui/callout';
import { Dialog, DialogContent } from '../../ui/dialog';
import { Spinner } from '../../ui/spinner';

export interface RequestTarget {
  org: string;
  factory: string;
  endpoint: string;
  endpointName: string;
  delivery: string;
  receivedAt: string;
}

export function RequestDialog({ target, onClose }: { target: RequestTarget | null; onClose: () => void }) {
  return (
    <Dialog open={target !== null} onOpenChange={(open) => (open ? undefined : onClose())}>
      {target ? <Request target={target} /> : null}
    </Dialog>
  );
}

function Request({ target }: { target: RequestTarget }) {
  const request = useQuery(webhookDeliveryQuery(target.org, target.factory, target.endpoint, target.delivery));
  const data = request.data;
  return (
    <DialogContent
      title="Request"
      description={`${target.endpointName}, received ${target.receivedAt}`}
      className="max-w-[680px]"
    >
      {request.isPending ? (
        <div aria-busy="true" className="flex items-center gap-2 text-body text-ink-soft">
          <Spinner />
          <span role="status">Loading the request</span>
        </div>
      ) : request.isError ? (
        <Callout tone="rework">
          {request.error instanceof ApiError && request.error.code === 'webhook-delivery-not-found'
            ? 'This delivery is no longer kept.'
            : 'Couldn’t load the request. Check your connection and try again.'}
        </Callout>
      ) : data ? (
        <div className="flex min-w-0 flex-col gap-4">
          <section aria-labelledby={`${target.delivery}-headers`} className="flex flex-col gap-2">
            <h3 id={`${target.delivery}-headers`} className="m-0 text-heading">
              Headers
            </h3>
            {Object.keys(data.headers).length === 0 ? (
              <p className="m-0 text-secondary text-muted">No headers were kept.</p>
            ) : (
              <dl className="m-0 grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1 font-mono text-secondary">
                {Object.entries(data.headers).map(([name, value]) => (
                  <div key={name} className="contents">
                    <dt className="text-muted">{name}</dt>
                    <dd className="m-0 min-w-0 break-all text-ink">{value}</dd>
                  </div>
                ))}
              </dl>
            )}
          </section>
          <section aria-labelledby={`${target.delivery}-body`} className="flex flex-col gap-2">
            <h3 id={`${target.delivery}-body`} className="m-0 text-heading">
              Body
            </h3>
            {data.body_encoding === 'base64' ? (
              <p className="m-0 text-meta text-muted">Not UTF-8 text: shown as base64.</p>
            ) : null}
            <pre className="m-0 max-h-80 overflow-auto rounded-control border border-line bg-panel p-3 font-mono text-secondary break-all whitespace-pre-wrap text-ink">
              {data.body === '' ? 'No body' : data.body}
            </pre>
          </section>
        </div>
      ) : null}
    </DialogContent>
  );
}
