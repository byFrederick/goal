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

// Evals is evals.schema.json, the contract for a definition's eval cases (evals/cases/*.yaml) and
// its judges' front matter (evals/judges/*.md).
//
//go:embed evals.schema.json
var Evals []byte
