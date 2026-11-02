// Form | YAML (screens build/definition): a segmented control of the two views of the draft. The
// entity stays across the views; from YAML, Form opens the entity at the cursor's line (`?line=`,
// kept by the YAML view), or the agent whose prompt is open. YAML is Later while no YAML view is
// registered. A SegmentedControl: the pill slides to the chosen view, and choosing one navigates.
import { useNavigate, useSearch } from '@tanstack/react-router';
import { laterText } from '../../../app/screens';
import { placeAtLine } from '../../../shared/definition/place';
import { useFactoryParams } from '../../../shared/factory';
import { SegmentedControl } from '../../../ui/segmented-control';
import { useDraft, useModel } from '../draft/use-draft';
import { yamlView } from '../section-views';
import type { EditorSection } from '../sections';
import { validateDefinitionSearch } from '../sections';

export function ViewToggle({ section, yaml }: { section: EditorSection; yaml: boolean }) {
  const params = useFactoryParams();
  const navigate = useNavigate();
  const showYaml = yaml && yamlView !== null;
  const { mode: _mode, file, line, ...entity } = validateDefinitionSearch(useSearch({ strict: false }));
  const model = useModel();
  const ranges = useDraft((s) => s.ranges);
  const here =
    showYaml && (line !== undefined || file)
      ? placeAtLine(model, ranges, file ?? 'factory.yaml', line ?? 0)
      : undefined;
  const { section: formSection, ...formEntity } = here ?? { section, ...entity };
  const toForm = () =>
    navigate({
      to: '/o/$org/f/$factory/definition/$section',
      params: { ...params, section: formSection },
      search: (here ? formEntity : entity) as never,
    });
  const toYaml = () =>
    navigate({
      to: '/o/$org/f/$factory/definition/$section',
      params: { ...params, section },
      search: { ...entity, mode: 'yaml' } as never,
    });

  return (
    <SegmentedControl
      label="Editor view"
      value={showYaml ? 'yaml' : 'form'}
      options={[
        { value: 'form', label: 'Form' },
        { value: 'yaml', label: 'YAML', disabledReason: yamlView ? undefined : laterText },
      ]}
      onChange={(v) => void (v === 'yaml' ? toYaml() : toForm())}
    />
  );
}
