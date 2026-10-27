// The Publish card (ADR 0016, build/definition-publish): "What changed, in one line" (prefilled
// from the draft's change summaries), "Where do these items go?" when the new version removes
// stations open items wait at, "Start the line right away" on a first publish, and Publish vN,
// disabled with its reason while problems remain. The wizard also shows the counts it creates
// (Stations, Agents, Connections, Skills). Publishing flushes the session, sends the draft's
// revision and the version it saw running, and on success closes the draft.
import { useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { useEffect, useId, useState } from 'react';
import { toast } from 'sonner';
import { ApiError } from '../../../api/client';
import { publishDraft } from '../../../api/definitions';
import { defKeys, keys } from '../../../api/queries';
import type { ItemsAtRemovedStations, Validation } from '../../../api/types';
import { useOrg } from '../../../shared/org';
import { Button } from '../../../ui/button';
import { SectionCard, Stat, StatGrid } from '../../../ui/card';
import { Field, Input, NativeSelect } from '../../../ui/field';
import { useDraftSession, useModel } from '../draft/use-draft';
import { SearchLink } from '../search-link';
import { blockedSentence, defaultMessage, type MoveNeed, mergeMoveNeeds, publishExplainer } from './review';
import { useComparison, useEditorFacts } from './use-review';

interface PublishCardProps {
  place: 'definition' | 'wizard';
  validation: Validation | undefined;
  /** The moves chosen so far (validated with the draft by the section). */
  moves: Record<string, string>;
  onMoves: (moves: Record<string, string>) => void;
  /** The moves B's items check asks for. */
  needs: MoveNeed[];
}

/** A refused publish, in the card's words. */
interface Refusal {
  message: string;
  /** "Review the differences" after a version was published meanwhile. */
  differences?: boolean;
  items?: ItemsAtRemovedStations;
}

export function PublishCard({ place, validation, moves, onMoves, needs }: PublishCardProps) {
  const facts = useEditorFacts();
  const { org, factory, hasDraft, current, next, detail } = facts;
  const session = useDraftSession();
  const client = useQueryClient();
  const navigate = useNavigate();
  const orgCtx = useOrg();
  const model = useModel();
  const changes = useComparison(hasDraft ? { from: 'base', to: 'draft' } : undefined).data?.changes ?? [];
  const first = current === null;
  const [message, setMessage] = useState<string | null>(null);
  const [start, setStart] = useState(false);
  const [pending, setPending] = useState(false);
  const [refusal, setRefusal] = useState<Refusal | null>(null);
  const [rows, setRows] = useState<MoveNeed[]>([]);
  const reason = useId();

  // Rows for the stations B asks about, and those a refused publish named; a row stays once mapped.
  const lineStations = (model.line.stations ?? []).map((s) => s.id);
  const refusedRows: MoveNeed[] = [...new Set((refusal?.items?.unmapped ?? []).map((i) => i.station))].map(
    (station) => ({ station, options: lineStations }),
  );
  const fresh = [...needs, ...refusedRows];
  const freshKey = fresh.map((n) => `${n.station}:${n.options.join(',')}`).join('|');
  // biome-ignore lint/correctness/useExhaustiveDependencies: merged when the asked-for rows change
  useEffect(() => {
    if (fresh.length > 0) setRows((known) => mergeMoveNeeds(known, fresh));
  }, [freshKey]);

  const counts = validation?.counts;
  const errors = validation?.problems.filter((p) => p.severity === 'error').length ?? 0;
  const unmapped = rows.filter((r) => !moves[r.station]).length;
  const note = message ?? defaultMessage(changes);
  const blocked = !hasDraft || !validation || errors > 0 || unmapped > 0;
  const why = !hasDraft
    ? null
    : !validation
      ? 'Checking the draft…'
      : errors > 0
        ? blockedSentence(errors)
        : unmapped > 0
          ? 'Choose where the open items go first. Your draft stays saved.'
          : null;

  const publish = async () => {
    if (blocked || pending) return;
    setPending(true);
    setRefusal(null);
    try {
      await session.flush();
      if (session.hasUnsaved()) {
        setRefusal({ message: 'Your last changes aren’t saved yet. Publish once the status line says Autosaved.' });
        return;
      }
      const revision = session.getSnapshot().revision;
      const version = await publishDraft(org, factory, {
        revision,
        expected_current_version_id: current?.id ?? null,
        ...(note.trim() ? { message: note.trim().slice(0, 200) } : {}),
        ...(Object.keys(moves).length > 0 ? { station_moves: moves } : {}),
        ...(first ? { start } : {}),
      });
      session.closeDraft();
      onMoves({});
      setRows([]);
      setMessage(null);
      toast(`${detail?.name ?? 'The factory'} v${version.number} published`);
      const refetched = Promise.all(
        [
          defKeys.draft(org, factory),
          defKeys.versions(org, factory),
          defKeys.versionsOf(org, factory),
          defKeys.factory(org, factory),
          defKeys.factories(org),
          defKeys.compares(org, factory),
          keys.board(org, factory),
        ].map((queryKey) => client.invalidateQueries({ queryKey })),
      );
      if (place === 'wizard') {
        // The Floor reads the factory's stations from the list: it opens once the list has them.
        await refetched.catch(() => undefined);
        void navigate({ to: '/o/$org/f/$factory', params: { org: orgCtx.slug, factory }, ignoreBlocker: true });
      }
    } catch (err) {
      if (!(err instanceof ApiError)) {
        setRefusal({ message: 'goald can’t be reached. Your draft stays saved; try again.' });
      } else if (err.code === 'draft-base-moved') {
        setRefusal({
          message: 'Another version was published or restored while you reviewed.',
          differences: true,
        });
        void client.invalidateQueries({ queryKey: defKeys.factory(org, factory) });
      } else if (err.code === 'items-at-removed-station') {
        setRefusal({
          message: 'Open items are at stations this version removes.',
          ...(err.problem.items ? { items: err.problem.items as ItemsAtRemovedStations } : {}),
        });
      } else if (err.status === 412) {
        setRefusal({ message: 'The draft changed while you reviewed. Review it again, then publish.' });
        void client.invalidateQueries({ queryKey: defKeys.draft(org, factory) });
      } else {
        setRefusal({ message: `Not published: ${(err.detail ?? err.title).replace(/\.$/, '')}.` });
        void client.invalidateQueries({ queryKey: defKeys.draft(org, factory) });
      }
    } finally {
      setPending(false);
    }
  };

  if (!hasDraft) {
    return (
      <SectionCard title="Publish">
        <p className="m-0 text-body">
          {current ? `No draft. v${current.number} is running.` : 'Nothing to publish yet.'}
        </p>
        <p className="m-0 text-secondary text-muted">Your first change in any section opens a draft of v{next}.</p>
      </SectionCard>
    );
  }

  return (
    <SectionCard title="Publish">
      {place === 'wizard' ? (
        <StatGrid>
          <Stat label="Stations">
            <dd className="m-0 text-detail-title tabular-nums">{counts?.stations ?? '–'}</dd>
          </Stat>
          <Stat label="Agents">
            <dd className="m-0 text-detail-title tabular-nums">{counts?.agents ?? '–'}</dd>
          </Stat>
          <Stat label="Connections">
            <dd className="m-0 text-detail-title tabular-nums">{counts?.connections ?? '–'}</dd>
          </Stat>
          <Stat label="Skills">
            <dd className="m-0 text-detail-title tabular-nums">{counts?.skills ?? '–'}</dd>
          </Stat>
        </StatGrid>
      ) : null}
      <Field label="What changed, in one line">
        <Input value={note} maxLength={200} onChange={(e) => setMessage(e.target.value)} />
      </Field>

      {rows.length > 0 ? (
        <fieldset className="m-0 flex flex-col gap-3 border-0 p-0">
          <legend className="mb-1 text-body font-medium">Where do these items go?</legend>
          {rows.map((r) => (
            <Field key={r.station} label={`Items waiting at ${r.station}, which v${next} removes`}>
              <NativeSelect
                value={moves[r.station] ?? ''}
                onChange={(e) => onMoves({ ...moves, [r.station]: e.target.value })}
              >
                <option value="" disabled>
                  Choose a station…
                </option>
                {r.options.map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </NativeSelect>
            </Field>
          ))}
        </fieldset>
      ) : null}

      {first ? (
        <label className="flex cursor-pointer items-start gap-3 text-body leading-normal">
          <input
            type="checkbox"
            checked={start}
            onChange={(e) => setStart(e.target.checked)}
            className="mt-1 size-4 shrink-0 accent-ink"
          />
          Start the line right away. Otherwise it stays paused until you start it.
        </label>
      ) : null}

      <div className="flex flex-col gap-2">
        <Button
          variant="primary"
          size="lg"
          className="w-full justify-center"
          aria-disabled={blocked || pending || undefined}
          aria-describedby={why ? reason : undefined}
          onClick={() => void publish()}
        >
          {pending ? 'Publishing…' : `Publish v${next}`}
        </Button>
        {why ? (
          <p id={reason} className="m-0 text-body text-rework-ink">
            {why}
          </p>
        ) : null}
        {refusal ? (
          <div role="alert" className="flex flex-col gap-2 text-body text-rework-ink">
            <p className="m-0">
              {refusal.message}{' '}
              {refusal.differences ? (
                <SearchLink patch={{ compare: 'current..draft' }} className="underline underline-offset-3">
                  Review the differences
                </SearchLink>
              ) : null}
            </p>
            {(refusal.items?.in_progress ?? []).map((i) => (
              <p key={i.id} className="m-0">
                #{i.number} is being worked at {i.station}: let its run finish or stop it first.
              </p>
            ))}
          </div>
        ) : null}
        <p className="m-0 text-secondary leading-normal text-muted">
          {publishExplainer(current?.number ?? null, next)}
        </p>
      </div>
    </SectionCard>
  );
}
