// Form | YAML (screens build/definition): a segmented control of the two views of the draft. The
// entity stays across the views; from YAML, Form opens the entity at the cursor's line (`?line=`,
// kept by the YAML view), or the agent whose prompt is open. YAML is Later while no YAML view is
// registered. The shared SegmentedControl (a radio group with a sliding pill); a choice navigates like a link would.
import { useNavigate, useSearch } from '@tanstack/react-router';
import { laterText } from '../../../app/screens';
import { placeAtLine } from '../../../shared/definition/place';
import { useFactoryParams } from '../../../shared/factory';
import { SegmentedControl, type SegmentedOption } from '../../../ui/segmented-control';
import { useDraft, useModel } from '../draft/use-draft';
import { yamlView } from '../section-views';
import type { EditorSection } from '../sections';
import { validateDefinitionSearch } from '../sections';

const segment =
  'inline-flex h-7 items-center rounded-chip px-3 text-secondary font-control transition-colors duration-(--duration-quick)';

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

  const options: SegmentedOption<View>[] = [
    { value: 'form', label: 'Form' },
    { value: 'yaml', label: 'YAML', ...(yamlView ? {} : { disabledReason: laterText }) },
  ];
  return (
    <SegmentedControl
      label="Editor view"
      options={options}
      value={showYaml ? 'yaml' : 'form'}
      onChange={(v) => void (v === 'yaml' ? toYaml() : toForm())}
    />
  );
}
