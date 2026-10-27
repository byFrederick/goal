package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.OrgPolicies = (*OrgPolicies)(nil)

// OrgPolicies implements ports.OrgPolicies over 00069's org_policies, as goal_app under row-level
// security: every statement sees the organization in ctx. An organization without a row reads
// as domain.DefaultOrgPolicy of the platform's models. A save inserts the first row or updates
// the one at the caller's revision, and appends its audit entry in the same transaction.
type OrgPolicies struct {
	s      *Store
	models []string
}

// NewOrgPolicies returns the policies on s's pool; models are the platform's model ids
// (GOAL_MODELS), in order, for the default policy.
func NewOrgPolicies(s *Store, models []string) *OrgPolicies {
	return &OrgPolicies{s: s, models: slices.Clone(models)}
}

// The audit data key of a saved policy's revision.
const auditRevision = "revision"

// Get implements ports.OrgPolicies.
func (o *OrgPolicies) Get(ctx context.Context) (domain.OrgPolicy, error) {
	r, err := fetch(ctx, o.s.read, func(q *db.Queries) (db.OrgPolicyRow, error) { return q.OrgPolicy(ctx) })
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DefaultOrgPolicy(o.models), nil
	}

	if err != nil {
		return domain.OrgPolicy{}, fmt.Errorf("get the organization policy: %w", err)
	}

	return domain.OrgPolicy{
		AllowedModels: r.AllowedModels, DefaultModel: r.DefaultModel, DeniedCommands: r.DeniedCommands,
		WriteOutsideBranch: domain.ToolSetting(r.WriteOutsideBranch), MergePullRequest: domain.ToolSetting(r.MergePullRequest),
		ReadWeb: domain.ToolSetting(r.ReadWeb), UseCredentials: domain.ToolSetting(r.UseCredentials),
		BoxTimeLimitMinutes: int(r.BoxTimeLimitMinutes), Revision: r.Revision,
		UpdatedBy: namedActor(r.UpdatedBy, r.UpdatedByName), UpdatedAt: r.UpdatedAt.UTC(),
	}, nil
}

// Put implements ports.OrgPolicies.
func (o *OrgPolicies) Put(ctx context.Context, p domain.OrgPolicy, revision int64, by domain.Actor) (domain.OrgPolicy, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.OrgPolicy{}, domain.ErrNoOrg
	}

	if err := p.Validate(); err != nil {
		return domain.OrgPolicy{}, err
	}

	if p.BoxTimeLimitMinutes <= 0 || p.BoxTimeLimitMinutes > math.MaxInt32 {
		return domain.OrgPolicy{}, fmt.Errorf("the box time limit %d is too long: %w", p.BoxTimeLimitMinutes, domain.ErrInvalid)
	}

	minutes := int32(p.BoxTimeLimitMinutes)

	out := p
	out.AllowedModels, out.DeniedCommands, out.UpdatedBy = slices.Clone(p.AllowedModels), slices.Clone(p.DeniedCommands), by

	if out.DeniedCommands == nil {
		out.DeniedCommands = []string{} // the column is NOT NULL
	}

	err := o.s.inTx(ctx, func(q *db.Queries) error {
		var err error

		out.Revision, out.UpdatedAt, err = savePolicy(ctx, q, out, minutes, revision, by)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("the organization policy is no longer at revision %d: %w", revision, ports.ErrConflict)
		}

		if err != nil {
			return ruleError(err, "")
		}

		return appendAudit(ctx, q, domain.AuditEntry{
			Actor: by, Action: domain.AuditOrgPolicyUpdated, TargetKind: domain.AuditTargetOrgPolicy, TargetID: string(org),
			Data: map[string]any{auditRevision: out.Revision},
		})
	})
	if err != nil {
		return domain.OrgPolicy{}, fmt.Errorf("save the organization policy: %w", err)
	}

	out.UpdatedAt = out.UpdatedAt.UTC()

	return out, nil
}

// savePolicy inserts the organization's first policy when revision is 0 and otherwise updates
// the one at revision, returning the new revision and time; pgx.ErrNoRows when the stored
// revision is another.
func savePolicy(ctx context.Context, q *db.Queries, p domain.OrgPolicy, minutes int32, revision int64,
	by domain.Actor,
) (int64, time.Time, error) {
	if revision == 0 {
		r, err := q.InsertOrgPolicy(ctx, db.InsertOrgPolicyParams{
			AllowedModels: p.AllowedModels, DefaultModel: p.DefaultModel, DeniedCommands: p.DeniedCommands,
			WriteOutsideBranch: string(p.WriteOutsideBranch), MergePullRequest: string(p.MergePullRequest),
			ReadWeb: string(p.ReadWeb), UseCredentials: string(p.UseCredentials), BoxTimeLimitMinutes: minutes, UpdatedBy: by.Ref(),
		})

		return r.Revision, r.UpdatedAt, err
	}

	r, err := q.UpdateOrgPolicy(ctx, db.UpdateOrgPolicyParams{
		AllowedModels: p.AllowedModels, DefaultModel: p.DefaultModel, DeniedCommands: p.DeniedCommands,
		WriteOutsideBranch: string(p.WriteOutsideBranch), MergePullRequest: string(p.MergePullRequest),
		ReadWeb: string(p.ReadWeb), UseCredentials: string(p.UseCredentials), BoxTimeLimitMinutes: minutes, UpdatedBy: by.Ref(),
		Revision: revision,
	})

	return r.Revision, r.UpdatedAt, err
}
