// The "Model preset" group on the Models box's heading (ADR 0022): Quality first and Cost first. Choosing one saves
// the draft's agents section through the draft session. The last saved choice goes into ?preset=; a save the server
// refuses shows its reason and puts the previous choice back in the draft.
import { useNavigate } from '@tanstack/react-router';
import { useEffect, useState } from 'react';
import type { ApiError } from '../../../api/client';
import type { AgentsSection } from '../../../shared/definition/model';
import { Button } from '../../../ui/button';
import { useDraft, useDraftSession, useSection } from '../draft/use-draft';
import { type PresetId, presetEdit, presets, pressedPreset } from '../start/model-presets';
import { useLineEditor } from './line-editor';

interface Pending {
  previous: AgentsSection;
  preset: PresetId;
  /** The session error before this choice, so an old error is not taken for this one's. */
  errorBefore: ApiError | null;
}

export function PresetControl() {
  const editor = useLineEditor();
  const session = useDraftSession();
  const agents = useSection('agents');
  const error = useDraft((s) => s.error);
  const status = useDraft((s) => s.status);
  const navigate = useNavigate();
  const [pending, setPending] = useState<Pending | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const pressed = pressedPreset(agents);

  useEffect(() => {
    if (!pending) return;
    if (error && error !== pending.errorBefore) {
      session.edit({ section: 'agents', value: pending.previous }, { undoable: false });
      setFailure(error.detail ?? error.title);
      setPending(null);
    } else if (status === 'saved') {
      setFailure(null);
      void navigate({ to: '.', search: (prev) => ({ ...prev, preset: pending.preset }), replace: true });
      setPending(null);
    }
  }, [pending, error, status, session, navigate]);

  const choose = (preset: PresetId) => {
    setPending({ previous: agents, preset, errorBefore: error });
    session.edit(presetEdit(agents, preset));
  };

  return (
    <div className="flex flex-col items-end gap-2">
      <fieldset className="m-0 flex min-w-0 gap-2 border-0 p-0">
        <legend className="sr-only">Model preset</legend>
        {presets.map((p) => (
          <Button
            key={p.id}
            type="button"
            variant="secondary"
            size="sm"
            aria-pressed={pressed === p.id}
            disabled={editor.viewOnly}
            onClick={() => choose(p.id)}
            className={pressed === p.id ? 'font-control shadow-raised' : undefined}
          >
            {p.label}
          </Button>
        ))}
      </fieldset>
      {failure ? (
        <p role="alert" className="m-0 text-sm text-rework-ink">
          The model preset wasn’t saved: {failure}.
        </p>
      ) : null}
    </div>
  );
}
