import { createFileRoute } from '@tanstack/react-router';
import { pageTitle } from '../app/route-focus';
import { IntakePage } from '../features/intake/intake-page';
import { NewTriggerDialog } from '../features/intake/new-trigger-dialog';

// New trigger, step 1 of 2: the Intake page with the source picker over it. Continue leaves for the trigger
// page at …/intake/new/$source?on=<event key>; closing the picker returns to the Intake page.
export const Route = createFileRoute('/o/$org/f/$factory/intake/new')({
  head: ({ match: { context } }) => pageTitle('New trigger', context.factory.name, context.org.name),
  component: NewTriggerRoute,
});

function NewTriggerRoute() {
  return (
    <>
      <IntakePage group="software" onGroupChange={() => {}} />
      <NewTriggerDialog />
    </>
  );
}
