package api

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The policy routes (ADR 0024): the organization's policy, a ceiling over every factory
// definition, with the facts its screen shows beside it. Those are computed on every read from
// the published definitions the caller sees, never stored: who uses each of the install's models
// and box sizes. A save names the revision it read in If-Match (428 without it); another revision
// is 409 policy-conflict and a policy the install cannot honor 422 policy-invalid.

var (
	// errPolicyInvalid is a policy that breaks its rules or allows a model the install does not
	// offer (422).
	errPolicyInvalid = errors.New("the policy is not valid")
	// errPolicyConflict is a save on a revision that is no longer the stored one (409).
	errPolicyConflict = errors.New("the policy changed since it was read: read it again")
)

// policyProblem answers the policy routes' refusals.
func policyProblem(err error) (gen.Problem, bool) {
	switch {
	case errors.Is(err, errPolicyInvalid):
		p := detailed(http.StatusUnprocessableEntity, err)
		p.Code = optional("policy-invalid")

		return p, true
	case errors.Is(err, errPolicyConflict):
		p := detailed(http.StatusConflict, err)
		p.Code = optional("policy-conflict")

		return p, true
	default:
		return gen.Problem{}, false
	}
}

func (s *server) GetOrgPolicy(ctx context.Context, _ gen.GetOrgPolicyRequestObject) (gen.GetOrgPolicyResponseObject, error) {
	p, err := s.policies.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the organization policy: %w", err)
	}

	return gen.GetOrgPolicy200JSONResponse{Body: s.policyView(ctx, p), Headers: gen.GetOrgPolicy200ResponseHeaders{ETag: revisionTag(p)}}, nil
}

func (s *server) PutOrgPolicy(ctx context.Context, req gen.PutOrgPolicyRequestObject) (gen.PutOrgPolicyResponseObject, error) {
	actor, err := actorOf(ctx, "save the organization policy")
	if err != nil {
		return nil, err
	}

	tag, err := ifMatch(req.Params.IfMatch)
	if err != nil {
		return nil, err
	}

	revision, err := strconv.ParseInt(tag, 10, 64)
	if err != nil || revision < 0 {
		return nil, fmt.Errorf("%w: If-Match %q is not a policy revision", errInvalid, tag)
	}

	p := policyIn(*req.Body)
	if err := s.checkPolicy(ctx, p); err != nil {
		return nil, err
	}

	saved, err := s.policies.Put(ctx, p, revision, actor)

	switch {
	case errors.Is(err, ports.ErrConflict):
		return nil, fmt.Errorf("%w (%w)", errPolicyConflict, err)
	case errors.Is(err, domain.ErrInvalid):
		return nil, fmt.Errorf("%w: %w", errPolicyInvalid, err)
	case err != nil:
		return nil, fmt.Errorf("save the organization policy: %w", err)
	}

	return gen.PutOrgPolicy200JSONResponse{Body: s.policyView(ctx, saved), Headers: gen.PutOrgPolicy200ResponseHeaders{ETag: revisionTag(saved)}}, nil
}

// checkPolicy refuses a policy that breaks its rules or allows a model the install does not offer.
func (s *server) checkPolicy(ctx context.Context, p domain.OrgPolicy) error {
	installed := s.builder.Meta(ctx).Models
	errs := []error{p.Validate()}

	for _, m := range p.AllowedModels {
		if !slices.ContainsFunc(installed, func(i domain.Model) bool { return i.ID == m }) {
			errs = append(errs, fmt.Errorf("model %s is not one of the install's models (GOAL_MODELS)", m))
		}
	}

	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("%w: %w", errPolicyInvalid, err)
	}

	return nil
}

