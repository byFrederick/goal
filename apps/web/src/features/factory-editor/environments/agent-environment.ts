// Which environment file each agent gets (ADR 0037 decision 5): `environment` on the agent in
// factory.yaml's spec.agents, `default` when it is omitted. The agent model the API serves does not
// carry the field, so the agent page reads and writes factory.yaml's text, comments and order intact.
import { isMap, isSeq, parseDocument } from 'yaml';
import { defaultEnvironment } from './env-file';

/** The environment of each agent of factory.yaml's text, by agent name. */
export function agentEnvironments(text: string | undefined): Record<string, string> {
  if (text === undefined) return {};
  const out: Record<string, string> = {};
  const agents = parseDocument(text).getIn(['spec', 'agents']);
  if (!isSeq(agents)) return out;
  for (const item of agents.items) {
    if (!isMap(item)) continue;
    const name = item.get('name');
    const env = item.get('environment');
    if (typeof name === 'string') out[name] = typeof env === 'string' && env !== '' ? env : defaultEnvironment;
  }
  return out;
}

/** factory.yaml's text with the agent's `environment` set; `default` removes the key, as it is the default. */
export function setAgentEnvironment(text: string, agent: string, environment: string): string {
  const doc = parseDocument(text);
  const agents = doc.getIn(['spec', 'agents']);
  if (!isSeq(agents)) return text;
  const item = agents.items.find((a) => isMap(a) && a.get('name') === agent);
  if (!isMap(item)) return text;
  if (environment === defaultEnvironment) item.delete('environment');
  else item.set('environment', environment);
  return doc.toString();
}
