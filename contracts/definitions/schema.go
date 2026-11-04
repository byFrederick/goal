// Package definitions embeds the JSON Schemas so the CLI and daemon validate against the same files editors use.
package definitions

import _ "embed"

// Factory is factory.schema.json, the contract for every factory.yaml.
//
//go:embed factory.schema.json
var Factory []byte

// ConnectionKind is connection-kind.schema.json, the contract for every connection kind: the
// platform's (internal/connkind/kinds) and the organizations' own.
//
//go:embed connection-kind.schema.json
var ConnectionKind []byte

// Environment is environment.schema.json, the contract for a definition's environment files
// (environments/<name>.yaml, ADR 0037).
//
//go:embed environment.schema.json
var Environment []byte

// Memory is memory.schema.json, the contract for a definition's factory memory (memory.yaml,
// ADR 0038 decision 6).
//
//go:embed memory.schema.json
var Memory []byte
