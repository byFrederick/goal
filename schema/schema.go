// Package schema embeds the factory JSON Schema so the CLI and daemon validate against the same file editors use.
package schema

import _ "embed"

// Factory is factory.schema.json, the contract for every factory.yaml.
//
//go:embed factory.schema.json
var Factory []byte
