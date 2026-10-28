// The Intake page (intake, ADR 0026): where the factory's newest items came from, its triggers with their
// last 7 days and switches, and the suggested triggers. Its four states: loading, error, empty (no items and
// no triggers) and ready. The New trigger picker renders over this page at …/intake/new.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Plus, Webhook } from 'lucide-react';
import { intakeQuery } from '../../api/queries';
import type { Intake } from '../../api/types';
import { ErrorScreen, PendingScreen } from '../../app/states';
import { TopBarSlot } from '../../app/top-bar';
import { factoryTitle, useFactory, useFactoryParams } from '../../shared/factory';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { EmptyState } from '../../ui/empty-state';
import { SourcesBar } from './sources-bar';
import { SuggestedTriggers } from './suggested-triggers';
import type { SuggestionGroup } from './suggestions';
import { TriggersTable } from './triggers-table';

export function IntakePage({
  group,
  onGroupChange,
}: {
  group: SuggestionGroup;
  onGroupChange: (group: SuggestionGroup) => void;
}) {
  const org = useOrg();
  const factory = useFactory();
  const query = useQuery(intakeQuery(org.id, factory.id));

  if (query.isPending) return <PendingScreen />;
  if (query.isError) return <ErrorScreen error={query.error} reset={() => void query.refetch()} />;
  return <Ready intake={query.data} group={group} onGroupChange={onGroupChange} />;
}

function Ready({
  intake,
  group,
  onGroupChange,
}: {
  intake: Intake;
  group: SuggestionGroup;
  onGroupChange: (group: SuggestionGroup) => void;
}) {
  const factory = useFactory();
  const empty = intake.items === 0 && intake.triggers.length === 0;

  return (
    <>
      <IntakeActions />
      <div className="flex flex-col gap-6 px-4 pt-4 pb-10 md:px-7 md:pt-6">
        <Header name={factoryTitle(factory)} />
        {empty ? (
          <EmptyState icon={<Webhook aria-hidden="true" />} title="No triggers yet" actions={<NewTriggerLink />}>
            Triggers turn events and schedules into work items. Add one, or create an item by hand.
          </EmptyState>
        ) : (
          <>
            <SourcesBar intake={intake} />
            <TriggersTable triggers={intake.triggers} />
          </>
        )}
        <SuggestedTriggers group={group} onGroupChange={onGroupChange} />
      </div>
    </>
  );
}

function Header({ name }: { name: string }) {
  return (
    <header className="flex flex-col gap-1">
      <h1 className="m-0 text-page-title">Intake</h1>
      <p className="m-0 text-body text-muted">Triggers turn events and schedules into work items on {name}'s line.</p>
    </header>
  );
}

/** The top bar's actions: Webhooks (its page comes with the Webhooks item) and New trigger, the primary one last. */
function IntakeActions() {
  const params = useFactoryParams();
  return (
    <TopBarSlot
      actions={[
        <Button key="webhooks" asChild variant="secondary">
          <a href={`/o/${params.org}/f/${params.factory}/intake/webhooks`}>
            <Webhook aria-hidden="true" />
            Webhooks
          </a>
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
