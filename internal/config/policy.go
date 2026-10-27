package config

import (
	"fmt"
	"slices"

	"github.com/byFrederick/goal/internal/core/domain"
)

// CheckPolicy is the check of a definition against its organization's policy (ADR 0024 decision
// 3): authoring runs it on publish and validate, since the policy is not in the definition.
const CheckPolicy = "policy"

// CodeModelNotAllowed: spec.runtime.model or an agent's model is not one the organization's
// policy allows.
const CodeModelNotAllowed = "model-not-allowed"

// ModelProblems reports each model f names, in spec.runtime.model and in its agents' model, that
// is not in allowed: an error at that key naming the agent (or runtime) and the model. A factory
// that names no model gets the policy's default at run start, so it has none.
func ModelProblems(f *Factory, allowed []string) []domain.DefinitionProblem {
	var out []domain.DefinitionProblem

	refuse := func(path []string, who, model string) {
		out = append(out, f.doc.locate(path).Problem(CheckPolicy, CodeModelNotAllowed, domain.SeverityError,
			fmt.Sprintf("%s uses model %s, which the organization policy does not allow; allowed: %v", who, model, allowed)))
	}

	if m := f.Spec.Runtime.Model; m != "" && !slices.Contains(allowed, m) {
		refuse(at("spec", "runtime", "model"), "runtime", m)
	}

	for i, a := range f.Spec.Agents {
		if a.Model != "" && !slices.Contains(allowed, a.Model) {
			refuse(at("spec", keyAgents, i, "model"), "agent "+a.Name, a.Model)
		}
	}

	return out
}
