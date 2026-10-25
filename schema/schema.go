// Package schema embeds the JSON Schemas so the CLI and daemon validate against the same files editors use.
package schema

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
