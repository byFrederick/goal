// One webhook (ADR 0040 decisions 12 and 13, canvas docs/design/screens/organization/webhook.jpg): its
// connection, the factories it is open to and the triggers that use it, how to set it up in its sender, and
// the delivery log. webhook:manage adds Edit, Delete and the bodies of deliveries; everyone who reads
// webhooks sees the rest. A name that does not exist is the route's not-found screen.
import { useMutation, useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query';
import { useNavigate, useParams } from '@tanstack/react-router';
import { useState } from 'react';
import { factoriesQuery, keys, webhookQuery } from '../../api/queries';
import { deleteWebhook } from '../../api/webhooks';
import { TopBarSlot } from '../../app/top-bar';
import { useOrg, useOrgHolds } from '../../shared/org';
import { Button } from '../../ui/button';
import { ConfirmDialog } from '../../ui/confirm-dialog';
import { ConnectionCard } from './connection-card';
import { DeliveriesCard } from './deliveries-card';
import { EditWebhookDialog, inUseTriggers, TriggersInUse } from './edit-webhook-dialog';
import { OpeningsCard } from './openings-card';
import { RequestDialog, type RequestTarget } from './request-dialog';
import { SetupCard } from './setup-card';
import { senderLabel } from './words';

export function WebhookPage() {
  const org = useOrg();
  const params = useParams({ from: '/o/$org/_org/webhooks/$webhook' });
  const { data: webhook } = useSuspenseQuery(webhookQuery(org.id, params.webhook));
  const factories = useQuery(factoriesQuery(org.id)).data ?? [];
  const manage = useOrgHolds('webhook:manage');
  const navigate = useNavigate();
  const client = useQueryClient();
  const [editing, setEditing] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [target, setTarget] = useState<RequestTarget | null>(null);

  const remove = useMutation({
    mutationFn: () => deleteWebhook(org.id, webhook.name),
    gcTime: 0,
    onSuccess: async () => {
      client.removeQueries({ queryKey: keys.webhook(org.id, webhook.name) });
      await client.invalidateQueries({ queryKey: keys.webhooks(org.id) });
      setDeleting(false);
      await navigate({ to: '/o/$org/webhooks', params: { org: org.slug } });
    },
  });
  const triggers = inUseTriggers(remove.error);

  return (
    <>
      <TopBarSlot
        crumb={webhook.name}
        actions={
          manage ? (
            <Button variant="danger" onClick={() => setDeleting(true)}>
              Delete
            </Button>
          ) : undefined
        }
      />
      <div className="flex min-w-0 flex-col gap-5">
        <header className="flex min-w-0 flex-col gap-1">
          <h1 className="m-0 break-words text-page-title tracking-display">{webhook.name}</h1>
          <span className="text-secondary text-muted">{senderLabel(webhook.sender)}</span>
        </header>
        <div className="grid min-w-0 items-start gap-5 lg:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
          <div className="flex min-w-0 flex-col gap-5">
            <ConnectionCard webhook={webhook} orgSlug={org.slug} />
            <DeliveriesCard
              org={org.id}
              orgSlug={org.slug}
              webhook={webhook.name}
              factories={factories}
              canManage={manage}
              onView={setTarget}
            />
          </div>
          <div className="flex min-w-0 flex-col gap-5">
            <OpeningsCard
              webhook={webhook}
              orgSlug={org.slug}
              factories={factories}
              canManage={manage}
              onEdit={() => setEditing(true)}
            />
            <SetupCard webhook={webhook} />
          </div>
        </div>
      </div>
      {editing ? (
        <EditWebhookDialog
          org={org.id}
          webhook={webhook}
          factories={factories}
          onClose={() => setEditing(false)}
          onRenamed={(name) =>
            void navigate({ to: '/o/$org/webhooks/$webhook', params: { org: org.slug, webhook: name }, replace: true })
          }
        />
      ) : null}
      <ConfirmDialog
        open={deleting}
        onOpenChange={(open) => {
          setDeleting(open);
          if (!open) remove.reset();
        }}
        title={`Delete ${webhook.name}?`}
        description="Its address stops accepting requests, and its openings and deliveries go. Its secret stays in Secrets."
        confirmLabel="Delete webhook"
        pendingLabel="Deleting…"
        tone="danger"
        pending={remove.isPending}
        onConfirm={() => remove.mutate()}
      >
        {triggers ? <TriggersInUse triggers={triggers} factories={factories} /> : null}
      </ConfirmDialog>
      <RequestDialog target={target} onClose={() => setTarget(null)} />
    </>
  );
}
