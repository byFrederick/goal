// Work types (D §10.5): a token field whose values are slugs, as in bug-fix.
import type { BasicsSection } from '../../../shared/definition/model';
import { addWorkType, removeWorkType } from '../draft/ops';
import { useDraftSession } from '../draft/use-draft';
import { EditedMark } from '../frame/section-shell';
import { useProblemText } from '../frame/use-editor';
import { TokenField } from './token-field';

const slugRule = /^[a-z0-9]+(-[a-z0-9]+)*$/;

export function WorkTypes({ basics }: { basics: BasicsSection }) {
  const session = useDraftSession();
  return (
    <TokenField
      label="Work types"
      aside={<EditedMark section="basics" pointer="/basics/work_types" label="work types" />}
      noun="work type"
      values={basics.work_types ?? []}
      placeholder="Add a type…"
      check={(type) =>
        slugRule.test(type) ? undefined : 'A work type is lowercase letters, digits and dashes, as in bug-fix.'
      }
      problem={useProblemText('/basics/work_types')}
      help="Items get one when they arrive. Lines can route on them."
      onAdd={(type) => session.edit(addWorkType(basics, type))}
      onRemove={(type) => session.edit(removeWorkType(basics, type))}
    />
  );
}
