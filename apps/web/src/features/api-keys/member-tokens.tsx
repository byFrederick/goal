// Members' tokens on the API keys page (spec 3 §10.2, PR 3-12b): every member's personal tokens in
// the organization, for owners and admins (api_key:read). Revoke… needs api_key:manage (a recent
// sign-in). A member's own tokens are on the account page (features/account/personal-tokens.tsx).
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { toast } from 'sonner';
import { revokeMemberToken } from '../../api/keys';
import { keys, memberTokensQuery } from '../../api/queries';
import type { OrgSummary, PersonalToken } from '../../api/types';
import { displayName } from '../../shared/names';
import { useOrgHolds } from '../../shared/org';
import { RevokeDialog, TokenList } from '../../shared/tokens/token-list';

/** Every member's tokens, for owners and admins. */
export function MemberTokensSection({ org }: { org: OrgSummary }) {
  const client = useQueryClient();
  const manage = useOrgHolds('api_key:manage');
  const list = useQuery(memberTokensQuery(org.id));
  const [revoking, setRevoking] = useState<PersonalToken | null>(null);

  const revoke = useMutation({
    mutationFn: (t: PersonalToken) => revokeMemberToken(org.id, t.id),
    onSuccess: async (_, t) => {
      setRevoking(null);
      toast.success(`Revoked ${displayName(t.owner)}’s token ${t.name}`, {
        description: 'Anything using it stops working at once.',
      });
      await Promise.all([
        client.invalidateQueries({ queryKey: keys.memberTokens(org.id) }),
        client.invalidateQueries({ queryKey: keys.personalTokens(org.id) }),
      ]);
    },
  });

  return (
    <section
      aria-labelledby="member-tokens-heading"
      className="flex flex-col gap-4 rounded-card border border-line bg-card p-5"
    >
      <div className="flex max-w-[680px] min-w-0 flex-col gap-2">
        <h2 id="member-tokens-heading" className="m-0 text-heading">
          Members’ tokens
        </h2>
        <p className="m-0 text-body text-muted">
          Every member’s personal tokens in {org.name}. Each acts as its owner, so removing someone from {org.name}{' '}
          deletes theirs.
        </p>
      </div>
      <TokenList
        list={list}
        what="members’ tokens"
        caption={`Members’ personal tokens in ${org.name}`}
        empty="No member has a personal token."
        owner
        onRevoke={manage ? setRevoking : undefined}
      />
      <RevokeDialog
        token={revoking}
        title={revoking ? `Revoke ${displayName(revoking.owner)}’s token ${revoking.name}?` : 'Revoke token?'}
        pending={revoke.isPending}
        error={revoke.error}
        onCancel={() => {
          setRevoking(null);
          revoke.reset();
        }}
        onConfirm={() => {
          if (revoking) revoke.mutate(revoking);
        }}
      />
    </section>
  );
}
