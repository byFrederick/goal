// Knowledge (ADR 0038 decision 12): the factory's sources, factory memory and what agents see. Sources
// are the draft model's knowledge.sources; factory memory is memory.yaml, edited as a file through the
// draft session like an environment.
import { useQuery } from '@tanstack/react-query';
import { useMemo, useState } from 'react';
import { getDraftKnowledgePreview } from '../../../api/definitions';
import { knowledgeSourcesQuery } from '../../../api/queries';
import { useOrg } from '../../../shared/org';
import { setBasics } from '../draft/ops';
import { useDraft, useDraftSession, useSection, useViewOnly } from '../draft/use-draft';
import { useFileText } from '../environments/use-environments';
import { SectionShell } from '../frame/section-shell';
import { MemoryCard, PreviewCard, SourcesCard } from './knowledge-cards';
import { memoryPath, readMemory, writeMemory } from './memory-file';
import { addSource, entryOf, removeSource } from './source-entries';

export function KnowledgeSection() {
  const org = useOrg();
  const session = useDraftSession();
  const viewOnly = useViewOnly();
  const basics = useSection('basics');
  const agents = useSection('agents').agents ?? [];
  const hasDraft = useDraft((s) => s.hasDraft);
  const known = useQuery(knowledgeSourcesQuery(org.slug)).data ?? [];

  const raw = basics.knowledge_sources ?? [];
  const entries = useMemo(() => raw.map(entryOf), [raw]);
  const setSources = (next: typeof raw) => {
    session.edit(setBasics(basics, 'knowledge_sources', next.length === 0 ? undefined : next));
    void session.flush();
  };

  const { text } = useFileText(memoryPath);
  const memory = useMemo(() => readMemory(text), [text]);
  const bytes = new TextEncoder().encode(text ?? '').length;

  const names = agents.map((a) => a.name);
  const [chosen, setChosen] = useState('');
  const agent = names.includes(chosen) ? chosen : (names[0] ?? '');
  const key = JSON.stringify([raw, text]);
  const preview = useQuery({
    queryKey: ['org', org.slug, 'factory', session.factory, 'draft', 'knowledge-preview', agent, key],
    queryFn: async ({ signal }) => {
      await session.flush();
      return getDraftKnowledgePreview(session.org, session.factory, agent, signal);
    },
    enabled: hasDraft && agent !== '',
    retry: false,
  });

  return (
    <SectionShell section="knowledge">
      <div className="flex min-w-0 flex-col gap-4">
        <SourcesCard
          entries={entries}
          known={known}
          readOnly={viewOnly}
          onAdd={(name) => setSources(addSource(raw, name))}
          onRemove={(name) => setSources(removeSource(raw, name))}
        />
        <MemoryCard
          entries={memory}
          readOnly={viewOnly}
          bytes={bytes}
          onChange={(next) => session.editFile(memoryPath, writeMemory(next))}
        />
        <PreviewCard
          agents={names}
          agent={agent}
          onAgent={setChosen}
          section={preview.data}
          loading={hasDraft && agent !== '' && preview.isPending}
          failed={preview.isError}
        />
      </div>
    </SectionShell>
  );
}
