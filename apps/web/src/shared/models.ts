// The install's models as people read them: a model id's label among goald's models (builder
// meta), without a leading "Claude ". An id the install does not list, or any id while the models
// load, reads as the id itself.
import { useQuery } from '@tanstack/react-query';
import { builderMetaQuery } from '../api/queries';
import type { BuilderModel } from '../api/types';
import { useOrg } from './org';

export function modelLabel(models: readonly BuilderModel[], id: string): string {
  const model = models.find((m) => m.id === id);
  return model ? model.label.replace(/^Claude /, '') : id;
}

/** modelLabel over the current organization's models, loaded by the item screens. */
export function useModelLabel(): (id: string) => string {
  const models = useQuery(builderMetaQuery(useOrg().id)).data?.models ?? [];
  return (id) => modelLabel(models, id);
}
