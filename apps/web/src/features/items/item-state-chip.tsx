// The item page's state chip: the item's state in words beside its status icon, the same icon the
// board, the inbox and the Items list show for it.
import type { ItemState } from '../../api/types';
import type { StateLabel, Tone } from '../../shared/item-state';
import { itemStatus } from '../../shared/item-state';
import { Chip, type ChipTone } from '../../ui/chip';
import { StatusIcon } from '../../ui/status-icon';

const chipTone: Record<Tone, ChipTone> = { run: 'run', you: 'you', rework: 'rework', muted: 'plain', ok: 'ok' };

export function ItemStateChip({ state, label }: { state: ItemState; label: StateLabel }) {
  const status = itemStatus({ state });
  return (
    <Chip tone={chipTone[label.tone]}>
      {status ? <StatusIcon status={status} /> : null}
      {label.text}
    </Chip>
  );
}
