// Form | YAML (screens build/definition): a segmented control of the two views of the draft. The
// entity stays across the views; from YAML, Form opens the entity at the cursor's line (`?line=`,
// kept by the YAML view), or the agent whose prompt is open. YAML is Later while no YAML view is
// registered. Each segment is a button with aria-pressed; it navigates like a link would.
import { useNavigate, useSearch } from '@tanstack/react-router';
import { laterText } from '../../../app/screens';
import { placeAtLine } from '../../../shared/definition/place';
import { useFactoryParams } from '../../../shared/factory';
import { cn } from '../../../ui/cn';
import { useDraft, useModel } from '../draft/use-draft';
import { yamlView } from '../section-views';
import type { EditorSection } from '../sections';
import { validateDefinitionSearch } from '../sections';

const segment =
  'inline-flex h-7 items-center rounded-chip px-3 text-secondary font-control transition-colors duration-200';

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
    <fieldset
      aria-label="Editor view"
      className="m-0 inline-flex gap-1 rounded-control border border-line bg-panel p-1"
    >
      <button
        type="button"
        aria-pressed={!showYaml}
        onClick={() => void toForm()}
        className={cn(segment, !showYaml ? 'bg-card text-ink shadow-raised' : 'text-ink-soft hover:text-ink')}
      >
        Form
      </button>
      {yamlView ? (
        <button
          type="button"
          aria-pressed={showYaml}
          onClick={() => void toYaml()}
          className={cn(segment, showYaml ? 'bg-card text-ink shadow-raised' : 'text-ink-soft hover:text-ink')}
        >
          YAML
        </button>
      ) : (
        <span aria-disabled="true" className={cn(segment, 'cursor-default text-muted')}>
          YAML<span className="sr-only">, {laterText}</span>
        </span>
      )}
    </fieldset>
  );
}
