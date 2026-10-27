// Definition (D §10.1): /o/$org/f/$factory/definition/$section. The layout opens the factory's
// draft session and draws the frame once; each section route renders its view from
// `section-views.tsx` inside it (or LaterSection), with a skeleton until the draft (or the
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
      <div
        role="alert"
        className="flex max-w-xl flex-col gap-3 rounded-card border border-rework-tint bg-rework-tint p-5"
      >
        <h2 className="m-0 text-xl font-medium text-rework-ink">The draft could not load</h2>
        <p className="m-0 text-[15px] leading-relaxed">
          {failed instanceof ApiError ? (failed.detail ?? failed.title) : 'goald is not answering.'}
        </p>
        <div>
          <Button
            variant="primary"
            onClick={() => void client.invalidateQueries({ queryKey: defKeys.draft(session.org, session.factory) })}
          >
            Try again
          </Button>
        </div>
      </div>
    );
  }
  if (!ready) return <SectionSkeleton />;

  const View = (search.mode === 'yaml' ? yamlView : null) ?? sectionViews[section];
  if (parseError && View !== yamlView && section !== 'versions') {
    return <ParseErrorState line={parseError.line} />;
  }
  if (session.readOnly && section === 'review' && View !== yamlView) return <ViewOnlyReview />;
  return View ? (
    <ReadOnly when={session.readOnly}>
      <Suspense fallback={<SectionSkeleton />}>
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
      <h2 data-route-focus="" tabIndex={-1} className="m-0 text-xl font-medium outline-none">
        Review and publish
      </h2>
      <p className="m-0 text-[15px] leading-relaxed">{reasons.publish(name)}</p>
    </div>
  );
}

/** A section while the draft (or the section's code) loads. */
function SectionSkeleton() {
  return (
    <div aria-busy="true" className="flex flex-col gap-4 rounded-card border border-line bg-card p-5">
      <span className="sr-only">Loading the draft</span>
      <div className="h-6 w-40 rounded-chip bg-panel" />
      <div className="h-4 w-full max-w-[520px] rounded-chip bg-panel/70" />
      <div className="grid gap-4 @[640px]:grid-cols-2">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="h-16 rounded-control bg-panel/60" />
        ))}
      </div>
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
      <h2 data-route-focus="" tabIndex={-1} className="m-0 text-xl font-medium text-rework-ink outline-none">
        factory.yaml doesn’t parse
      </h2>
      <p className="m-0 text-[15px] leading-relaxed">
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
