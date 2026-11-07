// The First item step's hold (ADR 0037 decision 7). Agents cannot run without a model key, and the engine blocks an
// item whose line has none. So the step reads the published factory.yaml (the version model does not carry the key)
// and, while runtime.modelKey is missing, shows the model key card instead of submitting the starter item. Publish
// and start writes the key into a draft, publishes it over the current version, and then lets the step go on.
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { type ReactNode, useState } from 'react';
import { ApiError } from '../../api/client';
import { publishDraft } from '../../api/definitions';
import { defKeys, keys, versionFileQuery, versionQuery } from '../../api/queries';
import type { OrgSummary } from '../../api/types';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Skeleton } from '../../ui/skeleton';
import { modelKeyOf } from '../factory-editor/basics/model-key';
import { DraftSessionProvider, useDraftSession } from '../factory-editor/draft/use-draft';
import { ModelKeyCard, useModelKeySecret } from './model-key-card';
import { OnboardingLayout, type PreviewSlots } from './onboarding-layout';

interface GateProps {
  org: OrgSummary;
  factoryId: string;
  /** The id of the version that runs now: the publish must still find it. */
  currentVersionId: string | null;
  back: ReactNode;
  preview: PreviewSlots;
  /** The step once the factory has a model key. */
  children: ReactNode;
}

const heading = <h1 className="m-0 text-kpi tracking-display">Watch your first item</h1>;

export function ModelKeyGate({ org, factoryId, currentVersionId, back, preview, children }: GateProps) {
  const version = useQuery(versionQuery(org.id, factoryId, 'current'));
  const sha = version.data?.files.find((f) => f.path === 'factory.yaml')?.sha256 ?? '';
  const file = useQuery({
    ...versionFileQuery(org.id, factoryId, 'current', 'factory.yaml', sha),
    enabled: sha !== '',
  });
  const [published, setPublished] = useState(false);

  if (published) return <>{children}</>;

  const failed = version.isError || file.isError;
  const waiting = !failed && (version.isPending || (sha !== '' && file.isPending));
  if (waiting || failed) {
    return (
      <OnboardingLayout step="first-item" org={org} back={back} preview={preview}>
        <div className="flex flex-col gap-2">{heading}</div>
        {failed ? (
          <Callout tone="rework">
            <span>The factory can’t be read.</span>
            <Button
              type="button"
              size="sm"
              variant="secondary"
              className="mt-2"
              onClick={() => {
                void version.refetch();
                void file.refetch();
              }}
            >
              Retry
            </Button>
          </Callout>
        ) : (
          <Skeleton
            ready={false}
            placeholder={
              <div className="flex flex-col gap-3">
                <div className="h-14 rounded-card bg-hover" />
                <div className="h-48 rounded-card bg-hover" />
              </div>
            }
          >
            {null}
          </Skeleton>
        )}
      </OnboardingLayout>
    );
  }

  if (modelKeyOf(file.data?.text) !== undefined) return <>{children}</>;

  return (
    <DraftSessionProvider org={org.id} factory={factoryId}>
      <Hold
        org={org}
        factoryId={factoryId}
        currentVersionId={currentVersionId}
        back={back}
        preview={preview}
        onPublished={() => setPublished(true)}
      />
    </DraftSessionProvider>
  );
}

function Hold({
  org,
  factoryId,
  currentVersionId,
  back,
  preview,
  onPublished,
}: Omit<GateProps, 'children'> & { onPublished: () => void }) {
  const session = useDraftSession();
  const client = useQueryClient();
  const { secret, ready } = useModelKeySecret();
  const [pending, setPending] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);

  const publish = async () => {
    if (pending) return;
    setPending(true);
    setProblem(null);
    try {
      await session.flush();
      if (session.hasUnsaved()) {
        setProblem('Your last changes aren’t saved yet. Try again in a moment.');
        return;
      }
      await publishDraft(session.org, factoryId, {
        revision: session.getSnapshot().revision,
        expected_current_version_id: currentVersionId,
      });
      session.closeDraft();
      await Promise.all(
        [
          defKeys.draft(session.org, factoryId),
          defKeys.versions(session.org, factoryId),
          defKeys.versionsOf(session.org, factoryId),
          defKeys.factory(session.org, factoryId),
          defKeys.factories(session.org),
          defKeys.compares(session.org, factoryId),
          defKeys.version(session.org, factoryId, 'current'),
          keys.board(session.org, factoryId),
        ].map((queryKey) => client.invalidateQueries({ queryKey })),
      );
      onPublished();
    } catch (err) {
      if (err instanceof ApiError && (err.status === 412 || err.code === 'draft-base-moved')) {
        void client.invalidateQueries({ queryKey: defKeys.draft(session.org, factoryId) });
        setProblem('The factory changed while you were here. Reload the step and try again.');
      } else if (err instanceof ApiError) {
        setProblem(`The key isn’t published: ${(err.detail ?? err.title).replace(/\.$/, '')}. Try again.`);
      } else {
        setProblem('goald can’t be reached. Your key stays saved; try again.');
      }
    } finally {
      setPending(false);
    }
  };

  return (
    <OnboardingLayout
      step="first-item"
      org={org}
      back={back}
      preview={preview}
      action={
        <Button
          type="button"
          size="lg"
          variant="primary"
          busy={pending}
          busyLabel="Publishing…"
          disabled={!ready || secret === undefined}
          onClick={() => void publish()}
        >
          Publish and start
        </Button>
      }
    >
      <div className="flex flex-col gap-2">{heading}</div>
      <div className="animate-fade-in motion-reduce:animate-none">
        <Callout tone="note">Agents need a model key to run. Add one here, then your first item starts.</Callout>
      </div>
      {problem ? (
        <div role="alert" className="animate-fade-in motion-reduce:animate-none">
          <Callout tone="rework">{problem}</Callout>
        </div>
      ) : null}
      <div className="animate-fade-in motion-reduce:animate-none">
        <ModelKeyCard org={org.id} factory={factoryId} />
      </div>
    </OnboardingLayout>
  );
}
