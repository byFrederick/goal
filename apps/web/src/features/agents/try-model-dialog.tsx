// Try another model (ADR 0039 decision 13, docs/design/screens/build/agent): pick a model the policy allows, see
// what the tries would cost, confirm. The estimate is the same call without confirm; confirming stores a proposal
// that Jonah tries three times on each of the agent's last finished items. Both calls need definition:edit.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { toast } from 'sonner';
import { ApiError, proposeAgentModel } from '../../api/client';
import { builderMetaQuery, keys, policyQuery } from '../../api/queries';
import { formatUsdCost, plural } from '../../shared/format';
import { modelLabel } from '../../shared/models';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Dialog, DialogClose, DialogContent, DialogTrigger } from '../../ui/dialog';
import { Field } from '../../ui/field';
import { Select } from '../../ui/select';
import { Skeleton } from '../../ui/skeleton';
import { InfoTip } from '../jonah/info-tip';

/** What Try another model says when goald refuses, by the API's code. */
export function tryModelFailure(err: unknown): string {
  const code = err instanceof ApiError ? err.code : undefined;
  if (code === 'model-not-allowed') return 'Your policy does not allow that model, or the agent already uses it.';
  if (code === 'agent-not-triable') return 'This agent needs tools or secrets Jonah cannot give a try.';
  if (code === 'no-finished-items') return 'This agent has no finished item to try the model on yet.';
  if (code === 'learning-budget-exhausted') return 'Jonah’s budget for this week would not cover the tries.';
  if (code === 'jonah-unavailable') return 'Jonah is not turned on for this installation yet.';
  return 'goald is not answering. Try again in a moment.';
}

const block = 'rounded-control bg-hover';

export function TryModelButton({
  orgId,
  factoryId,
  agent,
  currentModel,
}: {
  orgId: string;
  factoryId: string;
  agent: string;
  /** The model the agent runs on now, left out of the choices. */
  currentModel: string | undefined;
}) {
  const [open, setOpen] = useState(false);
  const [model, setModel] = useState('');
  const queryClient = useQueryClient();
  const meta = useQuery({ ...builderMetaQuery(orgId), enabled: open });
  const policy = useQuery({ ...policyQuery(orgId), enabled: open, retry: false });
  const installed = meta.data?.models ?? [];
  // The policy narrows the install's models; when it can't be read, the install's list stands.
  const allowed = policy.data?.policy.allowed_models;
  const choices = installed.filter((m) => m.id !== currentModel && (!allowed || allowed.includes(m.id)));

  const estimate = useQuery({
    queryKey: ['org', orgId, 'factory', factoryId, 'model-estimate', agent, model] as const,
    queryFn: () => proposeAgentModel(orgId, factoryId, agent, model, false),
    enabled: open && model !== '',
    retry: false,
    gcTime: 0,
  });
  const confirm = useMutation({
    mutationFn: () => proposeAgentModel(orgId, factoryId, agent, model, true),
    onSuccess: async () => {
      toast.success('Proposal made');
      setOpen(false);
      await queryClient.invalidateQueries({ queryKey: keys.jonah(orgId, factoryId) });
    },
  });

  const est = estimate.data?.estimate;
  const overBudget = est !== undefined && est.estimate_usd > est.budget_left_usd;
  const error = confirm.isError ? confirm.error : estimate.isError ? estimate.error : null;
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) {
          setModel('');
          confirm.reset();
        }
      }}
    >
      <DialogTrigger asChild>
        <Button variant="secondary" size="sm" aria-haspopup="dialog">
          Try another model
        </Button>
      </DialogTrigger>
      <DialogContent title="Try another model" description={`Jonah tries it on what ${agent} finished lately.`}>
        <form
          className="flex flex-col gap-4"
          noValidate
          onSubmit={(e) => {
            e.preventDefault();
            e.stopPropagation();
            if (est && !confirm.isPending) confirm.mutate();
          }}
        >
          <Field label="Model">
            <Select
              value={model}
              onValueChange={(next) => {
                confirm.reset();
                setModel(next);
              }}
              placeholder={meta.isPending ? 'Loading models…' : 'Choose a model'}
              options={choices.map((m) => ({ value: m.id, label: modelLabel(installed, m.id) }))}
            />
          </Field>
          {model !== '' ? (
            <div className="flex flex-col gap-2" aria-live="polite">
              <div className="flex items-center gap-1 text-secondary font-control text-ink">
                Estimate
                <InfoTip label="How the estimate is worked out">
                  Three tries on each of the agent’s newest finished items, each costing what its original run cost.
                  The tries come out of Jonah’s budget.
                </InfoTip>
              </div>
              <Skeleton
                ready={!estimate.isPending || estimate.isError}
                placeholder={
                  <div className="flex flex-col gap-2">
                    <div className={`${block} h-5 w-2/3`} />
                    <div className={`${block} h-5 w-1/2`} />
                  </div>
                }
              >
                {est ? (
                  <ul className="m-0 flex list-none flex-col gap-1 p-0 text-body text-ink-soft">
                    <li>
                      {plural(est.tries, 'try', 'tries')} on {plural(est.items.length, 'item')}
                    </li>
                    <li className="tabular-nums">
                      About {formatUsdCost(est.estimate_usd)} of {formatUsdCost(est.budget_left_usd)} left this week
                    </li>
                  </ul>
                ) : (
                  <span />
                )}
              </Skeleton>
              {overBudget ? <Callout tone="rework">Jonah’s budget would not cover these tries.</Callout> : null}
            </div>
          ) : null}
          {error ? (
            <div role="alert">
              <Callout tone="rework">{tryModelFailure(error)}</Callout>
            </div>
          ) : null}
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary">Cancel</Button>
            </DialogClose>
            <Button type="submit" variant="primary" disabled={!est || confirm.isPending}>
              {confirm.isPending ? 'Making the proposal…' : 'Make the proposal'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
