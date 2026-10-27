// The agent's prompt file as the version that runs has it. The Overview shows its first lines and a link
// to the whole; the Prompt view shows all of it. Read only: Definition › Agents edits the file.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { versionFileQuery } from '../../api/queries';
import { useRunningFiles, useRunningModel } from '../../shared/definition/agent-cards';
import { cleanPath } from '../../shared/definition/place';
import { useFactory, useFactoryParams } from '../../shared/factory';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { SectionCard } from '../../ui/card';

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
      <p aria-busy="true" className="m-0">
        <span className="sr-only">Loading the prompt</span>
      </p>
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

/** The Overview's prompt: its first lines in a panel, and a link to the Prompt view. */
export function PromptPreview({ agent }: { agent: string }) {
  const params = useFactoryParams();
  const { path } = usePrompt(agent);
  return (
    <section aria-label="Prompt" className="flex min-w-0 flex-col gap-2">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <h2 className="m-0 text-heading">Prompt</h2>
        {path ? <span className="font-mono text-meta text-muted">{path}</span> : null}
      </div>
      <div className="flex flex-col gap-3 rounded-cell border border-line bg-panel p-4">
        <PromptText agent={agent} lines={previewLines} />
        <Link
          to="/o/$org/f/$factory/agents/$agent"
          params={{ ...params, agent }}
          search={{ view: 'prompt' }}
          className="self-start font-control text-body text-run-ink hover:text-run"
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
    <SectionCard title="Prompt" description={path || undefined}>
      <PromptText agent={agent} />
    </SectionCard>
  );
}
