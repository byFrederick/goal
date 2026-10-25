package domain

import (
	"testing"
)

// An agent's own caps replace the runtime's; an unset one falls back to the runtime's.
func TestAgentSpec_Limits(t *testing.T) {
	t.Parallel()

	rt := Runtime{MaxTurnsPerTask: 40, PerItemUSD: 3}

	tests := []struct {
		name      string
		agent     AgentSpec
		wantTurns int
		wantUSD   float64
	}{
		{name: "runtime's", wantTurns: 40, wantUSD: 3},
		{name: "own turns", agent: AgentSpec{MaxTurns: 12}, wantTurns: 12, wantUSD: 3},
		{name: "own budget", agent: AgentSpec{PerItemUSD: 1.5}, wantTurns: 40, wantUSD: 1.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if turns, usd := tt.agent.Limits(rt); turns != tt.wantTurns || usd != tt.wantUSD {
				t.Errorf("Limits = %d, %v; want %d, %v", turns, usd, tt.wantTurns, tt.wantUSD)
			}
		})
	}
}
