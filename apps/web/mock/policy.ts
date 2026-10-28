// The organization's model and tool policy for the mock API (GET and PUT /v1/orgs/{org}/policy, ADR 0024).
// The install's models and box sizes are fixed here, as the install's configuration sets them; the facts
// (which factories use each model and box size) are the acme fixtures' definitions, by factory name, and
// only the factories the organization has are named. The acme policy is the one models.jpg draws, at
// revision 3, so the screen opens as pictured. A PUT with a stale If-Match is 409 policy-conflict, one
// without it 428 precondition-required, and a value goald refuses is 422 policy-invalid.
import type { IncomingMessage, ServerResponse } from 'node:http';
import type { components } from '@goal/api-client';

type Schemas = components['schemas'];
type OrgPolicyInput = Schemas['OrgPolicyInput'];
type OrgPolicyView = Schemas['OrgPolicyView'];
type PolicyModel = Schemas['PolicyModel'];
type PolicyBoxSize = Schemas['PolicyBoxSize'];

const gib = 1024 ** 3;

/** The install's models (GOAL_MODELS), in its order. */
const installModels: readonly { id: string; label: string }[] = [
  { id: 'claude-opus-5-5', label: 'Claude Opus 5.5' },
  { id: 'claude-sonnet-5-5', label: 'Claude Sonnet 5.5' },
  { id: 'claude-haiku-5-5', label: 'Claude Haiku 5.5' },
  { id: 'claude-fable-5-1', label: 'Claude Fable 5.1' },
];

/** The install's box sizes: their vCPU and memory, and the factories (by name) whose runtime.size is each. */
const boxSizes: readonly { size: PolicyBoxSize['size']; vcpu: number; gib: number; factories: string[] }[] = [
  { size: 'small', vcpu: 2, gib: 4, factories: ['support', 'sre'] },
  { size: 'medium', vcpu: 4, gib: 8, factories: [] },
  { size: 'large', vcpu: 8, gib: 16, factories: ['software'] },
];

/** The agents of the acme fixtures that use a model, by factory name: the facts the policy answer carries. */
const agentsUsing: Record<string, { factory: string; agents: string[] }[]> = {
  'claude-sonnet-5-5': [{ factory: 'support', agents: ['responder'] }],
  'claude-haiku-5-5': [{ factory: 'software', agents: ['frontend-engineer'] }],
};

/** The egress addresses the install lists (GOAL_EGRESS_ADDRESSES). Documentation ranges, not the operator's. */
const egressAddresses = ['192.0.2.0/24', '2001:db8::/32'];

/** The policy acme's organization saved, as models.jpg draws it. */
const acmePolicy = (): OrgPolicyInput => ({
  allowed_models: installModels.map((m) => m.id),
  default_model: 'claude-opus-5-5',
  denied_commands: [
    'Bash(terraform apply:*)',
    'Bash(terraform destroy:*)',
    'Bash(git push --force:*)',
    'Bash(git push * main:*)',
    'Bash(--no-verify:*)',
    'Bash(wrangler deploy:*)',
    'Bash(gh release:*)',
  ],
  tools: {
    write_outside_branch: 'ask',
    merge_pull_request: 'review_approval',
    read_web: 'allowed',
    use_credentials: 'attached_only',
  },
  box_time_limit_minutes: 180,
});

export interface PolicyFactory {
  id: string;
  name: string;
}

export interface PolicyOptions {
  clock: () => number;
  /** The factories the organization has, as the mock keeps them. */
  factoriesOf: (org: string) => PolicyFactory[];
}

interface Stored {
  policy: OrgPolicyInput;
  revision: number;
  updatedAt: number;
}

/** A PUT's body or a refusal: what the handler answers. */
type Refusal = { status: number; title: string; detail?: string; code: string };

