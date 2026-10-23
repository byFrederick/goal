package porttest

import (
	"context"
	"fmt"
	"strings"

	"github.com/byFrederick/factory/internal/ports"
)

var _ ports.Secrets = Secrets(nil)

// Secrets resolves "env:NAME" from the map instead of the process environment; any other
// reference is a literal value. Read-only after construction, so safe for concurrent use.
type Secrets map[string]string

// Resolve implements ports.Secrets.
func (s Secrets) Resolve(_ context.Context, ref string) (string, error) {
	name, isEnv := strings.CutPrefix(ref, "env:")
	if !isEnv {
		return ref, nil
	}

	v, ok := s[name]
	if !ok {
		return "", fmt.Errorf("resolve env:%s: %w", name, ports.ErrSecretNotFound)
	}

	return v, nil
}
