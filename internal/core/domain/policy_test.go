package domain

import "testing"

// Only mode branch gives an assembly an integration branch: the zero policy, as in a line built
// without config, is trunk.
func TestIntegrationPolicy_BranchesOnlyInBranchMode(t *testing.T) {
	t.Parallel()

	for p, want := range map[IntegrationPolicy]bool{
		{}:                            false,
		{Mode: IntegrationModeTrunk}:  false,
		{Mode: IntegrationModeBranch}: true,
		{Mode: IntegrationModeBranch, MinParts: 3, Refresh: RefreshEachPart}: true,
	} {
		if got := p.Branches(); got != want {
			t.Errorf("%+v.Branches() = %v, want %v", p, got, want)
		}
	}
}
