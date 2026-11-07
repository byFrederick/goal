// The engine's model_key_missing rule (domain.Line.RefusalCode), read from the published version: the line is
// on secrets (it has environment files) and factory.yaml names no runtime.modelKey. Pure, so the Floor's header
// and its setup checklist say the same thing the engine does.
import { useQuery } from '@tanstack/react-query';
import { versionFileQuery, versionQuery } from '../../api/queries';
import { modelKeyOf } from '../factory-editor/basics/model-key';
import { environmentsDir } from '../factory-editor/environments/env-file';

/** Whether the line is on secrets: the version has an environment file, or factory.yaml names a model key. */
export function onSecrets(paths: readonly string[], factoryYaml: string | undefined): boolean {
  return paths.some((path) => path.startsWith(environmentsDir)) || modelKeyOf(factoryYaml) !== undefined;
}

/** True when the line has environment files and no runtime.modelKey: every item would be blocked. */
export function needsModelKey(paths: readonly string[], factoryYaml: string | undefined): boolean {
  return paths.some((path) => path.startsWith(environmentsDir)) && modelKeyOf(factoryYaml) === undefined;
}

/** The published version's file paths and factory.yaml text, with the state of the two reads. */
export function usePublishedKeyInput(org: string, factory: string) {
  const version = useQuery(versionQuery(org, factory, 'current'));
  const sha = version.data?.files.find((f) => f.path === 'factory.yaml')?.sha256 ?? '';
  const file = useQuery({ ...versionFileQuery(org, factory, 'current', 'factory.yaml', sha), enabled: sha !== '' });
  return {
    paths: version.data?.files.map((f) => f.path) ?? [],
    factoryYaml: file.data?.text,
    queries: sha === '' ? [version] : [version, file],
    ready: version.isSuccess && (sha === '' || file.isSuccess),
  };
}
