import { createFileRoute, notFound, redirect } from '@tanstack/react-router';
import { pageTitle } from '../app/route-focus';
import { WizardPage } from '../features/factory-editor/wizard-page';
import { isWizardStep, stepLabel } from '../features/factory-editor/wizard-progress';
import { validateWizardSearch } from '../features/factory-editor/wizard-search';

// The New factory wizard (D §10.1), beside the organization's sidebar. Without `?factory=`, every step
// but Start goes back to Start; Start with one resumes that draft factory.
export const Route = createFileRoute('/o/$org/new/$step')({
  validateSearch: validateWizardSearch,
  beforeLoad: ({ params, search }) => {
    if (!isWizardStep(params.step)) throw notFound();
    if (params.step !== 'start' && !search.factory) {
      throw redirect({ to: '/o/$org/new/$step', params: { ...params, step: 'start' }, replace: true });
    }
  },
  head: ({ match: { context, params } }) =>
    pageTitle(...(isWizardStep(params.step) ? [stepLabel(params.step)] : []), 'New factory', context.org.name),
  component: WizardRoute,
});

function WizardRoute() {
  const { step } = Route.useParams();
  const search = Route.useSearch();
  return isWizardStep(step) ? <WizardPage step={step} search={search} /> : null;
}
