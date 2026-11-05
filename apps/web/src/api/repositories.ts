// A factory's repository tests (ADR 0041 decision 8): POST …/factories/{factory}/repositories/test and the kept
// results at …/repositories/tests. Testing a repository's token before a factory exists (ADR 0037): POST …/repositories/test over
// client.ts's goald client. The secret is opened inside goald; no answer carries the token.
import { orgPath, unwrap } from '@goal/api-client';
import { cancel, goal } from './client';
import type {
  FactoryRepositoryTest,
  FactoryRepositoryTestRequest,
  RepositoryTestRequest,
  RepositoryTestResult,
} from './types';

/** Whether the token in the named secret can read the repository and push to it; a failed check is an answer, not an error. */
export function testRepository(
  org: string,
  request: RepositoryTestRequest,
  signal?: AbortSignal,
): Promise<RepositoryTestResult> {
  return unwrap(
    goal.POST(orgPath('/repositories/test'), { params: { path: { org } }, body: request, ...cancel(signal) }),
  );
}

/** A factory's kept results, one per url tested, sorted by url (factory:read); reading tests nothing. */
export async function listFactoryRepositoryTests(
  org: string,
  factory: string,
  signal?: AbortSignal,
): Promise<FactoryRepositoryTest[]> {
  const list = await unwrap(
    goal.GET(orgPath('/factories/{factory}/repositories/tests'), {
      params: { path: { org, factory } },
      ...cancel(signal),
    }),
  );
  return list.tests;
}

/** Tests the token in `secret` against `url` for the factory and keeps the result (definition:edit); a failed check is an answer; the secret must be open to the factory. */
export function testFactoryRepository(
  org: string,
  factory: string,
  request: FactoryRepositoryTestRequest,
  signal?: AbortSignal,
): Promise<FactoryRepositoryTest> {
  return unwrap(
    goal.POST(orgPath('/factories/{factory}/repositories/test'), {
      params: { path: { org, factory } },
      body: request,
      ...cancel(signal),
    }),
  );
}
