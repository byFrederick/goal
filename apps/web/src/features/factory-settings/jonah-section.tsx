// Settings › Jonah (ADR 0039, docs/design/screens/build/factory-settings.jpg): learning.jonah, whether Jonah
// reviews the line (enabled), the model of its review, its share of the line's spend and the items alike
// an observation needs before it becomes a proposal (proposeAfter). The visual model carries no `learning`
// key, so the fields read and write factory.yaml in the draft as text, keeping its comments and other keys; Save
// changes publishes it with the rest. Each field has an ⓘ tooltip in place of a paragraph.
import { useQuery } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { parseDocument } from 'yaml';
import { builderMetaQuery } from '../../api/queries';
import { Field, Input } from '../../ui/field';
import { Select } from '../../ui/select';
import { Switch } from '../../ui/switch';
import { useDraft, useDraftSession, useViewOnly } from '../factory-editor/draft/use-draft';
import { factoryFile } from '../factory-editor/yaml/files';
import { InfoTip } from '../jonah/info-tip';
import { SettingsGroup } from './settings-group';

/** What learning.jonah holds in factory.yaml; the keys left out are goald's defaults. */
export interface JonahSettings {
  enabled: boolean;
  model: string;
  /** The percent of the line's spend, as the YAML writes it without the sign. */
  share: string;
  proposeAfter: string;
}

const defaults: JonahSettings = { enabled: true, model: '', share: '5', proposeAfter: '3' };

/** learning.jonah from factory.yaml's text; a file that does not parse reads as the defaults. */
export function readJonah(text: string): JonahSettings {
  const doc = parseDocument(text);
  if (doc.errors.length > 0) return defaults;
  const get = (key: string): unknown => doc.getIn(['learning', 'jonah', key]);
  const share = get('share');
  const after = get('proposeAfter');
  return {
    enabled: get('enabled') !== false,
    model: typeof get('model') === 'string' ? (get('model') as string) : '',
    share: typeof share === 'string' ? share.replace(/%$/, '') : defaults.share,
    proposeAfter: typeof after === 'number' ? String(after) : defaults.proposeAfter,
  };
}

/** The text with one learning.jonah key set (or removed with undefined); everything else stays as written. */
export function writeJonah(text: string, key: string, value: boolean | number | string | undefined): string {
  const doc = parseDocument(text);
  if (doc.errors.length > 0) return text;
  if (value === undefined) {
    doc.deleteIn(['learning', 'jonah', key]);
  } else {
    doc.setIn(['learning', 'jonah', key], value);
  }
  return String(doc);
}

/** The share as a number from 0 to 100, or null while the text is not one. */
const percent = (text: string): number | null => {
  const n = Number(text);
  return text.trim() !== '' && Number.isFinite(n) && n >= 0 && n <= 100 ? n : null;
};
const count = (text: string): number | null => {
  const n = Number(text);
  return text.trim() !== '' && Number.isInteger(n) && n >= 1 && n <= 50 ? n : null;
};

export function JonahSection() {
  const session = useDraftSession();
  const viewOnly = useViewOnly();
  const text = useDraft((s) => s.files.get(factoryFile)?.text);
  const models = useQuery(builderMetaQuery(session.org)).data?.models ?? [];
  // factory.yaml joins the session's files on first read; a factory with no version has none to read.
  const [loaded, setLoaded] = useState(false);
  const ready = useDraft((s) => s.ready);
  useEffect(() => {
    if (!ready) return;
    let live = true;
    session
      .fileText(factoryFile)
      .catch(() => '')
      .then(() => live && setLoaded(true));
    return () => {
      live = false;
    };
  }, [session, ready]);

  const current = readJonah(text ?? '');
  const [typed, setTyped] = useState<{ share?: string; proposeAfter?: string }>({});
  const share = typed.share ?? current.share;
  const proposeAfter = typed.proposeAfter ?? current.proposeAfter;
  const write = (key: string, value: boolean | number | string | undefined, coalesce = false) => {
    if (viewOnly || !loaded) return;
    const next = writeJonah(text ?? '', key, value);
    if (next !== (text ?? '')) {
      session.editFile(factoryFile, next);
      if (!coalesce) void session.flush();
    }
  };
  const flush = () => {
    setTyped({});
    void session.flush();
  };
  const modelOptions = [
    { value: '', label: 'Same as the line' },
    ...(current.model === '' || models.some((m) => m.id === current.model)
      ? []
      : [{ value: current.model, label: current.model }]),
    ...models.map((m) => ({ value: m.id, label: m.label })),
  ];

  return (
    <SettingsGroup id="jonah" title="Jonah">
      <div className="grid grid-cols-1 gap-x-5 gap-y-4 @[640px]:grid-cols-2">
        <div className="col-span-full flex min-h-11 items-center gap-3 md:min-h-8">
          <Switch
            id="jonah-enabled"
            aria-labelledby="jonah-enabled-label"
            checked={current.enabled}
            onCheckedChange={(on) => write('enabled', on)}
          />
          <span id="jonah-enabled-label" className="text-sm font-medium">
            Jonah reviews this line
          </span>
          <InfoTip label="About Jonah">
            Once a day Jonah reads what happened on the line and proposes changes for you to approve. Off, it reviews
            nothing.
          </InfoTip>
        </div>
        <Field
          label={
            <span className="inline-flex items-center gap-1">
              Model
              <InfoTip label="About Jonah’s model">
                The model of Jonah’s daily review. The line’s model when left empty.
              </InfoTip>
            </span>
          }
        >
          <Select
            options={modelOptions}
            value={current.model}
            onValueChange={(m) => write('model', m === '' ? undefined : m)}
          />
        </Field>
        <Field
          label="Budget, % of the line’s spend"
          aside={
            <InfoTip label="About Jonah’s budget">
              A share of what the line spent in the last 7 days, from 0 to 100. It never stops the line.
            </InfoTip>
          }
          error={percent(share) === null ? 'A number from 0 to 100.' : undefined}
        >
          <Input
            type="number"
            inputMode="decimal"
            min={0}
            max={100}
            step="any"
            value={share}
            onChange={(e) => {
              setTyped((t) => ({ ...t, share: e.target.value }));
              const n = percent(e.target.value);
              if (n !== null) write('share', `${n}%`, true);
            }}
            onBlur={flush}
          />
        </Field>
        <Field
          label="Proposes after, items"
          aside={
            <InfoTip label="About proposing">
              How many items with the same cause Jonah needs to see before it makes a proposal.
            </InfoTip>
          }
          error={count(proposeAfter) === null ? 'A whole number from 1 to 50.' : undefined}
        >
          <Input
            type="number"
            inputMode="numeric"
            min={1}
            max={50}
            step={1}
            value={proposeAfter}
            onChange={(e) => {
              setTyped((t) => ({ ...t, proposeAfter: e.target.value }));
              const n = count(e.target.value);
              if (n !== null) write('proposeAfter', n, true);
            }}
            onBlur={flush}
          />
        </Field>
      </div>
    </SettingsGroup>
  );
}
