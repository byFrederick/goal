import { createFileRoute } from '@tanstack/react-router';
import { pageTitle } from '../app/route-focus';
import { IntakePage } from '../features/intake/intake-page';
import { type SuggestionGroup, suggestionGroups } from '../features/intake/suggestions';

// The Intake page (intake): sources, triggers and suggested triggers. Its states are the page's own: the
// route does not wait for the data, so loading, error and empty show on the page. The suggestion tab is in
// the URL (?group=), so a link opens the same tab.
export const Route = createFileRoute('/o/$org/f/$factory/intake/')({
  validateSearch: (search: Record<string, unknown>): { group?: SuggestionGroup | undefined } => ({
    group: suggestionGroups.find((g) => g === search.group),
  }),
  head: ({ match: { context } }) => pageTitle('Intake', context.factory.name, context.org.name),
  component: IntakeRoute,
});

function IntakeRoute() {
  const { group = 'software' } = Route.useSearch();
  const navigate = Route.useNavigate();
  return (
    <IntakePage
      group={group}
      onGroupChange={(next) => navigate({ search: (prev) => ({ ...prev, group: next }), replace: true, resetScroll: false })}
    />
  );
}
