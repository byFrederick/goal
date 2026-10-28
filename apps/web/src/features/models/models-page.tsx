// Models and policies (canvas "Models and policies", organization/models.html): the defaults every factory
// in the organization gets, the models agents may use, the four tool policy defaults, the commands no agent
// may run, the box sizes with their time limit, and where agents connect from (ADR 0024). The policy comes
// from getOrgPolicy; Save changes sends it with putOrgPolicy on the revision the form was loaded with.
// Only owners and admins (org:manage) change it: everyone else sees it read-only.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { TriangleAlert } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';
import { ApiError } from '../../api/client';
import type { PolicyLoad } from '../../api/policy';
import { putOrgPolicy } from '../../api/policy';
import { keys, policyQuery } from '../../api/queries';
import { TopBarSlot } from '../../app/top-bar';
import { useOrg, useOrgHolds } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { EmptyState } from '../../ui/empty-state';
import { ReadOnly } from '../../ui/read-only';
import { ViewOnlyBanner } from '../../ui/view-only-banner';
import { BoxCard } from './box-card';
import { CommandsCard } from './commands-card';
import { EgressCard } from './egress-card';
import { changed, formOf, type PolicyForm } from './form';
import { HarnessCard, ModelsCard } from './models-card';
import { ToolCard } from './tool-card';

/** What a save that goald refused without a change says: the edits stay, and the person saves again. */
const conflictCopy = 'The policy changed while you were editing. Your edits are kept: save again to apply them.';

export function ModelsPage() {
  const org = useOrg();
  const query = useQuery(policyQuery(org.id));

  return (
    <div className="flex min-w-0 flex-col gap-5 p-4 md:p-7">
      <header className="flex flex-col gap-1">
        <h1 className="m-0 text-page-title tracking-display">Models and policies</h1>
        <p className="m-0 text-body text-ink-soft">
          Defaults for every factory in Goal. A factory or an agent can choose less, never more.
        </p>
      </header>
      {query.isPending ? (
        <div aria-busy="true">
          <span role="status" className="sr-only">
            Loading models and policies
          </span>
        </div>
      ) : query.isError ? (
        <div
          className="flex max-w-xl flex-col items-start gap-3 rounded-card border border-line bg-card p-5"
          role="alert"
        >
          <Callout tone="rework">Couldn’t load models and policies. Check your connection and try again.</Callout>
          <Button variant="white" onClick={() => void query.refetch()}>
            Try again
          </Button>
        </div>
      ) : query.data.view.models.length === 0 ? (
        <EmptyState icon={<TriangleAlert size={20} strokeWidth={1.5} />} title="No models to choose from">
          This install has no models. Its operator adds them in the install's configuration.
        </EmptyState>
      ) : (
        <Ready load={query.data} org={org.id} />
      )}
    </div>
  );
}

/** The loaded policy as a form. Its edits stay when the policy is read again; only a save replaces them. */
function Ready({ load, org }: { load: PolicyLoad; org: string }) {
  const client = useQueryClient();
  const mayManage = useOrgHolds('org:manage');
  const [form, setForm] = useState<PolicyForm>(() => formOf(load.view.policy));
  const [message, setMessage] = useState<string | null>(null);
  const dirty = changed(formOf(load.view.policy), form);

  const save = useMutation({
    mutationFn: (body: PolicyForm) => putOrgPolicy(org, body, load.etag),
    onSuccess: (next) => {
      client.setQueryData(keys.policy(org), next);
      setMessage(null);
      toast.success('Saved');
    },
    onError: async (err) => {
      if (err instanceof ApiError && err.code === 'policy-conflict') {
        // Read the policy again for its new ETag; the edits in the form stay as they are.
        setMessage(conflictCopy);
        await client.invalidateQueries({ queryKey: keys.policy(org) });
        return;
      }
      setMessage(
        err instanceof ApiError
          ? `Not saved: ${err.detail ?? err.title}`
          : 'goald is not answering. Your edits are still here; save again once it is.',
      );
    },
  });

  const view = load.view;
  return (
    <ReadOnly when={!mayManage}>
      <TopBarSlot
        actions={
          <Button variant="primary" disabled={!mayManage || !dirty || save.isPending} onClick={() => save.mutate(form)}>
            Save changes
          </Button>
        }
      />
      {mayManage ? null : <ViewOnlyBanner>View only. Owners and admins change these policies.</ViewOnlyBanner>}
      {message ? <Callout tone="rework">{message}</Callout> : null}
      <div className="grid gap-4 lg:grid-cols-2">
        <div className="flex min-w-0 flex-col gap-4">
          <ModelsCard form={form} models={view.models} onChange={setForm} />
          <HarnessCard />
          <BoxCard form={form} sizes={view.box_sizes} onChange={setForm} />
        </div>
        <div className="flex min-w-0 flex-col gap-4">
          <ToolCard form={form} onChange={setForm} />
          <CommandsCard form={form} onChange={setForm} />
          <EgressCard addresses={view.egress_addresses} />
        </div>
      </div>
    </ReadOnly>
  );
}
