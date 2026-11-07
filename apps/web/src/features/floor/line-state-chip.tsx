// A published line's state chip, the one place Home's row and the Floor's header decide it: a toneless
// placeholder while the published version's two reads are pending, then Needs a model key (the engine's
// model_key_missing rule, model-key-needed.ts) or Running. A failed read shows Running, so an error
// never hides the line's state.
import { useEffect } from 'react';
import { Chip } from '../../ui/chip';
import { Skeleton } from '../../ui/skeleton';
import { needsModelKey, usePublishedKeyInput } from './model-key-needed';

const slot = {
  md: 'h-5.5 w-24',
  lg: 'h-7 w-32',
} as const;

export function LineStateChip({
  org,
  factory,
  size,
  runningText,
}: {
  org: string;
  factory: string;
  size: 'md' | 'lg';
  runningText: 'Running' | 'Line running';
}) {
  const published = usePublishedKeyInput(org, factory);
  const failed = published.failed;
  useEffect(() => {
    if (failed) console.error('Could not read the published version to tell if the line needs a model key');
  }, [failed]);
  const ready = published.ready || failed;
  const missing = published.ready && needsModelKey(published.paths, published.factoryYaml);

  return (
    <Skeleton
      ready={ready}
      className="inline-grid w-fit"
      placeholder={<div className={`${slot[size]} rounded-chip bg-hover`} />}
    >
      {!ready ? null : missing ? (
        <Chip tone="you" size={size} dot>
          Needs a model key
        </Chip>
      ) : (
        <Chip tone="ok" size={size} dot>
          {runningText}
        </Chip>
      )}
    </Skeleton>
  );
}
