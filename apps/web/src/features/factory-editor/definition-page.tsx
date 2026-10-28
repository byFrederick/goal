// Definition (D §10.1): /o/$org/f/$factory/definition/$section. The layout opens the factory's
// draft session and draws the frame once; each section route renders its view from
// `section-views.tsx` inside it (or LaterSection), with the app background until the draft (or the
// running version) has loaded and an error with Try again when it can't. Without
// definition:edit the whole editor is view only (canvas Define-View): the session changes
// nothing, the controls only show their values, and Review and publish says who publishes.
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useParams, useSearch } from '@tanstack/react-router';
import { type ReactNode, Suspense } from 'react';
import { ApiError } from '../../api/client';
import { defKeys, draftModelQuery } from '../../api/queries';
import { useFactory } from '../../shared/factory';
import { useOrg } from '../../shared/org';
import { holds, reasons } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { ReadOnly } from '../../ui/read-only';
import { DraftSessionProvider, useDraft, useDraftOpen, useDraftSession, useSection } from './draft/use-draft';
import { DraftFailed, DraftLoading } from './frame/draft-states';
import { DefinitionFrame } from './frame/editor-frame';
import { LaterSection } from './frame/section-shell';
import { type SectionViewProps, sectionViews, yamlView } from './section-views';
import { type DefinitionSearch, type EditorSection, isEditorSection } from './sections';

export function DefinitionLayout({ children }: { children: ReactNode }) {
  const org = useOrg();
  const factory = useFactory();
  const { section } = useParams({ strict: false });
  const { mode } = useSearch({ strict: false });
  const current: EditorSection = section && isEditorSection(section) ? section : 'line';
  const readOnly = !holds(factory, 'definition:edit');
  return (
    <DraftSessionProvider key={`${factory.id}:${readOnly}`} org={org.id} factory={factory.id} readOnly={readOnly}>
      <DefinitionFrame section={current} yaml={mode === 'yaml'}>
        {children}
      </DefinitionFrame>
    </DraftSessionProvider>
  );
}

/** No draft and a factory.yaml that doesn't parse are states the session shows, not failures. */
const isDraftState = (error: unknown) =>
  error instanceof ApiError && (error.code === 'no-draft' || error.code === 'yaml-invalid');

/** One section of the draft, in Definition or the wizard: its view once the draft has loaded. */
export function SectionView({
  section,
  search,
  place,
}: {
  section: EditorSection;
  search: DefinitionSearch;
  place: SectionViewProps['place'];
}) {
  const session = useDraftSession();
  const client = useQueryClient();
  const ready = useDraft((s) => s.ready);
  const parseError = useDraft((s) => s.parseError);
  const open = useDraftOpen(session);
  const model = useQuery({ ...draftModelQuery(session.org, session.factory), enabled: open === true });
  const failed = model.error && !isDraftState(model.error) ? model.error : null;

  if (!ready && failed) {
    return (
      <DraftFailed
        detail={failed instanceof ApiError ? (failed.detail ?? failed.title) : 'goald is not answering.'}
        onRetry={() => void client.invalidateQueries({ queryKey: defKeys.draft(session.org, session.factory) })}
      />
    );
  }
  if (!ready) return <DraftLoading />;

  const View = (search.mode === 'yaml' ? yamlView : null) ?? sectionViews[section];
  if (parseError && View !== yamlView && section !== 'versions') {
    return <ParseErrorState line={parseError.line} />;
  }
  if (session.readOnly && section === 'review' && View !== yamlView) return <ViewOnlyReview />;
  return View ? (
    <ReadOnly when={session.readOnly}>
      <Suspense fallback={<DraftLoading />}>
        <View place={place} search={search} />
      </Suspense>
    </ReadOnly>
  ) : (
    <LaterSection section={section} />
  );
}

/** Review and publish, for someone who may not publish: who does. */
function ViewOnlyReview() {
  const name = useSection('basics').name ?? 'this factory';
  return (
    <div className="flex max-w-xl flex-col gap-3 rounded-card border border-line bg-card p-5">
      <h2 data-route-focus="" tabIndex={-1} className="m-0 text-heading font-control outline-none">
        Review and publish
      </h2>
      <p className="m-0 text-body leading-relaxed">{reasons.publish(name)}</p>
    </div>
  );
}

/** Visual while factory.yaml does not parse (D §10.9): it opens again once the YAML parses. */
function ParseErrorState({ line }: { line: number }) {
  return (
    <div
      role="alert"
      className="flex max-w-xl flex-col gap-3 rounded-card border border-rework-tint bg-rework-tint p-5"
    >
      <h2 data-route-focus="" tabIndex={-1} className="m-0 text-heading font-control text-rework-ink outline-none">
        factory.yaml doesn’t parse
      </h2>
      <p className="m-0 text-body leading-relaxed">
        factory.yaml has a syntax error{line > 0 ? ` on line ${line}` : ''}. Visual opens again when it parses.
      </p>
      <div>
        <Button asChild variant="primary">
          <Link to="." search={{ mode: 'yaml', ...(line > 0 ? { line } : {}) }}>
            Open in YAML
          </Link>
        </Button>
      </div>
    </div>
  );
}
