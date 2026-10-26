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

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Contracts = (*Cache)(nil)

// Validate implements ports.Contracts: output must satisfy the JSON Schema the definition
// version declares for contract. A schema can reference only itself; nothing is fetched from
// the network or the host's files.
func (c *Cache) Validate(ctx context.Context, id domain.DefinitionVersionID, contract string, output json.RawMessage) error {
	v, err := c.version(ctx, id)
	if err != nil {
		return err
	}

	schema, err := compileContract(v, contract)
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

// contractFile returns the path of a contract's schema inside the version's tree.
func contractFile(v *version, contract string) (string, error) {
	p, ok := v.factory.Spec.Contracts[contract]
	if !ok {
		return "", fmt.Errorf("factory %s v%d: unknown contract %q", v.factory.Metadata.ID, v.number, contract)
	}

	return path.Clean(p), nil
}

func compileContract(v *version, contract string) (*jsonschema.Schema, error) {
	file, err := contractFile(v, contract)
	if err != nil {
		return nil, err
	}

	body, err := fs.ReadFile(v.files, file)
	if err != nil {
		return nil, fmt.Errorf("contract %s: %w", contract, err)
	}

	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("contract %s: %s is not JSON: %w", contract, file, err)
	}

	comp := jsonschema.NewCompiler()
	comp.UseLoader(jsonschema.SchemeURLLoader{}) // no loader for any scheme: no remote or file refs

	url := "goal:///" + v.factory.Metadata.ID + "/" + string(v.id) + "/" + file
	if err := comp.AddResource(url, doc); err != nil {
		return nil, fmt.Errorf("contract %s: %w", contract, err)
	}

	schema, err := comp.Compile(url)
	if err != nil {
		return nil, fmt.Errorf("contract %s: compile %s: %w", contract, file, err)
	}

	return schema, nil
}
