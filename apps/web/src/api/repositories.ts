// Testing a repository's token before a factory exists (ADR 0037): POST …/repositories/test over
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

/**
 * Tests a repository URL, or an owner pattern, with the token in the named secret, on the factory's route
 * (ADR 0041): the secret must be open to the factory. The answer is kept per url; a failed check is an answer.
 */
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
