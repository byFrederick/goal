// Form | YAML (screens build/definition): a segmented control of the two views of the draft. The
// entity stays across the views; from YAML, Form opens the entity at the cursor's line (`?line=`,
// kept by the YAML view), or the agent whose prompt is open. YAML is Later while no YAML view is
// registered. A SegmentedControl: the pill slides to the chosen view, and choosing one navigates.

import { useNavigate, useSearch } from '@tanstack/react-router';
import { useState } from 'react';
import { laterText } from '../../../app/screens';
import { placeAtLine } from '../../../shared/definition/place';
import { useFactoryParams } from '../../../shared/factory';
import { SegmentedControl } from '../../../ui/segmented-control';
import { useDraft, useModel } from '../draft/use-draft';
import { defaultEnvironment, envName, envPath } from '../environments/env-file';
import { yamlView } from '../section-views';
import type { EditorSection } from '../sections';
import { validateDefinitionSearch } from '../sections';

/** `cursorLine`: the YAML view's cursor, which Form opens at before `?line=` has caught up with it. */
export function ViewToggle({
  section,
  yaml,
  cursorLine,
}: {
  section: EditorSection;
  yaml: boolean;
  cursorLine?: number | undefined;
}) {
  const params = useFactoryParams();
  const navigate = useNavigate();
  const showYaml = yaml && yamlView !== null;
  // The pressed radio flips on the same frame; the URL's view catches up when the navigation commits.
  const [pending, setPending] = useState<'form' | 'yaml' | null>(null);
  const [seen, setSeen] = useState(showYaml);
  if (seen !== showYaml) {
    setSeen(showYaml);
    setPending(null);
  }
  const { mode: _mode, file, line: urlLine, ...entity } = validateDefinitionSearch(useSearch({ strict: false }));
  const line = cursorLine ?? urlLine;
  const model = useModel();
  const ranges = useDraft((s) => s.ranges);
  const here =
    showYaml && (line !== undefined || file)
      ? placeAtLine(model, ranges, file ?? 'factory.yaml', line ?? 0)
      : undefined;
  const { section: formSection, ...formEntity } = here ?? { section, ...entity };
  // The Environments page's tab is the YAML view's file, and back.
  const fileEnvironment = file ? envName(file) : undefined;
  const toForm = () =>
    navigate({
      to: '/o/$org/f/$factory/definition/$section',
      params: { ...params, section: formSection },
      search: (here ? formEntity : fileEnvironment ? { ...entity, environment: fileEnvironment } : entity) as never,
    });
  const toYaml = () =>
    navigate({
      to: '/o/$org/f/$factory/definition/$section',
      params: { ...params, section },
      search: {
        ...entity,
        mode: 'yaml',
        ...(section === 'environments' ? { file: envPath(entity.environment ?? defaultEnvironment) } : {}),
      } as never,
    });

  return (
    <SegmentedControl
      label="Editor view"
      value={pending ?? (showYaml ? 'yaml' : 'form')}
      options={[
        { value: 'form', label: 'Form' },
        { value: 'yaml', label: 'YAML', disabledReason: yamlView ? undefined : laterText },
      ]}
      onChange={(v) => {
        setPending(v === 'yaml' ? 'yaml' : 'form');
        void (v === 'yaml' ? toYaml() : toForm());
      }}
    />
  );
}
