package porttest

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.Contracts = (*Contracts)(nil)

// Contracts is a stand-in for JSON Schema validation: a contract is a list of required
// top-level keys of a JSON object.
type Contracts struct {
	mu       sync.Mutex
	required map[string][]string // version/contract → keys
}

// NewContracts returns a validator that knows no contracts.
func NewContracts() *Contracts { return &Contracts{required: map[string][]string{}} }

// Require declares a contract of a definition version whose outputs must be JSON objects with
// all keys.
func (c *Contracts) Require(version domain.DefinitionVersionID, contract string, keys ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.required[string(version)+"/"+contract] = keys
}

// Validate implements ports.Contracts.
func (c *Contracts) Validate(_ context.Context, version domain.DefinitionVersionID, contract string, output json.RawMessage) error {
	c.mu.Lock()
	keys, ok := c.required[string(version)+"/"+contract]
	c.mu.Unlock()

	if !ok {
		return fmt.Errorf("definition version %s: unknown contract %q", version, contract)
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(output, &obj); err != nil {
		return fmt.Errorf("%w: %s: output is not a JSON object", ports.ErrContractViolation, contract)
	}

	for _, k := range keys {
		if _, ok := obj[k]; !ok {
			return fmt.Errorf("%w: %s: missing property %q", ports.ErrContractViolation, contract, k)
		}
	}

	return nil
}
