import { createFileRoute, notFound } from '@tanstack/react-router';
import { pageTitle } from '../app/route-focus';
import { SectionView } from '../features/factory-editor/definition-page';
import { isEditorSection, sectionInfo, validateDefinitionSearch } from '../features/factory-editor/sections';

// One section of the Definition: basics, line, agents, connections, review or versions, with the
// entity it shows in the search params (?station=, ?agent=, ?connection=, ?mode=yaml, …).
export const Route = createFileRoute('/o/$org/f/$factory/definition/$section')({
  validateSearch: validateDefinitionSearch,
  beforeLoad: ({ params }) => {
    if (!isEditorSection(params.section)) throw notFound();
  },
  head: ({ match: { context, params } }) =>
    pageTitle(
      isEditorSection(params.section) ? sectionInfo[params.section].label : 'Definition',
      'Definition',
      context.factory.name,
      context.org.name,
    ),
  component: SectionRoute,
});

function SectionRoute() {
  const { section } = Route.useParams();
  const search = Route.useSearch();
  return isEditorSection(section) ? <SectionView place="definition" section={section} search={search} /> : null;
}
