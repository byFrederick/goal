import { createFileRoute, notFound, redirect } from '@tanstack/react-router';
import { pageTitle } from '../app/route-focus';
import { isWizardStep, validateWizardSearch, WizardPage } from '../features/factory-editor/wizard-page';

// The New factory wizard (D §10.1), on the rail with no sidebar. Without `?factory=`, every step
// but Start goes back to Start.
export const Route = createFileRoute('/o/$org/new/$step')({
  validateSearch: validateWizardSearch,
  beforeLoad: ({ params, search }) => {
    if (!isWizardStep(params.step)) throw notFound();
    if (params.step !== 'start' && !search.factory) {
      throw redirect({ to: '/o/$org/new/$step', params: { ...params, step: 'start' }, replace: true });
    }
  },
  head: ({ match: { context } }) => pageTitle('New factory', context.org.name),
  component: WizardRoute,
});

function WizardRoute() {
  const { step } = Route.useParams();
  const search = Route.useSearch();
  return isWizardStep(step) ? <WizardPage step={step} search={search} /> : null;
}
