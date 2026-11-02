// The agent's prompt file as the version that runs has it. The Overview shows its first lines and a link
// to the whole; the Prompt view shows all of it. The text is read only here: "Edit prompt" opens it in
// Definition › Agents, and "Reset to default" writes the running version's prompt into the draft.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { toast } from 'sonner';
import { versionFileQuery } from '../../api/queries';
import { useRunningFiles, useRunningModel } from '../../shared/definition/agent-cards';
import { cleanPath } from '../../shared/definition/place';
import { useFactory, useFactoryParams } from '../../shared/factory';
import { useOrg } from '../../shared/org';
import { holds } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { SectionCard } from '../../ui/card';
import { NotAvailable } from '../../ui/not-available';
import { Skeleton, SkeletonRows } from '../../ui/skeleton';
import { Tooltip } from '../../ui/tooltip';
import { resetBlockedReason, usePromptText } from '../factory-editor/agents/prompt-editor';
import { useDraftSession } from '../factory-editor/draft/use-draft';

/** The lines the Overview shows before "Show the full prompt". */
const previewLines = 6;

/** The prompt file's path in the version that runs, and its text once read. */
function usePrompt(agent: string) {
  const org = useOrg();
  const factory = useFactory();
  const running = useRunningModel();
  const declared = running.data?.agents.agents?.find((a) => a.name === agent)?.prompt?.path;
  const path = cleanPath(declared ?? '');
  const sha = useRunningFiles(path !== '')?.find((f) => f.path === path)?.sha256 ?? '';
  const text = useQuery({ ...versionFileQuery(org.id, factory.id, 'current', path, sha), enabled: sha !== '' });
  return { running, path, text };
}

/** The prompt's text, or what to show while it loads or cannot be read. */
function PromptText({
  agent,
  lines,
}: {
  agent: string;
  /** How many lines to show; all of them when absent. */
  lines?: number;
}) {
  const { running, path, text } = usePrompt(agent);
  if (running.isPending || (path !== '' && text.isPending)) {
    return (
      <Skeleton ready={false} placeholder={<SkeletonRows rows={lines ?? 4} />}>
        <span />
      </Skeleton>
    );
  }
  if (running.isError || text.isError) {
    const retry = running.isError ? () => void running.refetch() : () => void text.refetch();
    return (
      <div className="flex flex-wrap items-center gap-3">
        <p className="m-0 text-body text-rework-ink">The prompt can’t be read right now.</p>
        <Button variant="secondary" size="sm" onClick={retry}>
          Try again
        </Button>
      </div>
    );
  }
  if (path === '') return <p className="m-0 text-body text-muted">This agent has no prompt file.</p>;
  const body = text.data?.text ?? '';
  const shown = lines === undefined ? body : body.split('\n').slice(0, lines).join('\n');
  return <p className="m-0 whitespace-pre-line text-body">{shown}</p>;
}

/**
 * "Reset to default" and "Edit prompt" beside a prompt header. Reset writes the running version's prompt
 * into the draft (goald serves no template prompt); it is off, with its reason, when there is nothing to
 * restore, and not available without definition:edit.
 */
function PromptActions({ agent }: { agent: string }) {
  const params = useFactoryParams();
  const factory = useFactory();
  const session = useDraftSession();
  const { path, text, running } = usePromptText(agent);
  const canEdit = holds(factory, 'definition:edit');
  const reason = resetBlockedReason(text, running, !canEdit);
  const reset = async () => {
    if (!path || typeof running !== 'string') return;
    session.editFile(path, running);
    await session.flush();
    toast(`${agent}’s prompt is the running version’s again`);
  };
  return (
    <span className="flex items-center gap-3">
      {path ? (
        canEdit ? (
          <Tooltip label={reason ?? 'Restore the running version’s prompt'}>
            <Button
              variant="secondary"
              size="sm"
              aria-disabled={reason !== undefined || undefined}
              onClick={() => {
                if (reason === undefined) void reset();
              }}
            >
              Reset to default
            </Button>
          </Tooltip>
        ) : (
          <NotAvailable>
            <Button variant="secondary" size="sm">
              Reset to default
            </Button>
          </NotAvailable>
        )
      ) : null}
      <Link
        to="/o/$org/f/$factory/definition/$section"
        params={{ ...params, section: 'agents' }}
        search={{ agent }}
        className="font-control text-body text-run-ink hover:text-link"
      >
        Edit prompt
      </Link>
    </span>
  );
}

/** The Overview's prompt: its first lines in a panel, and a link to the Prompt view. */
export function PromptPreview({ agent }: { agent: string }) {
  const params = useFactoryParams();
  const { path } = usePrompt(agent);
  return (
    <section aria-label="Prompt" className="flex min-w-0 flex-col gap-2">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <h2 className="m-0 text-heading">Prompt</h2>
        {path ? <span className="font-mono text-meta text-muted">{path}</span> : null}
        <span className="ml-auto">
          <PromptActions agent={agent} />
        </span>
      </div>
      <div className="flex flex-col gap-3 rounded-cell border border-line bg-panel p-4">
        <PromptText agent={agent} lines={previewLines} />
        <Link
          to="/o/$org/f/$factory/agents/$agent"
          params={{ ...params, agent }}
          search={{ view: 'prompt' }}
          className="self-start font-control text-body text-run-ink hover:text-link"
        >
          Show the full prompt
        </Link>
      </div>
    </section>
  );
}

/** The Prompt view: the whole prompt file, as the version that runs has it. */
export function PromptFull({ agent }: { agent: string }) {
  const { path } = usePrompt(agent);
  return (
    <SectionCard title="Prompt" description={path || undefined} aside={<PromptActions agent={agent} />}>
      <PromptText agent={agent} />
    </SectionCard>
  );
}
