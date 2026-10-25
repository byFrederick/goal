package config

import (
	"reflect"
	"testing"
)

// SecretUses names every place a secret is needed, the reference it holds and what it is for,
// so the credentials check can resolve each one without reading values from the definition.
func TestSecretUses_ListsEveryReferenceAndLiteral(t *testing.T) {
	t.Parallel()

	f, err := Load(checkTree(edited(
		runtimeModel, runtimeModel+"    repository: { url: \"https://code.example.com/acme/app\", token: env:ACME_REPO_TOKEN }\n",
		specAgents, `  connections:
    - name: tickets
      type: mcp
      transport: http
      url: https://tickets.example.com/mcp
      headers: { Authorization: "Bearer env:ACME_TICKETS_TOKEN", X-Team: support }
    - name: shop-db
      type: mcp
      transport: stdio
      image: ghcr.io/acme/db-mcp:1.0.0
      command: [serve, --password, env:ACME_DB_PASSWORD]
      env: { DATABASE_URL: env:ACME_DB_URL }
`+specAgents,
	), nil))
	if err != nil {
		t.Fatal(err)
	}

	want := []SecretUse{
		{
			Path: "spec.runtime.repository.token", Doc: "/spec/runtime/repository/token", Purpose: PurposeRepository,
			URL: "https://code.example.com/acme/app", Ref: "env:ACME_REPO_TOKEN",
		},
		{
			Path: "spec.connections[0].headers.Authorization", Doc: "/spec/connections/0/headers/Authorization",
			Purpose: PurposeConnection, Connection: "tickets", URL: "https://tickets.example.com/mcp", Ref: "env:ACME_TICKETS_TOKEN",
		},
		{
			Path: "spec.connections[0].headers.X-Team", Doc: "/spec/connections/0/headers/X-Team", Purpose: PurposeConnection, Connection: "tickets",
			URL: "https://tickets.example.com/mcp",
		},
		{
			Path: "spec.connections[1].env.DATABASE_URL", Doc: "/spec/connections/1/env/DATABASE_URL",
			Purpose: PurposeConnection, Connection: "shop-db", Ref: "env:ACME_DB_URL",
		},
		{
			Path: "spec.connections[1].command[2]", Doc: "/spec/connections/1/command/2",
			Purpose: PurposeConnection, Connection: "shop-db", Ref: "env:ACME_DB_PASSWORD",
		},
	}

	if got := f.SecretUses(); !reflect.DeepEqual(got, want) {
		t.Errorf("uses =\n%+v\nwant\n%+v", got, want)
	}

	if l := f.Locate(want[4].Doc); l.Line != 20 || l.Section != "connections" {
		t.Errorf("command reference located at %+v, want line 20 in connections", l)
	}
}
