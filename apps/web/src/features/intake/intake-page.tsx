// The Intake page (intake, ADR 0026 and 0040): the items coming up, where items came from, the follow-ups to
// triage, the triggers with their switches (the definition's off ones are its suggestions) and the webhooks
// open to the factory. Its states: loading, error and ready. The New trigger picker renders over this page at …/intake/new.
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Plus, Webhook } from 'lucide-react';
import { useState } from 'react';
import type { IntakeWindow } from '../../api/intake';
import { intakeQuery } from '../../api/queries';
import type { Intake } from '../../api/types';
import { ErrorScreen } from '../../app/states';
import { TopBarSlot } from '../../app/top-bar';
import { useFactory, useFactoryParams } from '../../shared/factory';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { EmptyState } from '../../ui/empty-state';
import { ComingUp } from './coming-up';
import { IntakeSkeleton } from './intake-skeleton';
import { OriginsCard } from './origins-card';
import { TriageCard } from './triage-card';
import { TriggersTable } from './triggers-table';
import { WebhooksCard } from './webhooks-card';

export function IntakePage() {
  const org = useOrg();
  const factory = useFactory();
  const [window, setWindow] = useState<IntakeWindow>('30d');
  // The server answers 30d without the parameter, so the default shares the Floor's query.
  const query = useQuery({
    ...intakeQuery(org.id, factory.id, window === '30d' ? undefined : window),
    placeholderData: keepPreviousData,
  });

  if (query.isPending) return <IntakeSkeleton />;
  if (query.isError) return <ErrorScreen error={query.error} reset={() => void query.refetch()} />;
  return <Ready intake={query.data} window={window} onWindowChange={setWindow} />;
}

function Ready({
  intake,
  window,
  onWindowChange,
}: {
  intake: Intake;
  window: IntakeWindow;
  onWindowChange: (window: IntakeWindow) => void;
}) {
  const comingUp = intake.coming_up ?? [];
  return (
    <>
      <IntakeActions />
      <div className="flex flex-col gap-5">
        <h1 className="m-0 text-page-title">Intake</h1>
        <ComingUp items={comingUp} total={intake.coming_up_total ?? comingUp.length} />
        <div className="grid items-start gap-4 @[860px]:grid-cols-2">
          {intake.origins ? (
            <OriginsCard origins={intake.origins} window={window} onWindowChange={onWindowChange} />
          ) : null}
          <TriageCard triage={intake.triage} />
        </div>
        {intake.triggers.length === 0 ? (
          <EmptyState icon={<Webhook aria-hidden="true" />} title="No triggers yet" actions={<NewTriggerLink />}>
            Triggers turn events and schedules into work items. Add one, or create an item by hand.
          </EmptyState>
        ) : (
          <TriggersTable triggers={intake.triggers} />
        )}
        <WebhooksCard webhooks={intake.webhooks ?? []} />
      </div>
    </>
  );
}

/** The top bar's actions: Webhooks (its page comes with the Webhooks item) and New trigger, the primary one last. A phone's bar keeps New trigger only; the webhooks card links to Webhooks. */
function IntakeActions() {
  const params = useFactoryParams();
  return (
    <TopBarSlot
      actions={[
        <Button key="webhooks" asChild variant="secondary" data-desktop-only>
          <Link to="/o/$org/f/$factory/intake/webhooks" params={params}>
            <Webhook aria-hidden="true" />
            Webhooks
          </Link>
        </Button>,
        <NewTriggerLink key="new" />,
      ]}
    />
  );
}

function NewTriggerLink() {
  const params = useFactoryParams();
  return (
    <Button asChild variant="primary">
      <Link to="/o/$org/f/$factory/intake/new" params={params}>
        <Plus aria-hidden="true" />
        New trigger
      </Link>
    </Button>
  );
}