func policyIn(b gen.OrgPolicyInput) domain.OrgPolicy {
	return domain.OrgPolicy{
		AllowedModels: b.AllowedModels, DefaultModel: b.DefaultModel, DeniedCommands: b.DeniedCommands,
		WriteOutsideBranch: domain.ToolSetting(b.Tools.WriteOutsideBranch), MergePullRequest: domain.ToolSetting(b.Tools.MergePullRequest),
		ReadWeb: domain.ToolSetting(b.Tools.ReadWeb), UseCredentials: domain.ToolSetting(b.Tools.UseCredentials),
		BoxTimeLimitMinutes: b.BoxTimeLimitMinutes,
	}
}

func revisionTag(p domain.OrgPolicy) string { return quote(strconv.FormatInt(p.Revision, 10)) }

// policyView is p with the facts beside it: the install's models and who uses each, the box sizes
// and who uses each, and the egress addresses.
func (s *server) policyView(ctx context.Context, p domain.OrgPolicy) gen.OrgPolicyView {
	out := gen.OrgPolicyView{
		Policy: gen.OrgPolicyInput{
			AllowedModels: nonNil(p.AllowedModels), DefaultModel: p.DefaultModel, DeniedCommands: nonNil(p.DeniedCommands),
			Tools: gen.ToolPolicy{
				WriteOutsideBranch: gen.ToolPolicyWriteOutsideBranch(p.WriteOutsideBranch), MergePullRequest: gen.ToolPolicyMergePullRequest(p.MergePullRequest),
				ReadWeb: gen.ToolPolicyReadWeb(p.ReadWeb), UseCredentials: gen.ToolPolicyUseCredentials(p.UseCredentials),
			},
			BoxTimeLimitMinutes: p.BoxTimeLimitMinutes,
		},
		Revision:        p.Revision,
		EgressAddresses: make([]string, 0, len(s.egress)),
	}

	if p.Revision > 0 {
		out.UpdatedAt = new(p.UpdatedAt)
	}

	for _, a := range s.egress {
		out.EgressAddresses = append(out.EgressAddresses, a.String())
	}

	factories := slices.SortedFunc(slices.Values(s.visibleFactories(ctx)), func(a, b *config.Factory) int {
		return cmp.Or(cmp.Compare(a.Metadata.Name, b.Metadata.Name), cmp.Compare(a.Metadata.ID, b.Metadata.ID))
	})

	models := s.builder.Meta(ctx).Models
	out.Models = make([]gen.PolicyModel, 0, len(models))

	for _, m := range models {
		out.Models = append(out.Models, gen.PolicyModel{Id: m.ID, Label: m.Label, UsedBy: modelUses(factories, m.ID, p.DefaultModel)})
	}

	out.BoxSizes = make([]gen.PolicyBoxSize, 0, len(s.boxSizes))

	for _, b := range s.boxSizes {
		size := gen.PolicyBoxSize{Size: gen.PolicyBoxSizeSize(b.Size), Vcpu: b.VCPUs, MemoryBytes: b.MemoryBytes, Factories: []gen.SizeUse{}}

		for _, f := range factories {
			if cmp.Or(domain.SandboxSize(f.Spec.Runtime.Size), domain.SandboxMedium) == b.Size {
				size.Factories = append(size.Factories, gen.SizeUse{FactoryId: f.Metadata.ID, Name: f.Metadata.Name})
			}
		}

		out.BoxSizes = append(out.BoxSizes, size)
	}

	return out
}

// modelUses are the factories, in order, whose agents use model: an agent uses its own model,
// else its factory's runtime.model, else the policy's default.
func modelUses(factories []*config.Factory, model, def string) []gen.ModelUse {
	out := []gen.ModelUse{}

	for _, f := range factories {
		var agents []string

		for _, a := range f.Spec.Agents {
			if cmp.Or(a.Model, f.Spec.Runtime.Model, def) == model {
				agents = append(agents, a.Name)
			}
		}

		if len(agents) > 0 {
			out = append(out, gen.ModelUse{FactoryId: f.Metadata.ID, Name: f.Metadata.Name, Agents: agents})
		}
	}

	return out
}

// nonNil is v, or an empty list for nil, which the contract writes as [].
func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}

	return v
}
