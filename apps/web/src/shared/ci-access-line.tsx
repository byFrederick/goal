// One line under a repository test's summary: whether the token can read CI (ADR 0044). A failed check
// shows the host's reason and an ⓘ with the token step. It is a warning only when a station waits for CI;
// otherwise it stays quiet. A test kept before goald checked CI has no `ci`, and shows nothing.
import type { RepositoryCheck } from '../api/types';
import { cn } from '../ui/cn';
import { InfoTip } from '../ui/info-tip';
import type { LineSection } from './definition/model';

const tokenStep =
  'GitHub: a fine-grained token needs read access to Checks and Commit statuses. GitLab: the token needs read_api.';

/** Whether any station of the line sets waitFor, as "ci" or as the { ci: {…} } form. */
export function waitsForCi(line: LineSection | undefined): boolean {
  return (line?.stations ?? []).some((s) => s.waitFor !== undefined && s.waitFor !== null);
}

export function CiAccessLine({ ci, waitsForCi: waits }: { ci: RepositoryCheck | undefined; waitsForCi: boolean }) {
  if (!ci) return null;
  const warn = !ci.ok && waits;
  return (
    <div className="flex min-w-0 items-start gap-1 text-secondary" data-testid="ci-access-line">
      <span className={cn('min-w-0 break-words', warn ? 'text-rework-ink' : 'text-muted')}>
        {ci.ok ? 'The token can read CI.' : `Can’t read CI. ${ci.reason}`.trim()}
      </span>
      {ci.ok ? null : (
        <InfoTip about="reading CI" className="max-md:size-11">
          {tokenStep}
        </InfoTip>
      )}
    </div>
  );
}
