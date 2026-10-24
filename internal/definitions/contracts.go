package definitions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.Contracts = (*Cache)(nil)

// Validate implements ports.Contracts: output must satisfy the JSON Schema the factory's
// current definition declares for contract. A schema can reference only itself; nothing is
// fetched from the network or the host's files.
func (c *Cache) Validate(ctx context.Context, factory domain.FactoryID, contract string, output json.RawMessage) error {
	l, err := c.get(ctx, factory)
	if err != nil {
		return err
	}

	schema, err := compileContract(l, contract)
	if err != nil {
		return err
	}

	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(output))
	if err != nil {
		return fmt.Errorf("%w: %s: output is not JSON: %w", ports.ErrContractViolation, contract, err)
	}

	if err := schema.Validate(doc); err != nil {
		if verr, ok := errors.AsType[*jsonschema.ValidationError](err); ok {
			return fmt.Errorf("%w: %s: %w", ports.ErrContractViolation, contract, verr)
		}

		return fmt.Errorf("validate against contract %s: %w", contract, err)
	}

	return nil
}

// contractFile returns the path of a contract's schema inside the factory's tree.
func contractFile(l loaded, contract string) (string, error) {
	p, ok := l.factory.Spec.Contracts[contract]
	if !ok {
		return "", fmt.Errorf("factory %s: unknown contract %q", l.factory.Metadata.ID, contract)
	}

	return path.Clean(p), nil
}

func compileContract(l loaded, contract string) (*jsonschema.Schema, error) {
	file, err := contractFile(l, contract)
	if err != nil {
		return nil, err
	}

	body, err := fs.ReadFile(l.snapshot.FS(), file)
	if err != nil {
		return nil, fmt.Errorf("contract %s: %w", contract, err)
	}

	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("contract %s: %s is not JSON: %w", contract, file, err)
	}

	comp := jsonschema.NewCompiler()
	comp.UseLoader(jsonschema.SchemeURLLoader{}) // no loader for any scheme: no remote or file refs

	url := "goal:///" + l.factory.Metadata.ID + "/" + file
	if err := comp.AddResource(url, doc); err != nil {
		return nil, fmt.Errorf("contract %s: %w", contract, err)
	}

	schema, err := comp.Compile(url)
	if err != nil {
		return nil, fmt.Errorf("contract %s: compile %s: %w", contract, file, err)
	}

	return schema, nil
}
