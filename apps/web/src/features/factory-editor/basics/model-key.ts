// The secret that gives every run its model (ADR 0037): factory.yaml's runtime.modelKey, a {{secret}}.
// The agent model the API serves does not carry it, so it is read from factory.yaml's text.
import { useMemo } from 'react';
import { parseDocument } from 'yaml';
import { secretRef } from '../environments/env-file';
import { useFileText } from '../environments/use-environments';

/** The secret named by runtime.modelKey in factory.yaml's text, if it has one. */
export function modelKeyOf(text: string | undefined): string | undefined {
  if (text === undefined) return undefined;
  const value = parseDocument(text).getIn(['spec', 'runtime', 'modelKey']);
  return typeof value === 'string' ? (secretRef(value) ?? undefined) : undefined;
}

export function useModelKey(): { secret: string | undefined; ready: boolean } {
  const { text, ready } = useFileText('factory.yaml');
  return { secret: useMemo(() => modelKeyOf(text), [text]), ready };
}
