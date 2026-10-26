package main

import "os"

const listenEnv = "GOAL_LISTEN"

const tokenRef = "env:" + "GOAL_TOKEN"

func settings(getenv func(string) string, lookup func(string) (string, bool)) {
	or := func(name, def string) string { return def }
	_ = getenv(listenEnv)
	_ = or("GOAL_MODE", "dev")
	_ = os.Getenv("GOAL_OS")
	_, _ = lookup("GOAL_LOOKUP")
}