export function createPolicy(options: PolicyOptions) {
  const stored = new Map<string, Stored>();

  const of = (org: string): Stored => {
    let entry = stored.get(org);
    if (!entry) {
      entry = { policy: acmePolicy(), revision: 3, updatedAt: options.clock() };
      stored.set(org, entry);
    }
    return entry;
  };

  /** The factories of the organization the acme fixtures name, with the agents of each that use a model. */
  const usersOf = (org: string, uses: { factory: string; agents: string[] }[]) =>
    uses.flatMap((use) => {
      const factory = options.factoriesOf(org).find((f) => f.name === use.factory);
      return factory ? [{ factory_id: factory.id, name: factory.name, agents: use.agents }] : [];
    });

  const viewOf = (org: string): OrgPolicyView => {
    const entry = of(org);
    const models: PolicyModel[] = installModels.map((m) => ({
      id: m.id,
      label: m.label,
      used_by: usersOf(org, agentsUsing[m.id] ?? []),
    }));
    const sizes: PolicyBoxSize[] = boxSizes.map((b) => ({
      size: b.size,
      vcpu: b.vcpu,
      memory_bytes: b.gib * gib,
      factories: usersOf(
        org,
        b.factories.map((factory) => ({ factory, agents: [] })),
      ).map(({ factory_id, name }) => ({ factory_id, name })),
    }));
    return {
      policy: entry.policy,
      revision: entry.revision,
      updated_at: new Date(entry.updatedAt).toISOString(),
      models,
      box_sizes: sizes,
      egress_addresses: egressAddresses,
    };
  };

  const invalid = (detail: string): Refusal => ({
    status: 422,
    title: 'Unprocessable Content',
    detail,
    code: 'policy-invalid',
  });

  /** Checks a PUT the way goald does; the first refusal names what is wrong. */
  const refusalOf = (policy: OrgPolicyInput): Refusal | null => {
    const ids = new Set(installModels.map((m) => m.id));
    const unknown = policy.allowed_models.find((id) => !ids.has(id));
    if (unknown) return invalid(`${unknown} is not one of the install's models`);
    if (policy.allowed_models.length === 0) return invalid('allow at least one model');
    if (!policy.allowed_models.includes(policy.default_model)) {
      return invalid('the default model must be one of the allowed models');
    }
    if (policy.denied_commands.some((c) => c.trim() === '')) return invalid('a denied command is blank');
    if (!Number.isInteger(policy.box_time_limit_minutes) || policy.box_time_limit_minutes < 1) {
      return invalid('the box time limit must be a positive number of minutes');
    }
    return null;
  };

  const send = (res: ServerResponse, status: number, body: unknown, etag?: number) => {
    res.statusCode = status;
    res.setHeader('Content-Type', status >= 400 ? 'application/problem+json' : 'application/json');
    if (etag !== undefined) res.setHeader('ETag', `"${etag}"`);
    res.end(JSON.stringify(body));
  };

  /** Answers the policy routes of one organization; false for any other path, so the caller goes on. */
  async function handle(
    org: { id: string },
    rest: string,
    method: string,
    req: IncomingMessage,
    res: ServerResponse,
    readBody: () => Promise<Record<string, unknown>>,
  ): Promise<boolean> {
    if (rest !== '/policy') return false;
    if (method === 'GET') {
      const view = viewOf(org.id);
      send(res, 200, view, view.revision);
      return true;
    }
    if (method !== 'PUT') return false;

    const entry = of(org.id);
    const ifMatch = req.headers['if-match'];
    if (ifMatch === undefined) {
      send(res, 428, { title: 'Precondition Required', status: 428, code: 'precondition-required' });
      return true;
    }
    if (ifMatch.replaceAll('"', '') !== String(entry.revision)) {
      send(res, 409, {
        title: 'Conflict',
        status: 409,
        detail: 'the policy changed since you read it',
        code: 'policy-conflict',
      });
      return true;
    }
    const body = (await readBody()) as unknown as OrgPolicyInput;
    const refusal = refusalOf(body);
    if (refusal) {
      send(res, refusal.status, { title: refusal.title, status: refusal.status, detail: refusal.detail, code: refusal.code });
      return true;
    }
    entry.policy = body;
    entry.revision += 1;
    entry.updatedAt = options.clock();
    const view = viewOf(org.id);
    send(res, 200, view, view.revision);
    return true;
  }

  return { handle };
}
