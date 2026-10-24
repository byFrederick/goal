package api

// rule is one permission of api/permissions.yaml as cmd/permgen generates it into
// permissionRules (permissions_gen.go); operationScopes names each operation's permission.
// Change the yaml or an operation's x-goal-scope, then run task api:gen.
type rule struct {
	// level is "install" (held outside an organization), "org" or "factory".
	level string
	// orgRoles hold an org permission. Owners and admins hold every factory permission.
	orgRoles []string
	// factoryRole is the lowest factory role holding a factory permission.
	factoryRole string
	// keys: an API key may carry it. fresh: a session signed in within the last 10 minutes.
	// platformAdmin: only platform admins hold this install permission.
	keys, fresh, platformAdmin bool
}
